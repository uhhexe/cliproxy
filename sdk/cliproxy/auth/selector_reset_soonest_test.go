package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func resetSoonestAuth(id string, reset time.Time, used string) *Auth {
	a := &Auth{ID: id, Provider: "codex"}
	h := http.Header{}
	h.Set("X-Codex-Primary-Used-Percent", used)
	h.Set("X-Codex-Primary-Reset-At", strconv.FormatInt(reset.Unix(), 10))
	a.Quota.ObserveResponseHeadersForProvider("codex", h, time.Now())
	return a
}

func TestResetSoonest(t *testing.T) {
	now := time.Now()
	a := resetSoonestAuth("a", now.Add(24*time.Hour), "50")
	b := resetSoonestAuth("b", now.Add(4*24*time.Hour), "50")
	c := resetSoonestAuth("c", now.Add(3*time.Hour), "50")
	s := &ResetSoonestSelector{}
	pick := func(want string) {
		t.Helper()
		got, err := s.Pick(context.Background(), "codex", "", cliproxyexecutor.Options{}, []*Auth{b, c, a})
		if err != nil || got == nil || got.ID != want {
			t.Fatalf("want %s, got %+v, %v", want, got, err)
		}
	}
	pick("c")
	c.Quota.Signals["X-Codex-Primary-Used-Percent"] = "100"
	pick("a")
	// The binding window is the most used, not simply the first to reset.
	a.Quota.Signals["X-Codex-Secondary-Used-Percent"] = "90"
	a.Quota.Signals["X-Codex-Secondary-Reset-At"] = strconv.FormatInt(now.Add(5*24*time.Hour).Unix(), 10)
	pick("b")
	a.Quota.ClearObservationSignals()
	b.Quota.ClearObservationSignals()
	c.Quota.ClearObservationSignals()
	pick("a")
	pick("b")
	pick("c")
	pick("a")
}

func TestResetSoonestAllBlocked(t *testing.T) {
	next := time.Now().Add(time.Hour)
	a := &Auth{ID: "a", ModelStates: map[string]*ModelState{"m": {Unavailable: true, NextRetryAfter: next, Quota: QuotaState{Exceeded: true, NextRecoverAt: next}}}}
	_, err := (&ResetSoonestSelector{}).Pick(context.Background(), "codex", "m", cliproxyexecutor.Options{}, []*Auth{a})
	var cooldown *modelCooldownError
	if !errors.As(err, &cooldown) {
		t.Fatalf("expected cooldown error, got %v", err)
	}
}

func TestResetSoonestExhaustionAndTies(t *testing.T) {
	reset := time.Now().Add(time.Hour)
	a := resetSoonestAuth("a", reset, "50")
	b := resetSoonestAuth("b", reset, "50")
	s := &ResetSoonestSelector{}
	got, err := s.Pick(context.Background(), "codex", "", cliproxyexecutor.Options{}, []*Auth{b, a})
	if err != nil || got.ID != "a" {
		t.Fatal("equal resets must tie-break by ID")
	}
	for _, a := range []*Auth{a, b} {
		a.Quota.Signals["X-Codex-Limit-Reached"] = "true"
	}
	_, err = s.Pick(context.Background(), "codex", "", cliproxyexecutor.Options{}, []*Auth{a, b})
	var cooldown *modelCooldownError
	if !errors.As(err, &cooldown) {
		t.Fatalf("expected exhausted pool cooldown, got %v", err)
	}
}

func TestResetSoonestWebsocketPreference(t *testing.T) {
	now := time.Now()
	a := resetSoonestAuth("a", now.Add(time.Hour), "50")
	b := resetSoonestAuth("b", now.Add(2*time.Hour), "50")
	b.Attributes = map[string]string{"websockets": "true"}
	ctx := cliproxyexecutor.WithDownstreamWebsocket(context.Background())
	s := &ResetSoonestSelector{}
	got, err := s.Pick(ctx, "codex", "", cliproxyexecutor.Options{}, []*Auth{a, b})
	if err != nil || got.ID != "b" {
		t.Fatalf("websocket preference lost: %+v, %v", got, err)
	}
	b.Quota.Signals["X-Codex-Primary-Used-Percent"] = "100"
	got, err = s.Pick(ctx, "codex", "", cliproxyexecutor.Options{}, []*Auth{a, b})
	if err != nil || got.ID != "a" {
		t.Fatalf("exhausted websocket credential was selected: %+v, %v", got, err)
	}
}

func TestResetSoonestFableExhaustion(t *testing.T) {
	now := time.Now()
	makeAuth := func(id string, reset time.Time, used string) *Auth {
		a := &Auth{ID: id, Provider: "claude"}
		h := http.Header{}
		h.Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.5")
		h.Set("Anthropic-Ratelimit-Unified-5h-Reset", strconv.FormatInt(reset.Unix(), 10))
		h.Set("Anthropic-Ratelimit-Unified-7d_fable-Utilization", used)
		h.Set("Anthropic-Ratelimit-Unified-7d_fable-Reset", strconv.FormatInt(now.Add(24*time.Hour).Unix(), 10))
		a.Quota.ObserveResponseHeadersForProvider("claude", h, now)
		return a
	}
	a, b := makeAuth("a", now.Add(time.Hour), "1"), makeAuth("b", now.Add(2*time.Hour), "0.25")
	for _, tt := range []struct{ model, want string }{{"claude-fable-5", "b"}, {"claude-sonnet-5", "a"}} {
		got, err := (&ResetSoonestSelector{}).Pick(context.Background(), "claude", tt.model, cliproxyexecutor.Options{}, []*Auth{a, b})
		if err != nil || got == nil || got.ID != tt.want {
			t.Fatalf("model %s: got %+v, %v; want %s", tt.model, got, err, tt.want)
		}
	}
}
