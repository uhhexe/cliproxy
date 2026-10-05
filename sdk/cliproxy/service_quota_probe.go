package cliproxy

import (
	"context"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/quotaprobe"
)

func (s *Service) runQuotaProbe(ctx context.Context) {
	if s.coreManager == nil {
		return
	}
	quotaprobe.New(s.coreManager, func() bool {
		s.cfgMu.RLock()
		defer s.cfgMu.RUnlock()
		return s.cfg != nil && !s.cfg.Home.Enabled && (s.cfg.QuotaProbe.Enabled || strings.EqualFold(strings.TrimSpace(s.cfg.Routing.Strategy), "reset-soonest"))
	}).Run(ctx)
}
