package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"weatherbot/internal/domain"
)

func Load(path string) (domain.Config, error) {
	var c domain.Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse config: %w", err)
	}
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	if c.Mode != "paper" && c.Mode != "live" {
		return c, fmt.Errorf("mode must be paper or live")
	}
	// Older configs used the loss limit for both controls. Preserve that behavior
	// only when the new submission limit is omitted.
	if c.DailySubmissionLimitDollars == 0 {
		c.DailySubmissionLimitDollars = c.DailyLossLimitDollars
	}
	if c.MaximumPaperPositions == 0 {
		c.MaximumPaperPositions = 10
	}
	if c.PaperIntradayTradeEdge == 0 {
		c.PaperIntradayTradeEdge = .015
	}
	if c.PaperPredayTradeEdge == 0 {
		c.PaperPredayTradeEdge = .03
	}
	if c.PaperRotationMinProfit == 0 {
		c.PaperRotationMinProfit = .02
	}
	if c.PaperRotationMinUpgrade == 0 {
		c.PaperRotationMinUpgrade = .015
	}
	for _, v := range []float64{c.MinimumOrderDollars, c.MaximumOrderDollars, c.BankrollDollars, c.MaximumExposureDollars, c.DailyLossLimitDollars, c.DailySubmissionLimitDollars, c.MinimumNetEdge, c.KellyFraction, c.MaximumMarketRiskFraction, c.MaximumCityDayRiskFraction, c.ExpectedSlippage, c.UncertaintyBuffer, c.SettlementRiskBuffer, c.PaperIntradayTradeEdge, c.PaperPredayTradeEdge, c.PaperRotationMinProfit, c.PaperRotationMinUpgrade} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return c, fmt.Errorf("invalid numeric configuration")
		}
	}
	if c.MinimumOrderDollars < .5 || c.MaximumOrderDollars < c.MinimumOrderDollars || c.BankrollDollars > 15 || c.BankrollDollars < c.MaximumOrderDollars || c.MaximumExposureDollars > c.BankrollDollars || c.MaximumExposureDollars <= 0 || c.DailyLossLimitDollars <= 0 || c.DailySubmissionLimitDollars <= 0 || (c.Mode == "paper" && c.DailySubmissionLimitDollars > 2*c.BankrollDollars) || c.MaximumPaperPositions < 1 || c.MaximumPaperPositions > 10 || c.PaperIntradayTradeEdge <= 0 || c.PaperPredayTradeEdge <= 0 || c.PaperRotationMinProfit <= 0 || c.PaperRotationMinUpgrade <= 0 || c.LookaheadDays < 1 || c.LookaheadDays > 7 {
		return c, fmt.Errorf("invalid $15 capital/order limits")
	}
	if c.Mode == "live" && (c.MinimumOrderDollars != 1 || c.MaximumOrderDollars != 1 || c.DailyLossLimitDollars > 5 || c.DailySubmissionLimitDollars > 6 || c.MaximumExposureDollars > 6) {
		return c, fmt.Errorf("live limits require exact $1 orders, daily submission cap <= $6, and exposure <= $6")
	}
	if c.PollIntervalSeconds < 15 || c.HTTPTimeoutSeconds < 1 {
		return c, fmt.Errorf("unsafe polling or timeout configuration")
	}
	if c.MinimumNetEdge < 0 || c.MinimumNetEdge >= 1 {
		return c, fmt.Errorf("minimum_net_edge must be between 0 inclusive and 1")
	}
	return c, nil
}

func LoadStations(path string) (map[string]domain.Station, error) {
	var rows []domain.Station
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read stations: %w", err)
	}
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("parse stations: %w", err)
	}
	out := make(map[string]domain.Station, len(rows))
	for _, s := range rows {
		s.ICAO = strings.ToUpper(s.ICAO)
		if s.ICAO == "" || s.Timezone == "" || s.BaseSigmaC <= 0 {
			return nil, fmt.Errorf("invalid station entry: %q", s.ICAO)
		}
		out[s.ICAO] = s
	}
	return out, nil
}

func LoadStationList(path string) ([]domain.Station, error) {
	var out []domain.Station
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read research stations: %w", err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parse research stations: %w", err)
	}
	seen := map[string]bool{}
	for i := range out {
		out[i].ICAO = strings.ToUpper(out[i].ICAO)
		if out[i].ICAO == "" || out[i].Timezone == "" || out[i].Latitude < -90 || out[i].Latitude > 90 || out[i].Longitude < -180 || out[i].Longitude > 180 || seen[out[i].ICAO] {
			return nil, fmt.Errorf("invalid research station entry: %q", out[i].ICAO)
		}
		seen[out[i].ICAO] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ICAO < out[j].ICAO })
	return out, nil
}
