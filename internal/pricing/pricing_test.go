package pricing

import (
	"math"
	"strings"
	"testing"
	"weatherbot/internal/domain"
)

func TestSizeUsesAllInCost(t *testing.T) {
	cfg := domain.Config{BankrollDollars: 15, KellyFraction: .1, MaximumOrderDollars: .5, MaximumMarketRiskFraction: .034, MaximumCityDayRiskFraction: .067}
	if suggestedSize(cfg, .3, .31) != 0 {
		t.Fatal("negative net edge must not get a position")
	}
	if v := suggestedSize(cfg, .9, .1); v > .5 {
		t.Fatal("order exceeds user budget")
	}
}

func TestEqualMinimumAndMaximumSelectsFixedStake(t *testing.T) {
	cfg := domain.Config{BankrollDollars: 15, KellyFraction: .1, MinimumOrderDollars: 1, MaximumOrderDollars: 1, MaximumMarketRiskFraction: .067, MaximumCityDayRiskFraction: .067}
	if got := suggestedSize(cfg, .55, .40); got != 1 {
		t.Fatalf("fixed paper stake=%v, want 1", got)
	}
}

func TestConvergedMarketCannotBecomeEntry(t *testing.T) {
	cfg := domain.Config{Mode: "paper", BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumMarketRiskFraction: .1, MaximumCityDayRiskFraction: .1, KellyFraction: 1, MaximumSpread: .1, MinimumNetEdge: .01}
	e := domain.Event{ID: "e", Markets: []domain.Market{
		{ID: "m1", GroupItemTitle: "23°C", BestBid: .96, BestAsk: .99, Spread: .03, LiquidityNum: 100, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes","no"]`},
		{ID: "m2", GroupItemTitle: "24°C", BestBid: .01, BestAsk: .03, Spread: .02, LiquidityNum: 100, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes2","no2"]`},
	}}
	d := domain.Distribution{Phase: "INTRADAY", RemainingHours: 2, DataQuality: .9, IntegerProbByUnit: map[int]float64{23: .2, 24: .8}}
	for _, s := range PriceEvent(cfg, e, d) {
		if s.NewEntryAllowed || s.Action != "SKIP" || !strings.Contains(s.Reason, "already converged") {
			t.Fatalf("converged event leaked into entries: %+v", s)
		}
	}
}

func TestSeventyPercentConvergenceStopsEntry(t *testing.T) {
	cfg := domain.Config{Mode: "paper", BankrollDollars: 15, MinimumOrderDollars: 1, MaximumOrderDollars: 1, MaximumMarketRiskFraction: .1, MaximumCityDayRiskFraction: .1, KellyFraction: 1, MinimumLiquidity: 1}
	e := domain.Event{ID: "e", Markets: []domain.Market{{ID: "m", GroupItemTitle: "20°C", BestBid: .69, BestAsk: .71, Spread: .02, LiquidityNum: 100, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes","no"]`}}}
	d := domain.Distribution{Phase: "INTRADAY", RemainingHours: 3, DataQuality: .9, IntegerProbByUnit: map[int]float64{20: .9}}
	s := PriceEvent(cfg, e, d)[0]
	if s.NewEntryAllowed || s.EntryWindow != "MARKET_CONVERGED" {
		t.Fatalf("70%% converged market remained open: %+v", s)
	}
}

func TestLateLocalWindowCannotBecomeEntry(t *testing.T) {
	cfg := domain.Config{Mode: "paper", BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumMarketRiskFraction: .1, MaximumCityDayRiskFraction: .1, KellyFraction: 1, MaximumSpread: .1, MinimumNetEdge: .01}
	e := domain.Event{ID: "e", Markets: []domain.Market{{ID: "m", GroupItemTitle: "20°C", BestBid: .01, BestAsk: .02, Spread: .01, LiquidityNum: 100, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes","no"]`}}}
	d := domain.Distribution{Phase: "LATE_DAY", DataQuality: .9, IntegerProbByUnit: map[int]float64{20: 1}}
	s := PriceEvent(cfg, e, d)[0]
	if s.NewEntryAllowed || s.EntryWindow != "TOO_LATE" || s.Action != "SKIP" {
		t.Fatalf("late event leaked into entries: %+v", s)
	}
}

func TestHistoricalConvergenceDeadlineStopsNewEntry(t *testing.T) {
	cfg := domain.Config{Mode: "paper", BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumMarketRiskFraction: .1, MaximumCityDayRiskFraction: .1, KellyFraction: 1, MaximumSpread: .1, MinimumNetEdge: .01}
	e := domain.Event{ID: "e", Markets: []domain.Market{{ID: "m", GroupItemTitle: "20°C", BestBid: .01, BestAsk: .02, Spread: .01, LiquidityNum: 100, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes","no"]`}}}
	d := domain.Distribution{Phase: "INTRADAY", LocalMinute: 800, EntryDeadlineMinute: 785, RemainingHours: 2, DataQuality: .9, IntegerProbByUnit: map[int]float64{20: 1}}
	s := PriceEvent(cfg, e, d)[0]
	if s.NewEntryAllowed || s.EntryWindow != "HISTORICAL_CONVERGENCE_WINDOW" || s.Action != "SKIP" {
		t.Fatalf("historically late event leaked into entries: %+v", s)
	}
}

func TestCautiousWindowStaysOpenOnlyBelowSixtyFivePercent(t *testing.T) {
	cfg := domain.Config{Mode: "live", BankrollDollars: 15, MinimumOrderDollars: 1, MaximumOrderDollars: 1, MaximumMarketRiskFraction: .1, MaximumCityDayRiskFraction: .1, KellyFraction: 1, MaximumSpread: .1, MinimumLiquidity: 1, MinimumNetEdge: .06}
	e := domain.Event{ID: "e", Markets: []domain.Market{{ID: "m", GroupItemTitle: "20°C", BestBid: .59, BestAsk: .61, Spread: .02, LiquidityNum: 100, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes","no"]`}}}
	d := domain.Distribution{Phase: "INTRADAY", LocalMinute: 800, EntryCautionMinute: 785, EntryDeadlineMinute: 870, RemainingHours: 2, DataQuality: .9, CalibrationReady: true, ProbabilityStatus: "CALIBRATED", IntegerProbByUnit: map[int]float64{20: 1}}
	s := PriceEvent(cfg, e, d)[0]
	if !s.NewEntryAllowed || s.EntryWindow != "CAUTIOUS_CONVERGENCE_WINDOW" {
		t.Fatalf("qualified market should remain observable in cautious window: %+v", s)
	}
	e.Markets[0].BestBid, e.Markets[0].BestAsk = .65, .67
	s = PriceEvent(cfg, e, d)[0]
	if s.NewEntryAllowed || s.EntryWindow != "CAUTIOUS_WINDOW_BLOCKED" {
		t.Fatalf("65%% concentrated market must close during cautious window: %+v", s)
	}
}

func TestFreshBookRepricingCanCancelStaleCandidate(t *testing.T) {
	cfg := domain.Config{Mode: "paper", BankrollDollars: 15, MinimumOrderDollars: 1, MaximumOrderDollars: 1, MaximumMarketRiskFraction: .067, MaximumCityDayRiskFraction: .067, ExpectedSlippage: .01, UncertaintyBuffer: .025, SettlementRiskBuffer: .015, MinimumNetEdge: .12}
	s := domain.Signal{Action: "BUY_PAPER", ModelProbability: .5, BestAsk: .1, BestBid: .08, ConvergenceCapture: .5}
	RepriceCandidate(cfg, domain.Distribution{Phase: "INTRADAY"}, &s, .44, .45)
	if s.Action != "SKIP" || s.ExpectedTradeEdge >= 0 {
		t.Fatalf("stale candidate survived adverse fresh quote: %+v", s)
	}
}

func TestMarketAnchoredPaperExplorerUsesThreePointTradeThreshold(t *testing.T) {
	cfg := domain.Config{Mode: "paper", BankrollDollars: 15, MinimumOrderDollars: 1, MaximumOrderDollars: 1, MaximumMarketRiskFraction: .067, MaximumCityDayRiskFraction: .067, KellyFraction: 1, MaximumSpread: .06, MinimumLiquidity: 75, ExpectedSlippage: .002, UncertaintyBuffer: .025, SettlementRiskBuffer: .015}
	e := domain.Event{ID: "e", Markets: []domain.Market{{ID: "m", GroupItemTitle: "20°C", BestBid: .10, BestAsk: .11, Spread: .01, LiquidityNum: 100, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes","no"]`}}}
	d := domain.Distribution{Phase: "INTRADAY", RemainingHours: 2, DataQuality: .9, IntegerProbByUnit: map[int]float64{20: .70}}
	s := PriceEvent(cfg, e, d)[0]
	if s.ExpectedTradeEdge < .03 || s.Action != "BUY_PAPER" || s.ModelProbability >= s.WeatherProbability || math.Abs(s.MarketConsensus-.105) > 1e-9 {
		t.Fatalf("market-anchored 3pp paper candidate was not selected: %+v", s)
	}
}

func TestPaperTradeThresholdsCanRunWiderResearchRange(t *testing.T) {
	cfg := domain.Config{PaperIntradayTradeEdge: .015, PaperPredayTradeEdge: .03}
	if got := paperTradeThreshold(cfg, "INTRADAY"); math.Abs(got-.015) > 1e-9 {
		t.Fatalf("unexpected intraday threshold: %v", got)
	}
	if got := paperTradeThreshold(cfg, "PRE_DAY"); math.Abs(got-.03) > 1e-9 {
		t.Fatalf("unexpected pre-day threshold: %v", got)
	}
}

func TestUnstablePaperWeatherAddsPenaltyInsteadOfBlanketSkip(t *testing.T) {
	cfg := domain.Config{Mode: "paper", BankrollDollars: 15, MinimumOrderDollars: 1, MaximumOrderDollars: 1, MaximumMarketRiskFraction: .067, MaximumCityDayRiskFraction: .067, KellyFraction: 1, MaximumSpread: .06, MinimumLiquidity: 75, ExpectedSlippage: .002, UncertaintyBuffer: .025, SettlementRiskBuffer: .015}
	e := domain.Event{ID: "e", Markets: []domain.Market{{ID: "m", GroupItemTitle: "20°C", BestBid: .10, BestAsk: .12, Spread: .02, LiquidityNum: 100, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes","no"]`}}}
	d := domain.Distribution{Phase: "INTRADAY", RemainingHours: 2, DataQuality: .9, UnstableWeather: true, IntegerProbByUnit: map[int]float64{20: .90}}
	s := PriceEvent(cfg, e, d)[0]
	if strings.Contains(s.Reason, "trajectory is unstable") || s.Action != "BUY_PAPER" {
		t.Fatalf("unstable paper weather should be penalized, not blanket-blocked: %+v", s)
	}
}

func TestFreshRepriceKeepsUnstableWeatherPenalty(t *testing.T) {
	cfg := domain.Config{Mode: "paper", BankrollDollars: 15, MinimumOrderDollars: 1, MaximumOrderDollars: 1, MaximumMarketRiskFraction: .067, MaximumCityDayRiskFraction: .067, ExpectedSlippage: .002, UncertaintyBuffer: .025, SettlementRiskBuffer: .015}
	s := domain.Signal{Action: "BUY_PAPER", WeatherProbability: .80, WeatherWeight: .30, ModelProbability: .30, BestAsk: .12, BestBid: .10, ConvergenceCapture: .5}
	RepriceCandidate(cfg, domain.Distribution{Phase: "INTRADAY", UnstableWeather: true}, &s, .10, .12)
	want := s.ModelProbability - .12 - .002 - .025 - .015 - .03
	if s.Action != "BUY_PAPER" || s.NetEdge-want > 1e-9 || want-s.NetEdge > 1e-9 {
		t.Fatalf("fresh repricing lost dynamic weather penalty: %+v", s)
	}
}

func TestPaperTurnoverFilterRejectsCheapTailTicket(t *testing.T) {
	cfg := domain.Config{Mode: "paper", BankrollDollars: 15, MinimumOrderDollars: 1, MaximumOrderDollars: 1, MaximumMarketRiskFraction: .067, MaximumCityDayRiskFraction: .067, KellyFraction: 1, MaximumSpread: .06, MinimumLiquidity: 75, ExpectedSlippage: .002, UncertaintyBuffer: .025, SettlementRiskBuffer: .015}
	e := domain.Event{ID: "e", Markets: []domain.Market{{ID: "m", GroupItemTitle: "20°C", BestBid: .03, BestAsk: .05, Spread: .02, LiquidityNum: 100, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes","no"]`}}}
	d := domain.Distribution{Phase: "INTRADAY", RemainingHours: 2, DataQuality: .9, IntegerProbByUnit: map[int]float64{20: .90}}
	s := PriceEvent(cfg, e, d)[0]
	if s.Action != "SKIP" || !strings.Contains(s.Reason, "10c-70c") {
		t.Fatalf("cheap tail ticket passed turnover filter: %+v", s)
	}
}

func TestRankAlignedSettlementStrategyIsPaperOnlyInLiveMode(t *testing.T) {
	cfg := domain.Config{Mode: "live", BankrollDollars: 15, MinimumOrderDollars: 1, MaximumOrderDollars: 1, MaximumMarketRiskFraction: .067, MaximumCityDayRiskFraction: .067, KellyFraction: 1, MaximumSpread: .06, MinimumLiquidity: 75, ExpectedSlippage: .002, UncertaintyBuffer: .025, SettlementRiskBuffer: .015, MinimumNetEdge: .10}
	e := domain.Event{ID: "e", Markets: []domain.Market{
		{ID: "top", GroupItemTitle: "30°C", BestBid: .20, BestAsk: .22, Spread: .02, LiquidityNum: 100, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes","no"]`},
		{ID: "other", GroupItemTitle: "29°C", BestBid: .10, BestAsk: .12, Spread: .02, LiquidityNum: 100, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes2","no2"]`},
	}}
	d := domain.Distribution{Phase: "INTRADAY", RemainingHours: 3, DataQuality: .9, IntegerProbByUnit: map[int]float64{30: .8, 29: .2}}
	signals := PriceEvent(cfg, e, d)
	var selected *domain.Signal
	for i := range signals {
		if signals[i].MarketID == "top" {
			selected = &signals[i]
			break
		}
	}
	if selected == nil || selected.Action != "BUY_PAPER" || selected.Strategy != "RANK_ALIGNED_SETTLEMENT" {
		t.Fatalf("rank-aligned strategy must remain simulation-only: %+v", selected)
	}
}

func TestEarlyThreeSourceAlignmentCreatesPaperResearchPosition(t *testing.T) {
	cfg := domain.Config{Mode: "live", BankrollDollars: 15, MinimumOrderDollars: 1, MaximumOrderDollars: 1, MaximumMarketRiskFraction: .067, MaximumCityDayRiskFraction: .067, KellyFraction: 1, MaximumSpread: .06, MinimumLiquidity: 75, ExpectedSlippage: .002, UncertaintyBuffer: .025, SettlementRiskBuffer: .015, MinimumNetEdge: .10}
	e := domain.Event{ID: "paris", Markets: []domain.Market{
		{ID: "32", GroupItemTitle: "32°C", BestBid: .45, BestAsk: .48, Spread: .03, LiquidityNum: 200, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes32","no32"]`},
		{ID: "31", GroupItemTitle: "31°C", BestBid: .09, BestAsk: .11, Spread: .02, LiquidityNum: 200, OutcomesJSON: `["Yes","No"]`, ClobTokenIDsJSON: `["yes31","no31"]`},
	}}
	d := domain.Distribution{Phase: "INTRADAY", LocalMinute: 408, RemainingHours: 8, DataQuality: .9, SourcePeaksC: map[string]float64{"ecmwf": 32.5, "gfs": 32.0, "best": 32.2}, IntegerProbByUnit: map[int]float64{32: .44, 31: .30, 33: .13}}
	signals := PriceEvent(cfg, e, d)
	for _, s := range signals {
		if s.MarketID == "32" {
			if s.Action != "BUY_PAPER" || s.Strategy != "SOURCE_ALIGNED_EARLY" {
				t.Fatalf("Paris-style source alignment should enter research simulation only: %+v", s)
			}
			return
		}
	}
	t.Fatal("32C signal missing")
}
