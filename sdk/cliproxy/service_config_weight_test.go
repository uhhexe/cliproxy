package cliproxy

import (
	"context"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestWeightedRoundRobinRoutingSelector(t *testing.T) {
	state := normalizedRoutingRuntimeState(&internalconfig.Config{
		Routing: internalconfig.RoutingConfig{Strategy: "wrr"},
	})
	if state.strategy != "weighted-round-robin" {
		t.Fatalf("strategy = %q, want weighted-round-robin", state.strategy)
	}
	if _, ok := newRoutingSelector(state).(*coreauth.WeightedRoundRobinSelector); !ok {
		t.Fatalf("selector type = %T, want *auth.WeightedRoundRobinSelector", newRoutingSelector(state))
	}
}

func TestServiceRejectsInvalidCredentialWeightConfigCommit(t *testing.T) {
	originalCfg := &internalconfig.Config{}
	service := &Service{cfg: originalCfg}
	invalidWeight := internalconfig.MaxCredentialWeight + 1
	newCfg := &internalconfig.Config{
		VertexCompatAPIKey: []internalconfig.VertexCompatKey{{
			APIKey: "vertex-key",
			Weight: &invalidWeight,
		}},
	}

	if service.applyConfigUpdateWithAuthSynthesis(nil, newCfg, true) {
		t.Fatal("hot config application accepted an invalid credential weight")
	}
	if service.cfg != originalCfg {
		t.Fatal("invalid hot config replaced the active config")
	}
	if service.configSequence != 0 {
		t.Fatalf("config sequence = %d, want 0", service.configSequence)
	}
}

type trackingStoppableSelector struct {
	stopped bool
}

func (s *trackingStoppableSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*coreauth.Auth) (*coreauth.Auth, error) {
	return nil, nil
}

func (s *trackingStoppableSelector) Stop() {
	s.stopped = true
}

func TestApplyManagerConfigStopsReplacedServiceAffinitySelector(t *testing.T) {
	tracking := &trackingStoppableSelector{}
	service := &Service{
		coreManager: coreauth.NewManager(nil, tracking, nil),
	}

	newCfg := &internalconfig.Config{
		Routing: internalconfig.RoutingConfig{
			Strategy: "round-robin",
		},
	}
	commit := configCommit{cfg: newCfg, sequence: 1}
	if !service.applyManagerConfig(context.Background(), commit) {
		t.Fatal("applyManagerConfig failed")
	}

	if !tracking.stopped {
		t.Fatal("expected replaced selector to be stopped during routing config apply")
	}
}

func TestResetSoonestRoutingSelector(t *testing.T) {
	state := normalizedRoutingRuntimeState(&internalconfig.Config{Routing: internalconfig.RoutingConfig{Strategy: " RESET-SOONEST "}})
	if state.strategy != "reset-soonest" {
		t.Fatalf("strategy = %q", state.strategy)
	}
	if _, ok := newRoutingSelector(state).(*coreauth.ResetSoonestSelector); !ok {
		t.Fatalf("selector = %T", newRoutingSelector(state))
	}
}

func TestRoutingStrategyOverride(t *testing.T) {
	for _, tt := range []struct {
		strategy, override, want string
		probe                    bool
	}{
		{"round-robin", "reset-soonest", "reset-soonest", true},
		{"fill-first", "", "fill-first", false},
		{"reset-soonest", "", "reset-soonest", true},
		{"round-robin", "unknown", "round-robin", false},
		{"reset-soonest", "unknown", "reset-soonest", true},
		{"reset-soonest", " RR ", "round-robin", false},
		{"round-robin", "wrr", "weighted-round-robin", false},
	} {
		t.Run(tt.strategy+"/"+tt.override, func(t *testing.T) {
			cfg := &internalconfig.Config{Routing: internalconfig.RoutingConfig{Strategy: tt.strategy, StrategyOverride: tt.override}}
			state := normalizedRoutingRuntimeState(cfg)
			if state.strategy != tt.want {
				t.Fatalf("strategy=%q, want %q", state.strategy, tt.want)
			}
			_, reset := newRoutingSelector(state).(*coreauth.ResetSoonestSelector)
			if reset != (tt.want == "reset-soonest") {
				t.Fatalf("selector=%T", newRoutingSelector(state))
			}
			s := &Service{cfg: cfg}
			if s.quotaProbeEnabled() != tt.probe {
				t.Fatalf("probe=%v, want %v", s.quotaProbeEnabled(), tt.probe)
			}
		})
	}
}
