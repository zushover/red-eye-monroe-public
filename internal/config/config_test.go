package config

import "testing"

func TestLiveConfigurationKeepsHardCaps(t *testing.T) {
	cfg, err := Load("../../config.live.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "live" || cfg.MinimumOrderDollars != 1 || cfg.MaximumOrderDollars != 1 || cfg.DailyLossLimitDollars != 5 || cfg.DailySubmissionLimitDollars != 6 || cfg.MaximumExposureDollars != 6 {
		t.Fatalf("unexpected live limits: %+v", cfg)
	}
}
