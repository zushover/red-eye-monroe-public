package pricing

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"weatherbot/internal/client"
	"weatherbot/internal/domain"
	"weatherbot/internal/rules"
)

func PriceEvent(cfg domain.Config, e domain.Event, dist domain.Distribution) []domain.Signal {
	now := time.Now().UTC()
	convergence := MarketConvergence(e.Markets)
	entryWindow := "OPEN"
	entryAllowed := true
	cautiousWindow := dist.Phase == "INTRADAY" && dist.EntryCautionMinute > 0 && dist.LocalMinute >= dist.EntryCautionMinute && (dist.EntryDeadlineMinute <= 0 || dist.LocalMinute < dist.EntryDeadlineMinute)
	if cfg.Mode == "paper" && cfg.PaperEntriesPaused {
		entryWindow, entryAllowed = "PAPER_PAUSED", false
	} else if convergence >= .70 {
		entryWindow, entryAllowed = "MARKET_CONVERGED", false
	} else if dist.Phase == "INTRADAY" && dist.EntryDeadlineMinute > 0 && dist.LocalMinute >= dist.EntryDeadlineMinute {
		entryWindow, entryAllowed = "HISTORICAL_CONVERGENCE_WINDOW", false
	} else if cautiousWindow && (convergence >= .65 || dist.UnstableWeather || !dist.CalibrationReady || dist.RemainingHours < .75) {
		entryWindow, entryAllowed = "CAUTIOUS_WINDOW_BLOCKED", false
	} else if dist.Phase == "LATE_DAY" || dist.Phase == "AWAITING_SETTLEMENT" || (dist.Phase == "INTRADAY" && dist.RemainingHours < .5) {
		entryWindow, entryAllowed = "TOO_LATE", false
	} else if cautiousWindow {
		entryWindow = "CAUTIOUS_CONVERGENCE_WINDOW"
	}
	threshold := cfg.MinimumNetEdge
	if cfg.Mode == "live" {
		threshold = 0
	} else if dist.Phase == "PRE_DAY" {
		threshold = math.Max(threshold, .10)
	}
	if dist.Phase == "LATE_DAY" {
		threshold = math.Max(threshold, .15)
	}
	if cautiousWindow && cfg.Mode != "live" {
		threshold = math.Max(threshold, .09)
	}
	out := make([]domain.Signal, 0, len(e.Markets))
	weight := weatherWeight(dist)
	for _, m := range e.Markets {
		b, err := rules.ParseBucket(m.GroupItemTitle)
		if err != nil {
			continue
		}
		weatherP := rules.BucketProbability(b, dist.IntegerProbByUnit)
		ask := m.BestAsk
		consensus := marketConsensusProbability(m.BestBid, ask)
		p := consensus + weight*(weatherP-consensus)
		p = math.Max(0, math.Min(1, p))
		consensusGap := p - consensus
		tokens, _ := client.DecodeStringArray(m.ClobTokenIDsJSON)
		token := ""
		outcomes, _ := client.DecodeStringArray(m.OutcomesJSON)
		for i, outcome := range outcomes {
			if outcome == "Yes" && i < len(tokens) {
				token = tokens[i]
			}
		}
		feeRate := 0.0
		if m.FeesEnabled {
			feeRate = cfg.DefaultWeatherTakerFeeRate
			if m.FeeSchedule != nil && m.FeeSchedule.Rate > 0 {
				feeRate = m.FeeSchedule.Rate
			}
		}
		fee := feeRate * ask * (1 - ask)
		gross := p - ask
		weatherPenalty := 0.0
		if dist.UnstableWeather && cfg.Mode == "paper" {
			weatherPenalty = .03
		}
		net := gross - fee - cfg.ExpectedSlippage - cfg.UncertaintyBuffer - cfg.SettlementRiskBuffer - weatherPenalty
		action := "WATCH"
		reasons := []string{}
		addSkip := func(reason string) { action = "SKIP"; reasons = append(reasons, reason) }
		if ask <= 0 || ask >= 1 {
			addSkip("no executable ask")
		}
		if cfg.Mode == "paper" && (ask < .10 || ask > .70) {
			addSkip("paper turnover filter requires 10c-70c entry ask")
		}
		if cfg.Mode == "paper" && consensusGap < .035 {
			addSkip("market-anchored model edge below 3.5pp")
		}
		if cfg.Mode == "live" && consensus < .10 {
			addSkip("live market consensus below 10% hard floor")
		}
		spreadLimit := AllowedSpread(cfg, ask)
		if m.Spread > spreadLimit {
			addSkip("spread too wide")
		}
		if ask-m.BestBid > spreadLimit {
			addSkip("computed spread too wide")
		}
		if dist.UnstableWeather && cfg.Mode != "paper" {
			addSkip("weather or model trajectory is unstable")
		}
		if !entryAllowed {
			if entryWindow == "PAPER_PAUSED" {
				addSkip("paper entries paused for model rebuild")
			} else if entryWindow == "MARKET_CONVERGED" {
				addSkip("market already converged; no new entry")
			} else if entryWindow == "HISTORICAL_CONVERGENCE_WINDOW" {
				addSkip("past the historically observed pre-convergence entry window")
			} else {
				addSkip("local heating window is effectively over")
			}
		}
		if m.LiquidityNum < cfg.MinimumLiquidity {
			addSkip("insufficient liquidity")
		}
		if dist.DataQuality < 0.65 && !(dist.Phase == "PRE_DAY" && dist.DataQuality >= .60) {
			addSkip("weather data quality too low")
		}
		if !dist.CalibrationReady && cfg.Mode != "paper" {
			addSkip("station model is not historically calibrated")
		}
		if dist.Phase == "AWAITING_SETTLEMENT" {
			addSkip("local observation day has ended")
		}
		if m.BestBid > ask || m.BestBid <= 0 || token == "" {
			addSkip("invalid or one-sided market")
		}
		// Size from conservative probability and all-in cost, not gross edge.
		conservativeP := math.Max(0, p-cfg.UncertaintyBuffer-cfg.SettlementRiskBuffer-weatherPenalty)
		conservativeValue := math.Max(0, conservativeP-fee-cfg.ExpectedSlippage)
		potentialMove := conservativeValue - ask
		// Separate terminal fair value from a short-horizon convergence trade.
		// Without calibrated price histories we assume only a conservative
		// fraction of the value gap is captured before the local peak.
		capture := .25
		if dist.Phase == "MORNING" {
			capture = .35
		} else if dist.Phase == "INTRADAY" {
			capture = math.Max(.25, math.Min(.65, .25+.5/(1+math.Max(0, dist.RemainingHours))))
		}
		spread := math.Max(0, ask-m.BestBid)
		expectedExitBid := m.BestBid + capture*math.Max(0, conservativeValue-m.BestBid)
		expectedExitBid = math.Max(0, expectedExitBid-spread*.25)
		exitFee := feeRate * expectedExitBid * (1 - expectedExitBid)
		expectedTradeEdge := expectedExitBid - ask - exitFee - cfg.ExpectedSlippage
		size := suggestedSize(cfg, conservativeP, ask+fee+cfg.ExpectedSlippage)
		if size < cfg.MinimumOrderDollars {
			if conservativeValue <= ask {
				addSkip("negative expected value after costs")
			} else {
				addSkip("risk-sized order below $0.50 minimum")
			}
			size = 0
		}
		if cfg.Mode != "live" && expectedTradeEdge <= 0 {
			addSkip("no positive modeled bid convergence before exit")
		}
		if cautiousWindow && cfg.Mode != "live" && expectedTradeEdge < .03 {
			addSkip("cautious window requires at least 3pp expected executable exit edge")
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "edge below phase threshold")
			if cfg.Mode == "paper" && !dist.CalibrationReady {
				reasons = append(reasons, "uncalibrated research simulation only")
			}
		}
		out = append(out, domain.Signal{
			EventID: e.ID, EventTitle: e.Title, MarketID: m.ID, MarketQuestion: m.Question,
			Bucket: m.GroupItemTitle, TokenID: token, WeatherProbability: weatherP, MarketConsensus: consensus, WeatherWeight: weight, ModelProbability: p, ConsensusGap: consensusGap, BestBid: m.BestBid, BestAsk: ask,
			GrossEdge: gross, FeePerShare: fee, NetEdge: net, SuggestedDollars: size,
			MarketConvergence: convergence, ConservativeValue: conservativeValue, PotentialMove: potentialMove,
			ExpectedExitBid: expectedExitBid, ExpectedTradeEdge: expectedTradeEdge, ConvergenceCapture: capture,
			NewEntryAllowed: entryAllowed, EntryWindow: entryWindow,
			Action: action, Reason: strings.Join(reasons, "; "), Reasons: reasons, GeneratedAt: now,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NetEdge > out[j].NetEdge })
	for i := range out {
		if out[i].NewEntryAllowed && out[i].Action == "WATCH" && out[i].NetEdge > 0 && out[i].ExpectedTradeEdge > 0 {
			phaseWeight := 1.0
			if dist.Phase == "PRE_DAY" {
				phaseWeight = .7
			} else if dist.Phase == "OVERNIGHT" {
				phaseWeight = .75
			} else if dist.Phase == "MORNING" {
				phaseWeight = .9
			}
			out[i].ConvergenceScore = 100 * out[i].ExpectedTradeEdge * phaseWeight * math.Max(.1, 1-out[i].MarketConvergence) * dist.DataQuality
		}
	}
	for i := range out {
		qualifies := out[i].NetEdge > threshold
		tradeThreshold := paperTradeThreshold(cfg, dist.Phase)
		if cfg.Mode == "paper" {
			qualifies = out[i].NetEdge > 0 && out[i].ExpectedTradeEdge >= tradeThreshold
		}
		if out[i].Action == "WATCH" && qualifies && out[i].SuggestedDollars > 0 {
			out[i].Strategy = "CONVERGENCE"
			if cfg.Mode == "live" {
				out[i].Action = "BUY_LIVE"
				out[i].Reason = fmt.Sprintf("live candidate: calibrated net edge %.1fpp meets %.1fpp threshold", out[i].NetEdge*100, threshold*100)
			} else {
				out[i].Action = "BUY_PAPER"
				out[i].Reason = fmt.Sprintf("research convergence simulation: expected bid-side edge %.1fpp meets %.1fpp threshold; not validated profit", out[i].ExpectedTradeEdge*100, tradeThreshold*100)
			}
			out[i].Reasons = []string{out[i].Reason}
			break
		}
	}
	// A separate paper-only strategy may hold the mutually top-ranked bucket
	// through resolution. Ranking agreement is not enough by itself: it still
	// requires positive value after buffers, usable liquidity, and an open local
	// entry window. It never emits BUY_LIVE.
	hasSelected := false
	for i := range out {
		if out[i].Action == "BUY_PAPER" || out[i].Action == "BUY_LIVE" {
			hasSelected = true
			break
		}
	}
	if !hasSelected && entryAllowed && !dist.UnstableWeather && dist.DataQuality >= .65 {
		weatherTop, marketTop := -1, -1
		for i := range out {
			if weatherTop < 0 || out[i].WeatherProbability > out[weatherTop].WeatherProbability {
				weatherTop = i
			}
			if marketTop < 0 || out[i].MarketConsensus > out[marketTop].MarketConsensus {
				marketTop = i
			}
		}
		if weatherTop >= 0 && weatherTop == marketTop {
			s := &out[weatherTop]
			var market *domain.Market
			for i := range e.Markets {
				if e.Markets[i].ID == s.MarketID {
					market = &e.Markets[i]
					break
				}
			}
			if market != nil && s.BestAsk >= .10 && s.BestAsk <= .70 && s.BestBid > 0 &&
				s.BestAsk-s.BestBid <= AllowedSpread(cfg, s.BestAsk) && market.LiquidityNum >= cfg.MinimumLiquidity &&
				s.NetEdge >= .06 && s.SuggestedDollars >= cfg.MinimumOrderDollars {
				s.Action = "BUY_PAPER"
				s.Strategy = "RANK_ALIGNED_SETTLEMENT"
				s.Reason = "paper-only rank-aligned settlement value; never eligible for live execution"
				s.Reasons = []string{s.Reason}
			}
		}
	}
	// Explore the specific early-session hypothesis separately from validated
	// value trading: when all three independent forecast peaks land in the same
	// top-ranked contract, permit a small paper position even when costs leave a
	// marginal (-3pp) terminal edge. This can teach the staged backtest whether
	// early source unanimity predicts price convergence. It is never live entry.
	hasSelected = false
	for i := range out {
		if out[i].Action == "BUY_PAPER" || out[i].Action == "BUY_LIVE" {
			hasSelected = true
			break
		}
	}
	if !hasSelected && entryAllowed && !dist.UnstableWeather && dist.DataQuality >= .65 && earlySourceWindow(dist) {
		weatherTop, marketTop := -1, -1
		for i := range out {
			if weatherTop < 0 || out[i].WeatherProbability > out[weatherTop].WeatherProbability {
				weatherTop = i
			}
			if marketTop < 0 || out[i].MarketConsensus > out[marketTop].MarketConsensus {
				marketTop = i
			}
		}
		if weatherTop >= 0 && weatherTop == marketTop && sourcesLandInBucket(dist.SourcePeaksC, out[weatherTop].Bucket) {
			s := &out[weatherTop]
			// This branch deliberately tests price-path convergence rather than
			// Kelly-sized terminal EV, so its paper stake is the configured minimum.
			s.SuggestedDollars = cfg.MinimumOrderDollars
			var market *domain.Market
			for i := range e.Markets {
				if e.Markets[i].ID == s.MarketID {
					market = &e.Markets[i]
					break
				}
			}
			if market != nil && len(dist.SourcePeaksC) >= 3 && s.BestAsk >= .10 && s.BestAsk <= .70 && s.BestBid > 0 &&
				s.BestAsk-s.BestBid <= AllowedSpread(cfg, s.BestAsk) && market.LiquidityNum >= cfg.MinimumLiquidity &&
				s.ModelProbability-s.BestAsk >= -.03 && s.SuggestedDollars >= cfg.MinimumOrderDollars {
				s.Action, s.Strategy = "BUY_PAPER", "SOURCE_ALIGNED_EARLY"
				s.Reason = "paper-only early three-source alignment study; marginal value is not eligible for live execution"
				s.Reasons = []string{s.Reason}
			}
		}
	}
	return out
}

