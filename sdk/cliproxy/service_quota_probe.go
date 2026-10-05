package cliproxy

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/quotaprobe"
)

func (s *Service) runQuotaProbe(ctx context.Context) {
	if s.coreManager == nil {
		return
	}
	quotaprobe.New(s.coreManager, s.quotaProbeEnabled).Run(ctx)
}

func (s *Service) quotaProbeEnabled() bool {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg != nil && !s.cfg.Home.Enabled && (s.cfg.QuotaProbe.Enabled || normalizedRoutingRuntimeState(s.cfg).strategy == "reset-soonest")
}
