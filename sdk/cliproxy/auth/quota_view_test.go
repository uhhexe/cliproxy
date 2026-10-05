package auth

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestQuotaView(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name, provider string
		age            time.Duration
		reset          time.Time
		known          bool
	}{
		{"claude", "claude", 0, now.Add(time.Hour), true},
		{"codex", "codex", 0, now.Add(time.Hour), true},
		{"stale", "claude", 7 * time.Hour, now.Add(time.Hour), false},
		{"passed reset", "codex", 0, now.Add(-time.Second), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.provider == "claude" {
				h.Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.75")
				h.Set("Anthropic-Ratelimit-Unified-5h-Reset", strconv.FormatInt(tt.reset.Unix(), 10))
			} else {
				h.Set("X-Codex-Primary-Used-Percent", "75")
				h.Set("X-Codex-Primary-Reset-At", strconv.FormatInt(tt.reset.Unix(), 10))
			}
			a := &Auth{Provider: tt.provider}
			a.Quota.ObserveResponseHeadersForProvider(tt.provider, h, now.Add(-tt.age))
			w := quotaWindowsAt(a, "", now)[0]
			if w.Known != tt.known || w.UsedFraction != 0.75 || !w.ResetAt.Equal(tt.reset) {
				t.Fatalf("unexpected window: %+v", w)
			}
		})
	}
	t.Run("model preference and relative reset", func(t *testing.T) {
		a := &Auth{Provider: "codex", ModelStates: map[string]*ModelState{"model": {Quota: QuotaState{ObservedAt: now, Signals: map[string]string{"X-Codex-Primary-Used-Percent": "100", "X-Codex-Primary-Reset-After-Seconds": "3600"}}}}}
		w := quotaWindowsAt(a, "model", now)[0]
		if !w.Known || !w.Exhausted || !w.ResetAt.Equal(now.Add(time.Hour)) {
			t.Fatalf("unexpected window: %+v", w)
		}
		if quotaWindowsAt(a, "other", now)[0].Known {
			t.Fatal("missing observation should be unknown")
		}
	})
}