func earlySourceWindow(dist domain.Distribution) bool {
	return dist.Phase == "PRE_DAY" || dist.Phase == "OVERNIGHT" || dist.Phase == "MORNING" || (dist.Phase == "INTRADAY" && dist.LocalMinute < 10*60)
}

func sourcesLandInBucket(peaks map[string]float64, label string) bool {
	if len(peaks) < 3 {
		return false
	}
	b, err := rules.ParseBucket(label)
	if err != nil {
		return false
	}
	isF := strings.Contains(strings.ToUpper(label), "F")
	for _, c := range peaks {
		v := c
		if isF {
			v = c*9/5 + 32
		}
		// Forecast peaks are continuous while resolution is whole-degree. Treat a
		// value within half a reporting unit (plus 0.05 numeric tolerance) as
		// supporting that bucket; 32.5C is therefore still a boundary vote for 32.
		if b.Low != nil && v < *b.Low-.55 {
			return false
		}
		if b.High != nil && v > *b.High+.55 {
			return false
		}
	}
	return true
}

func marketConsensusProbability(bid, ask float64) float64 {
	if bid > 0 && ask > bid && ask < 1 {
		return (bid + ask) / 2
	}
	if ask > 0 && ask < 1 {
		return ask
	}
	return 0
}

// weatherWeight is deliberately capped until a station has genuine historical
// calibration. The market is the prior; weather data earns permission to move
// away from it only when source quality and same-day evidence support that move.
func weatherWeight(dist domain.Distribution) float64 {
	quality := math.Max(0, math.Min(1, dist.DataQuality))
	weight := .20 + .30*quality
	if dist.EnsembleMemberCount >= 20 {
		weight += .05
	}
	if dist.HasObservation && (dist.Phase == "MORNING" || dist.Phase == "INTRADAY") {
		weight += .05
	}
	if dist.CalibrationReady {
		weight += .20
	}
	if dist.Phase == "PRE_DAY" {
		weight *= .75
	} else if dist.Phase == "OVERNIGHT" {
		weight *= .80
	} else if dist.Phase == "MORNING" {
		weight *= .90
	}
	if dist.UnstableWeather {
		weight *= .65
	}
	ceiling := .60
	if dist.CalibrationReady {
		ceiling = .80
	}
	return math.Max(.15, math.Min(ceiling, weight))
}

