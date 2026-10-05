// Package quotaprobe seeds the same quota observations used by passive routing.
package quotaprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

type Manager interface {
	List() []*auth.Auth
	HttpRequest(context.Context, *auth.Auth, *http.Request) (*http.Response, error)
	ObserveQuotaProbe(string, http.Header, time.Time, bool)
}

type entry struct {
	running bool
	next    time.Time
}

// Service owns scheduling; credential injection, proxies and token refresh remain
// with the existing manager/executors and their auto-refresh worker.
type Service struct {
	manager Manager
	enabled func() bool
	mu      sync.Mutex
	entries map[string]entry
}

func New(manager Manager, enabled func() bool) *Service {
	return &Service{manager: manager, enabled: enabled, entries: make(map[string]entry)}
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var workers sync.WaitGroup
	defer workers.Wait()
	for {
		if ctx.Err() != nil {
			return
		}
		if s.enabled() {
			live := make(map[string]bool)
			for _, a := range s.manager.List() {
				if !eligible(a) {
					continue
				}
				live[a.ID] = true
				if !s.acquire(a.ID, time.Now()) {
					continue
				}
				workers.Add(1)
				go func(a *auth.Auth) {
					defer workers.Done()
					delay := s.probe(ctx, a)
					s.mu.Lock()
					s.entries[a.ID] = entry{next: time.Now().Add(delay)}
					s.mu.Unlock()
				}(a)
			}
			s.mu.Lock()
			for id, e := range s.entries {
				if !live[id] && !e.running {
					delete(s.entries, id)
				}
			}
			s.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func eligible(a *auth.Auth) bool {
	return a != nil && !a.Disabled && a.AuthKind() != auth.AuthKindAPIKey && (a.Provider == "claude" || a.Provider == "codex")
}

func (s *Service) acquire(id string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[id]
	if e.running || now.Before(e.next) {
		return false
	}
	e.running = true
	s.entries[id] = e
	return true
}

func (s *Service) probe(ctx context.Context, a *auth.Auth) time.Duration {
	delay := 10*time.Minute + time.Duration(rand.Int64N(121)-60)*time.Second
	target := "https://api.anthropic.com/api/oauth/usage"
	if a.Provider == "codex" {
		target = "https://chatgpt.com/backend-api/wham/usage"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return delay
	}
	req.Header.Set("Accept", "application/json")
	if a.Provider == "claude" {
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	} else if account, _ := a.Metadata["account_id"].(string); strings.TrimSpace(account) != "" {
		req.Header.Set("Chatgpt-Account-Id", account)
	}
	started := time.Now()
	resp, err := s.manager.HttpRequest(ctx, a, req)
	if err != nil {
		return delay
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.WithError(errClose).Debug("quota probe response close failed")
		}
	}()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		s.manager.ObserveQuotaProbe(a.ID, nil, started, true)
		return time.Hour
	}
	if resp.StatusCode != http.StatusOK {
		return delay
	}
	headers, err := decode(a.Provider, io.LimitReader(resp.Body, 1<<20))
	if err == nil && ctx.Err() == nil {
		s.manager.ObserveQuotaProbe(a.ID, headers, started, false)
	}
	return delay
}

func decode(provider string, body io.Reader) (http.Header, error) {
	h := http.Header{}
	if provider == "claude" {
		var payload map[string]*struct {
			Utilization *float64 `json:"utilization"`
			ResetsAt    string   `json:"resets_at"`
		}
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			return nil, err
		}
		for _, pair := range [][2]string{{"five_hour", "5h"}, {"seven_day", "7d"}, {"seven_day_opus", "7d_oi"}} {
			w := payload[pair[0]]
			if w == nil || w.Utilization == nil {
				continue
			}
			reset, err := time.Parse(time.RFC3339Nano, w.ResetsAt)
			if err != nil {
				continue
			}
			p := "Anthropic-Ratelimit-Unified-" + pair[1] + "-"
			h.Set(p+"Utilization", strconv.FormatFloat(*w.Utilization/100, 'f', -1, 64))
			h.Set(p+"Reset", strconv.FormatInt(reset.Unix(), 10))
		}
	} else {
		type window struct {
			Used    *float64 `json:"used_percent"`
			Reset   *int64   `json:"reset_at"`
			After   *int64   `json:"reset_after_seconds"`
			Seconds int64    `json:"limit_window_seconds"`
		}
		var payload struct {
			RateLimit *struct {
				Reached   bool    `json:"limit_reached"`
				Primary   *window `json:"primary_window"`
				Secondary *window `json:"secondary_window"`
			} `json:"rate_limit"`
		}
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			return nil, err
		}
		if payload.RateLimit == nil {
			return nil, fmt.Errorf("missing rate_limit")
		}
		for name, w := range map[string]*window{"primary": payload.RateLimit.Primary, "secondary": payload.RateLimit.Secondary} {
			if w == nil || w.Used == nil {
				continue
			}
			p := "X-Codex-" + name + "-"
			h.Set(p+"Used-Percent", strconv.FormatFloat(*w.Used, 'f', -1, 64))
			if w.Reset != nil {
				h.Set(p+"Reset-At", strconv.FormatInt(*w.Reset, 10))
			} else if w.After != nil {
				h.Set(p+"Reset-After-Seconds", strconv.FormatInt(*w.After, 10))
			}
			h.Set(p+"Window-Minutes", strconv.FormatInt(w.Seconds/60, 10))
		}
		if len(h) > 0 {
			h.Set("X-Codex-Limit-Reached", strconv.FormatBool(payload.RateLimit.Reached))
		}
	}
	if len(h) == 0 {
		return nil, fmt.Errorf("no quota windows")
	}
	return h, nil
}
