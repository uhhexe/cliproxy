package auth

import (
	"net/http"
	"time"
)

// ObserveQuotaProbe updates observations only, never request outcomes or cooldowns.
// A response that started before a newer passive observation cannot overwrite it.
func (m *Manager) ObserveQuotaProbe(id string, headers http.Header, started time.Time, rejected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.auths[id]
	if a == nil {
		return
	}
	update := func(q *QuotaState) {
		if q.ObservedAt.After(started) {
			return
		}
		if rejected {
			q.ClearObservationSignals()
			// Keep the invalidation watermark so stale auth clones cannot restore it.
			q.ObservedAt = started
		} else {
			q.ObserveResponseHeadersForProvider(a.Provider, headers, started)
		}
	}
	update(&a.Quota)
	for _, state := range a.ModelStates {
		if state != nil {
			update(&state.Quota)
		}
	}
}