func MarketConvergence(markets []domain.Market) float64 {
	maxMid := 0.0
	for _, m := range markets {
		if m.BestBid <= 0 || m.BestAsk <= 0 || m.BestBid > m.BestAsk {
			continue
		}
		mid := (m.BestBid + m.BestAsk) / 2
		if mid > maxMid {
			maxMid = mid
		}
	}
	return math.Max(0, math.Min(1, maxMid))
}

// AllowedSpread preserves the live limit. The paper explorer tolerates two
// extra points only for contracts at 10c or below, where one tick is material.
func AllowedSpread(cfg domain.Config, ask float64) float64 {
	if cfg.Mode == "paper" {
		return math.Max(.01, math.Min(.03, .20*ask))
	}
	return cfg.MaximumSpread
}

// RepriceCandidate applies a freshly verified CLOB quote to a candidate. Gamma
// quotes can lag the book, so a changed price is recalculated rather than
// rejected forever. It never turns a non-candidate into an order.
func RepriceCandidate(cfg domain.Config, dist domain.Distribution, s *domain.Signal, bid, ask float64) {
	if s == nil || (s.Action != "BUY_PAPER" && s.Action != "BUY_LIVE") {
		return
	}
	previousAsk := s.BestAsk
	feeRate := 0.0
	if previousAsk > 0 && previousAsk < 1 {
		feeRate = s.FeePerShare / (previousAsk * (1 - previousAsk))
	}
	s.BestBid, s.BestAsk = bid, ask
	s.FeePerShare = feeRate * ask * (1 - ask)
	if cfg.Mode == "paper" && s.WeatherProbability > 0 && s.WeatherWeight > 0 {
		s.MarketConsensus = marketConsensusProbability(bid, ask)
		s.ModelProbability = s.MarketConsensus + s.WeatherWeight*(s.WeatherProbability-s.MarketConsensus)
		s.ModelProbability = math.Max(0, math.Min(1, s.ModelProbability))
		s.ConsensusGap = s.ModelProbability - s.MarketConsensus
	}
	s.GrossEdge = s.ModelProbability - ask
	weatherPenalty := 0.0
	if dist.UnstableWeather && cfg.Mode == "paper" {
		weatherPenalty = .03
	}
	s.NetEdge = s.GrossEdge - s.FeePerShare - cfg.ExpectedSlippage - cfg.UncertaintyBuffer - cfg.SettlementRiskBuffer - weatherPenalty
	conservativeP := math.Max(0, s.ModelProbability-cfg.UncertaintyBuffer-cfg.SettlementRiskBuffer-weatherPenalty)
	s.ConservativeValue = math.Max(0, conservativeP-s.FeePerShare-cfg.ExpectedSlippage)
	s.PotentialMove = s.ConservativeValue - ask
	spread := math.Max(0, ask-bid)
	s.ExpectedExitBid = math.Max(0, bid+s.ConvergenceCapture*math.Max(0, s.ConservativeValue-bid)-spread*.25)
	exitFee := feeRate * s.ExpectedExitBid * (1 - s.ExpectedExitBid)
	s.ExpectedTradeEdge = s.ExpectedExitBid - ask - exitFee - cfg.ExpectedSlippage
	s.SuggestedDollars = suggestedSize(cfg, conservativeP, ask+s.FeePerShare+cfg.ExpectedSlippage)
	threshold := cfg.MinimumNetEdge
	if cfg.Mode == "live" {
		threshold = 0
	} else if dist.Phase == "PRE_DAY" {
		threshold = math.Max(threshold, .10)
	}
	if cfg.Mode != "live" && dist.Phase == "INTRADAY" && dist.EntryCautionMinute > 0 && dist.LocalMinute >= dist.EntryCautionMinute {
		threshold = math.Max(threshold, .09)
	}
	qualified := s.NetEdge > threshold
	tradeThreshold := paperTradeThreshold(cfg, dist.Phase)
	if s.Strategy == "SOURCE_ALIGNED_EARLY" {
		qualified = earlySourceWindow(dist) && s.ModelProbability-ask >= -.03 && ask >= .10 && ask <= .70 && ask-bid <= AllowedSpread(cfg, ask)
		s.SuggestedDollars = cfg.MinimumOrderDollars
	} else if s.Strategy == "RANK_ALIGNED_SETTLEMENT" {
		qualified = s.NetEdge >= .06 && ask >= .10 && ask <= .70 && ask-bid <= AllowedSpread(cfg, ask)
	} else if cfg.Mode == "paper" {
		qualified = s.NetEdge > 0 && s.ExpectedTradeEdge >= tradeThreshold
		if ask < .10 || ask > .70 || s.ConsensusGap < .035 || ask-bid > AllowedSpread(cfg, ask) {
			qualified = false
		}
	}
	if cfg.Mode == "live" && s.MarketConsensus < .10 {
		qualified = false
	}
	if cfg.Mode != "live" && dist.Phase == "INTRADAY" && dist.EntryCautionMinute > 0 && dist.LocalMinute >= dist.EntryCautionMinute && s.ExpectedTradeEdge < .03 {
		qualified = false
	}
	if !qualified || s.SuggestedDollars < cfg.MinimumOrderDollars {
		s.Action = "SKIP"
		s.Reason = fmt.Sprintf("fresh book repricing removed the edge: expected bid-side %.1fpp", s.ExpectedTradeEdge*100)
	}
}

