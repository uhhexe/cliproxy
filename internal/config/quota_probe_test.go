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
