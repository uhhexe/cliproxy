package config

import "testing"

func TestQuotaProbeConfig(t *testing.T) {
	for _, raw := range []string{"quota-probe: {enabled: true}\n", "config-version: 8\nquota-probe: {enabled: true}\n"} {
		if err := ValidateV8Config([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		cfg, err := ParseConfigBytes([]byte(raw))
		if err != nil || !cfg.QuotaProbe.Enabled {
			t.Fatalf("quota probe flag lost: %+v, %v", cfg, err)
		}
	}
	cfg, err := ParseConfigBytes([]byte("routing: {strategy: round-robin}\n"))
	if err != nil || cfg.QuotaProbe.Enabled {
		t.Fatal("quota probing should default off")
	}
}

func TestRoutingStrategyOverrideConfig(t *testing.T) {
	for _, prefix := range []string{"", "config-version: 8\n"} {
		raw := []byte(prefix + "routing: {strategy: round-robin, strategy-override: reset-soonest}\n")
		if err := ValidateV8Config(raw); err != nil {
			t.Fatal(err)
		}
		cfg, err := ParseConfigBytes(raw)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Routing.Strategy != "round-robin" || cfg.Routing.StrategyOverride != "reset-soonest" {
			t.Fatalf("routing=%+v", cfg.Routing)
		}
	}
	cfg, err := ParseConfigBytes([]byte("routing: {strategy: fill-first}\n"))
	if err != nil || cfg.Routing.StrategyOverride != "" {
		t.Fatal("override should default empty")
	}
}