func paperTradeThreshold(cfg domain.Config, phase string) float64 {
	intraday := cfg.PaperIntradayTradeEdge
	if intraday <= 0 {
		intraday = .015
	}
	if phase != "PRE_DAY" {
		return intraday
	}
	preday := cfg.PaperPredayTradeEdge
	if preday <= 0 {
		preday = .03
	}
	return preday
}

func suggestedSize(cfg domain.Config, p, price float64) float64 {
	if price <= 0 || price >= 1 || p <= price {
		return 0
	}
	kelly := (p - price) / (1 - price) * cfg.KellyFraction
	amount := cfg.BankrollDollars * math.Max(0, kelly)
	amount = math.Min(amount, cfg.BankrollDollars*cfg.MaximumMarketRiskFraction)
	amount = math.Min(amount, cfg.BankrollDollars*cfg.MaximumCityDayRiskFraction)
	amount = math.Min(amount, cfg.MaximumOrderDollars)
	// Equal min/max explicitly selects fixed-stake execution. The probability
	// and edge gates still decide whether a trade exists; this only standardizes
	// the size of accepted paper/live candidates.
	if math.Abs(cfg.MinimumOrderDollars-cfg.MaximumOrderDollars) < 1e-9 && cfg.MinimumOrderDollars <= cfg.BankrollDollars*cfg.MaximumMarketRiskFraction+1e-9 && cfg.MinimumOrderDollars <= cfg.BankrollDollars*cfg.MaximumCityDayRiskFraction+1e-9 {
		amount = cfg.MinimumOrderDollars
	}
	return math.Floor((amount+1e-9)*100) / 100
}
