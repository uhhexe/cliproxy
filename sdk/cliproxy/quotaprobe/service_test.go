package quotaprobe

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type testManager struct {
	a      *auth.Auth
	server *httptest.Server
	calls  atomic.Int32
}

func (m *testManager) List() []*auth.Auth { return []*auth.Auth{m.a} }
func (m *testManager) HttpRequest(ctx context.Context, a *auth.Auth, r *http.Request) (*http.Response, error) {
	m.calls.Add(1)
	r.URL, _ = url.Parse(m.server.URL)
	return m.server.Client().Do(r)
}
func (m *testManager) ObserveQuotaProbe(id string, h http.Header, at time.Time, rejected bool) {
	if rejected {
		m.a.Quota.ClearObservationSignals()
	} else {
		m.a.Quota.ObserveResponseHeadersForProvider(m.a.Provider, h, at)
	}
}

func TestProbeUsage(t *testing.T) {
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if provider == "claude" {
					if r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
						t.Error("missing oauth beta")
					}
					fmt.Fprintf(w, `{"five_hour":{"utilization":75,"resets_at":%q},"seven_day":null,"seven_day_opus":null}`, reset.Format(time.RFC3339))
				} else {
					if r.Header.Get("Chatgpt-Account-Id") != "account" {
						t.Error("missing account")
					}
					fmt.Fprintf(w, `{"rate_limit":{"limit_reached":false,"primary_window":{"used_percent":75,"reset_at":%d,"limit_window_seconds":18000},"secondary_window":null}}`, reset.Unix())
				}
			}))
			defer server.Close()
			m := &testManager{a: &auth.Auth{ID: "a", Provider: provider, Metadata: map[string]any{"account_id": "account"}}, server: server}
			delay := New(m, func() bool { return true }).probe(context.Background(), m.a)
			windows := auth.QuotaWindows(m.a, "")
			if !windows[0].Known || windows[0].UsedFraction != 0.75 || !windows[0].ResetAt.Equal(reset) {
				t.Fatalf("windows: %+v", windows)
			}
			if delay < 9*time.Minute || delay > 11*time.Minute {
				t.Fatalf("delay: %v", delay)
			}
		})
	}
}

func TestProbeRejection(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			m := &testManager{a: &auth.Auth{ID: "a", Provider: "codex", Quota: auth.QuotaState{ObservedAt: time.Now(), Signals: map[string]string{"X-Codex-Primary-Used-Percent": "75"}}}, server: server}
			delay := New(m, func() bool { return true }).probe(context.Background(), m.a)
			if delay != time.Hour || len(m.a.Quota.Signals) != 0 || auth.QuotaWindows(m.a, "")[0].Known {
				t.Fatal("rejection must clear observation and back off for an hour")
			}
		})
	}
}

func TestProbeScheduling(t *testing.T) {
	s := New(nil, func() bool { return true })
	now := time.Now()
	if !s.acquire("a", now) || s.acquire("a", now.Add(time.Hour)) {
		t.Fatal("duplicate in-flight probe")
	}
	s.entries["a"] = entry{next: now.Add(time.Hour)}
	if s.acquire("a", now.Add(59*time.Minute)) || !s.acquire("a", now.Add(time.Hour)) {
		t.Fatal("backoff not respected")
	}
}

func TestProbeDisabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &testManager{a: &auth.Auth{ID: "a", Provider: "codex"}}
	s := New(m, func() bool { cancel(); return false })
	s.Run(ctx)
	if m.calls.Load() != 0 {
		t.Fatal("disabled prober made request")
	}
}
