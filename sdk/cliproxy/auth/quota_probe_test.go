package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestObserveQuotaProbe(t *testing.T) {
	now := time.Now()
	a := &Auth{ID: "a", Provider: "codex", Quota: QuotaState{Exceeded: true, NextRecoverAt: now.Add(time.Hour)}, ModelStates: map[string]*ModelState{"m": {}}}
	m := &Manager{auths: map[string]*Auth{"a": a}}
	h := http.Header{}
	h.Set("X-Codex-Primary-Used-Percent", "75")
	h.Set("X-Codex-Primary-Reset-After-Seconds", "3600")
	m.ObserveQuotaProbe("a", h, now, false)
	if !QuotaWindows(a, "m")[0].Known || !a.Quota.Exceeded || !a.Quota.NextRecoverAt.Equal(now.Add(time.Hour)) {
		t.Fatal("probe must update windows and preserve cooldown")
	}
	m.ObserveQuotaProbe("a", nil, now.Add(-time.Second), true)
	if !QuotaWindows(a, "m")[0].Known {
		t.Fatal("older probe erased newer observation")
	}
	old := a.Quota.Clone()
	m.ObserveQuotaProbe("a", nil, now.Add(time.Second), true)
	if len(mergeQuotaObservation(a.Quota, old).Signals) != 0 {
		t.Fatal("stale auth clone restored a rejected observation")
	}
	if QuotaWindows(a, "m")[0].Known || QuotaWindows(a, "")[0].Known {
		t.Fatal("rejection must clear model and account observations")
	}
}
