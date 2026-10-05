package auth

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// QuotaWindow is a read-only interpretation of an observed provider limit.
type QuotaWindow struct {
	Name         string
	UsedFraction float64
	ResetAt      time.Time
	Exhausted    bool
	Known        bool
}

// QuotaWindows prefers model observations and falls back to credential observations.
func QuotaWindows(auth *Auth, model string) []QuotaWindow {
	return quotaWindowsAt(auth, model, time.Now())
}

func quotaWindowsAt(auth *Auth, model string, now time.Time) []QuotaWindow {
	if auth == nil {
		return nil
	}
	q := auth.Quota
	if state := auth.ModelStates[model]; state != nil && len(state.Quota.Signals) > 0 {
		q = state.Quota
	}
	get := func(key string) string { return q.Signals[http.CanonicalHeaderKey(key)] }
	fresh := !q.ObservedAt.IsZero() && now.Sub(q.ObservedAt) <= 6*time.Hour
	var windows []QuotaWindow
	names := []string{"5h", "7d", "7d_oi"}
	if strings.EqualFold(auth.Provider, "codex") {
		names = []string{"primary", "secondary"}
	} else if !strings.EqualFold(auth.Provider, "claude") {
		return nil
	}
	for _, name := range names {
		w := QuotaWindow{Name: name}
		var raw, reset string
		if strings.EqualFold(auth.Provider, "claude") {
			prefix := "Anthropic-Ratelimit-Unified-" + name + "-"
			raw, reset = get(prefix+"Utilization"), get(prefix+"Reset")
			w.Exhausted = strings.EqualFold(get(prefix+"Status"), "rejected")
		} else {
			prefix := "X-Codex-" + name + "-"
			raw, reset = get(prefix+"Used-Percent"), get(prefix+"Reset-At")
			if reset == "" {
				if seconds, err := strconv.ParseFloat(get(prefix+"Reset-After-Seconds"), 64); err == nil && seconds >= 0 && seconds < 1e10 {
					w.ResetAt = q.ObservedAt.Add(time.Duration(seconds * float64(time.Second)))
				}
			}
			w.Exhausted = strings.EqualFold(get("X-Codex-Limit-Reached"), "true")
		}
		if reset != "" {
			if seconds, err := strconv.ParseInt(reset, 10, 64); err == nil {
				w.ResetAt = time.Unix(seconds, 0)
			} else {
				w.ResetAt, _ = time.Parse(time.RFC3339Nano, reset)
			}
		}
		used, err := strconv.ParseFloat(raw, 64)
		if strings.EqualFold(auth.Provider, "codex") {
			used /= 100
		}
		valid := err == nil && !math.IsNaN(used) && !math.IsInf(used, 0) && used >= 0
		if valid {
			w.UsedFraction = used
		}
		w.Known = fresh && valid && w.ResetAt.After(now)
		w.Exhausted = w.Known && (w.Exhausted || used >= 1)
		windows = append(windows, w)
	}
	return windows
}
