package execution

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
	"weatherbot/internal/client"
	"weatherbot/internal/domain"
)

// Paper fills are hypothetical taker fills, never proof of exchange execution.
type Position struct {
	EventID            string
	EventTitle         string
	MarketID           string
	Bucket             string
	TokenID            string
	Shares             float64
	Cost               float64
	EntryPrice         float64
	EntryProbability   float64
	HighestBid         float64
	EdgeFailureCount   int
	ModelFailureCount  int
	ThesisFailureCount int
	Strategy           string
	OpenedAt           time.Time
}
type TradeRecord struct {
	At         time.Time
	Type       string
	EventID    string
	EventTitle string
	MarketID   string
	Bucket     string
	Shares     float64
	Price      float64
	Dollars    float64
	PnL        float64
	Reason     string
}
type Ledger struct {
	RealizedPnL  float64
	Day          string
	DayLoss      float64
	DaySubmitted float64
	Closed       map[string]bool
	Entries      map[string]int
	LastExit     map[string]time.Time
	Initial      float64
	Cash         float64
	Positions    map[string]Position
	Orders       int
	Trades       []TradeRecord
	UpdatedAt    time.Time
}

// Reset archives the current paper ledger and starts a fresh simulated account.
// It never touches wallet state or live execution files.
func Reset(path string, cfg domain.Config) (string, error) {
	if cfg.Mode != "paper" {
		return "", fmt.Errorf("paper ledger reset requires paper mode")
	}
	backup := ""
	if previous, err := os.ReadFile(path); err == nil {
		archiveDir := filepath.Join(filepath.Dir(path), "archive", "paper-resets")
		if err := os.MkdirAll(archiveDir, 0700); err != nil {
			return "", err
		}
		backup = filepath.Join(archiveDir, "paper-ledger-"+time.Now().UTC().Format("20060102-150405.000000000")+".json")
		if err := os.WriteFile(backup, previous, 0600); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	l := Ledger{
		Day: time.Now().UTC().Format("2006-01-02"), Initial: cfg.BankrollDollars, Cash: cfg.BankrollDollars,
		Closed: map[string]bool{}, Entries: map[string]int{}, LastExit: map[string]time.Time{}, Positions: map[string]Position{}, UpdatedAt: time.Now().UTC(),
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	tmp := path + ".reset.tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return backup, nil
}

func Process(path string, cfg domain.Config, snapshot *domain.Snapshot) error {
	l := Ledger{Initial: cfg.BankrollDollars, Cash: cfg.BankrollDollars, Positions: map[string]Position{}}
	b, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(b, &l); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if l.Initial != cfg.BankrollDollars || l.Cash < 0 || math.IsNaN(l.Cash) || math.IsInf(l.Cash, 0) || l.Positions == nil {
		return fmt.Errorf("invalid paper ledger; refusing reset")
	}
	if l.Closed == nil {
		l.Closed = map[string]bool{}
	}
	if l.Entries == nil {
		l.Entries = map[string]int{}
	}
	if l.LastExit == nil {
		l.LastExit = map[string]time.Time{}
	}
	day := time.Now().UTC().Format("2006-01-02")
	if l.Day != day {
		l.Day = day
		l.DayLoss = 0
		l.DaySubmitted = 0
	}
	liquidatePath := filepath.Join(filepath.Dir(path), "LIQUIDATE_PAPER")
	liquidateInfo, liquidateErr := os.Stat(liquidatePath)
	liquidateRequested := liquidateErr == nil
	if liquidateErr != nil && !os.IsNotExist(liquidateErr) {
		return liquidateErr
	}
	for _, r := range snapshot.Reports {
		for _, m := range r.Event.Markets {
			p, ok := l.Positions[m.ID]
			if !ok || !m.Closed || m.ResolutionStatus != "resolved" {
				continue
			}
			tokens, e := client.DecodeStringArray(m.ClobTokenIDsJSON)
			prices, e2 := client.DecodeStringArray(m.OutcomePricesJSON)
			if e != nil || e2 != nil || len(tokens) != len(prices) {
				continue
			}
			for i, token := range tokens {
				if token != p.TokenID {
					continue
				}
				price, e := strconv.ParseFloat(prices[i], 64)
				if e != nil || (price != 0 && price != 1) {
					continue
				}
				receipt := p.Shares * price
				pnl := receipt - p.Cost
				l.Cash += receipt
				l.RealizedPnL += pnl
				if pnl < 0 {
					l.DayLoss -= pnl
				}
				delete(l.Positions, m.ID)
				l.Closed[m.ID] = true
				l.Trades = append(l.Trades, TradeRecord{At: time.Now().UTC(), Type: "SETTLEMENT", EventID: p.EventID, EventTitle: p.EventTitle, MarketID: p.MarketID, Bucket: p.Bucket, Shares: p.Shares, Price: price, Dollars: receipt, PnL: pnl, Reason: "official market resolution"})
			}
		}
	}
	for i := range snapshot.Reports {
		r := &snapshot.Reports[i]
		for j := range r.Signals {
			s := &r.Signals[j]
			p, held := l.Positions[s.MarketID]
			if !held || !s.BookVerified || s.BestBid <= 0 {
				continue
			}
			// Only verified bid-side executable prices can release cash. Ask is not
			// an exit price, so drawdown and trailing logic use the executable bid.
			if p.EntryPrice <= 0 && p.Shares > 0 {
				p.EntryPrice = p.Cost / p.Shares
			}
			if s.BestBid > p.HighestBid {
				p.HighestBid = s.BestBid
			}
			rate := 0.0
			if s.BestAsk > 0 && s.BestAsk < 1 {
				rate = s.FeePerShare / (s.BestAsk * (1 - s.BestAsk))
			}
			exitUnit := math.Max(0, s.BestBid-rate*s.BestBid*(1-s.BestBid)-cfg.ExpectedSlippage)
			netReturn := exitUnit/p.EntryPrice - 1
			reason := ""
			settlementHold := p.Strategy == "RANK_ALIGNED_SETTLEMENT"
			patientHold := settlementHold || p.Strategy == "SOURCE_ALIGNED_EARLY"
			earlyWindow := r.Distribution != nil && (r.Distribution.Phase == "PRE_DAY" || r.Distribution.Phase == "OVERNIGHT" || r.Distribution.Phase == "MORNING" || (r.Distribution.Phase == "INTRADAY" && r.Distribution.LocalMinute > 0 && r.Distribution.LocalMinute < 10*60))
			minGrace := 30 * time.Minute
			if r.Distribution != nil && r.Distribution.Phase == "PRE_DAY" {
				minGrace = 45 * time.Minute
			}
			graceElapsed := p.OpenedAt.IsZero() || snapshot.GeneratedAt.Sub(p.OpenedAt) >= minGrace
			edgeFailed := !patientHold && s.ExpectedTradeEdge <= 0
			if edgeFailed {
				p.EdgeFailureCount++
			} else {
				p.EdgeFailureCount = 0
			}
			modelDrop := p.EntryProbability - s.ModelProbability
			if modelDrop >= .03 && netReturn < 0 {
				p.ModelFailureCount++
			} else {
				p.ModelFailureCount = 0
			}
			if patientHold && !rankAlignedForMarket(r.Signals, s.MarketID) {
				p.ThesisFailureCount++
			} else {
				p.ThesisFailureCount = 0
			}
			highGain := p.HighestBid - p.EntryPrice
			trail := dynamicProfitTrail(highGain, s.MarketConvergence, r.Distribution)
			activation := math.Max(.02, math.Min(.04, p.EntryPrice*.15))
			if liquidateRequested {
				reason = "user-requested paper portfolio liquidation"
			} else if netReturn <= -.40 {
				reason = "40% emergency executable loss limit overrides early grace"
			} else if patientHold && p.ThesisFailureCount >= 2 && (!earlyWindow || graceElapsed) {
				reason = "weather and market top-bucket ranking diverged for two scans"
			} else if !patientHold && !earlyWindow && s.ExpectedTradeEdge <= -.01 {
				reason = "expected executable exit edge deteriorated below -1pp"
			} else if !patientHold && earlyWindow && graceElapsed && p.EdgeFailureCount >= 3 {
				reason = "early-session exit edge stayed non-positive for three scans after grace period"
			} else if !patientHold && !earlyWindow && p.EdgeFailureCount >= 2 {
				reason = "expected executable exit edge was non-positive for two scans"
			} else if modelDrop >= .04 && netReturn < 0 && (!earlyWindow || graceElapsed) {
				reason = "model probability fell by at least 4pp while position was losing"
			} else if p.ModelFailureCount >= 2 && (!earlyWindow || graceElapsed) {
				reason = "model probability fell by at least 3pp for two losing scans"
			} else if netReturn <= -.25 && (!earlyWindow || graceElapsed) {
				reason = "25% hard executable loss limit"
			} else if highGain >= activation && s.BestBid <= p.HighestBid-trail && netReturn > 0 {
				reason = "dynamic profit trail protected a profitable bid-side move"
			} else if s.MarketConvergence >= .65 && netReturn >= .05 && !patientHold {
				reason = "profitable convergence trade harvested before 70% market concentration"
			} else if s.BestBid > s.ModelProbability+.02 {
				reason = "bid moved above current model value"
			} else if !patientHold && r.Distribution != nil && r.Distribution.Phase == "AWAITING_SETTLEMENT" {
				reason = "observation day ended"
			} else if r.Distribution != nil && r.Distribution.Phase == "LATE_DAY" && s.MarketConvergence >= .70 {
				reason = "late local window and market already converged"
			}
			exit := reason != ""
			if !exit {
				l.Positions[s.MarketID] = p
				continue
			}
			receipt := p.Shares * exitUnit
			pnl := receipt - p.Cost
			l.Cash += receipt
			l.RealizedPnL += pnl
			if pnl < 0 {
				l.DayLoss -= pnl
			}
			delete(l.Positions, s.MarketID)
			l.LastExit[s.MarketID] = time.Now().UTC()
			s.Action = "PAPER_EXITED"
			s.Reason = "hypothetical bid-side exit after costs: " + reason
			l.Trades = append(l.Trades, TradeRecord{At: time.Now().UTC(), Type: "EXIT", EventID: p.EventID, EventTitle: p.EventTitle, MarketID: p.MarketID, Bucket: p.Bucket, Shares: p.Shares, Price: s.BestBid, Dollars: receipt, PnL: pnl, Reason: reason})
		}
	}
	if liquidateRequested && time.Since(liquidateInfo.ModTime()) >= 2*time.Minute {
		for id, p := range l.Positions {
			pnl := -p.Cost
			l.RealizedPnL += pnl
			l.DayLoss -= pnl
			delete(l.Positions, id)
			l.LastExit[id] = time.Now().UTC()
			l.Trades = append(l.Trades, TradeRecord{At: time.Now().UTC(), Type: "EXIT", EventID: p.EventID, EventTitle: p.EventTitle, MarketID: p.MarketID, Bucket: p.Bucket, Shares: p.Shares, Price: 0, Dollars: 0, PnL: pnl, Reason: "user-requested liquidation write-off after no executable bid"})
		}
	}
	exposure := 0.0
	for _, p := range l.Positions {
		exposure += p.Cost
	}
	if liquidateRequested && len(l.Positions) == 0 {
		if err := os.Remove(liquidatePath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	dailySubmissionLimit := cfg.DailySubmissionLimitDollars
	if dailySubmissionLimit <= 0 {
		dailySubmissionLimit = cfg.DailyLossLimitDollars
	}
	type candidateRef struct {
		signal *domain.Signal
	}
	candidates := []candidateRef{}
	for i := range snapshot.Reports {
		for j := range snapshot.Reports[i].Signals {
			if snapshot.Reports[i].Signals[j].Action == "BUY_PAPER" {
				candidates = append(candidates, candidateRef{signal: &snapshot.Reports[i].Signals[j]})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].signal.ExpectedTradeEdge > candidates[j].signal.ExpectedTradeEdge
	})
	maxPositions := cfg.MaximumPaperPositions
	if maxPositions <= 0 {
		maxPositions = 10
	}
	for _, candidate := range candidates {
		s := candidate.signal
		reason := ""
		city := 0.0
		for _, p := range l.Positions {
			if p.EventID == s.EventID {
				city += p.Cost
			}
		}
		if _, ok := l.Positions[s.MarketID]; ok {
			reason = "already held: no duplicate entries"
		}
		if l.Closed[s.MarketID] {
			reason = "market already settled"
		}
		if exited := l.LastExit[s.MarketID]; !exited.IsZero() && s.GeneratedAt.Sub(exited) < 30*time.Minute {
			reason = "30-minute re-entry cooldown"
		}
		if !s.BookVerified {
			reason = "fresh depth verification required"
		}
		if s.SuggestedDollars < cfg.MinimumOrderDollars || s.SuggestedDollars > cfg.MaximumOrderDollars {
			reason = "order budget exceeded"
		}
		if l.DayLoss+s.SuggestedDollars > cfg.DailyLossLimitDollars+1e-9 {
			reason = "realized daily loss budget exhausted"
		}
		if l.DaySubmitted+s.SuggestedDollars > dailySubmissionLimit+1e-9 {
			reason = "daily buy submission cap reached"
		}
		needsCapacity := len(l.Positions) >= maxPositions || exposure+s.SuggestedDollars > cfg.MaximumExposureDollars+1e-9
		needsCitySlot := city+s.SuggestedDollars > cfg.BankrollDollars*cfg.MaximumCityDayRiskFraction+1e-9
		if reason == "" && (needsCapacity || needsCitySlot) {
			requiredEvent := ""
			if needsCitySlot {
				requiredEvent = s.EventID
			}
			if rotateProfitableWeakPosition(&l, cfg, snapshot, s, requiredEvent, &exposure) {
				city = 0
				for _, p := range l.Positions {
					if p.EventID == s.EventID {
						city += p.Cost
					}
				}
			} else {
				reason = "ten-slot portfolio full; no profitable weaker position to rotate"
			}
		}
		if reason == "" && (len(l.Positions) >= maxPositions || exposure+s.SuggestedDollars > cfg.MaximumExposureDollars+1e-9 || city+s.SuggestedDollars > cfg.BankrollDollars*cfg.MaximumCityDayRiskFraction+1e-9 || s.SuggestedDollars > l.Cash) {
			reason = "portfolio exposure, city slot, or available cash limit"
		}
		if reason != "" {
			s.Action = "SKIP"
			s.Reason = reason
			continue
		}
		unit := s.BestAsk + s.FeePerShare + cfg.ExpectedSlippage
		shares := math.Floor(s.SuggestedDollars/unit*100) / 100
		if shares < s.MinimumShares {
			s.Action = "SKIP"
			s.Reason = "minimum exchange shares exceed $0.50 budget"
			continue
		}
		cost := shares * unit
		l.Cash -= cost
		exposure += cost
		l.Orders++
		l.DaySubmitted += s.SuggestedDollars
		l.Entries[s.MarketID]++
		strategy := s.Strategy
		if strategy == "" {
			strategy = "CONVERGENCE"
		}
		l.Positions[s.MarketID] = Position{EventID: s.EventID, EventTitle: s.EventTitle, MarketID: s.MarketID, Bucket: s.Bucket, TokenID: s.TokenID, Shares: shares, Cost: cost, EntryPrice: unit, EntryProbability: s.ModelProbability, HighestBid: s.BestBid, Strategy: strategy, OpenedAt: s.GeneratedAt}
		l.Trades = append(l.Trades, TradeRecord{At: time.Now().UTC(), Type: "ENTRY", EventID: s.EventID, EventTitle: s.EventTitle, MarketID: s.MarketID, Bucket: s.Bucket, Shares: shares, Price: unit, Dollars: cost, Reason: s.Reason})
		s.Action = "PAPER_FILLED"
		s.Reason = "hypothetical taker fill; includes fee and slippage, no real order"
	}
	l.UpdatedAt = time.Now().UTC()
	b, err = json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func dynamicProfitTrail(highGain, convergence float64, dist *domain.Distribution) float64 {
	trail := math.Max(.015, math.Min(.04, highGain*.40))
	if convergence >= .50 {
		trail = math.Min(trail, .03)
	}
	if convergence >= .65 {
		trail = math.Min(trail, .02)
	}
	if dist != nil && (dist.Phase == "LATE_DAY" || dist.Phase == "AWAITING_SETTLEMENT") {
		trail = math.Min(trail, .015)
	}
	return trail
}

func rankAlignedForMarket(signals []domain.Signal, marketID string) bool {
	weatherTop, marketTop := "", ""
	weatherP, marketP := -1.0, -1.0
	for _, s := range signals {
		if s.WeatherProbability > weatherP {
			weatherP, weatherTop = s.WeatherProbability, s.MarketID
		}
		if s.MarketConsensus > marketP {
			marketP, marketTop = s.MarketConsensus, s.MarketID
		}
	}
	return weatherTop == marketID && marketTop == marketID
}

// rotateProfitableWeakPosition sells one weaker paper position only when its
// current executable Bid realizes a net profit and the incoming opportunity's
// expected round-trip edge clears the remaining hold value by a safety margin.
// It can preserve a profit on the outgoing leg; it cannot guarantee the new
// position will be profitable.
func rotateProfitableWeakPosition(l *Ledger, cfg domain.Config, snapshot *domain.Snapshot, incoming *domain.Signal, requiredEvent string, exposure *float64) bool {
	minProfit := cfg.PaperRotationMinProfit
	if minProfit <= 0 {
		minProfit = .02
	}
	minUpgrade := cfg.PaperRotationMinUpgrade
	if minUpgrade <= 0 {
		minUpgrade = .015
	}
	type choice struct {
		position Position
		signal   *domain.Signal
		exitUnit float64
		holdEdge float64
	}
	var best *choice
	for i := range snapshot.Reports {
		for j := range snapshot.Reports[i].Signals {
			s := &snapshot.Reports[i].Signals[j]
			p, held := l.Positions[s.MarketID]
			if !held || s.MarketID == incoming.MarketID || !s.BookVerified || s.BestBid <= 0 || (requiredEvent != "" && p.EventID != requiredEvent) {
				continue
			}
			exitUnit := paperExitUnit(cfg, s)
			if p.EntryPrice <= 0 || exitUnit/p.EntryPrice-1 < minProfit {
				continue
			}
			holdExit := paperExpectedExitUnit(cfg, s)
			holdEdge := math.Max(0, holdExit-exitUnit)
			if incoming.ExpectedTradeEdge < holdEdge+minUpgrade {
				continue
			}
			if best == nil || holdEdge < best.holdEdge {
				best = &choice{position: p, signal: s, exitUnit: exitUnit, holdEdge: holdEdge}
			}
		}
	}
	if best == nil {
		return false
	}
	receipt := best.position.Shares * best.exitUnit
	pnl := receipt - best.position.Cost
	if pnl <= 0 {
		return false
	}
	l.Cash += receipt
	l.RealizedPnL += pnl
	delete(l.Positions, best.position.MarketID)
	l.LastExit[best.position.MarketID] = time.Now().UTC()
	*exposure -= best.position.Cost
	best.signal.Action = "PAPER_ROTATED_OUT"
	best.signal.Reason = "profitable bid-side rotation into a materially stronger opportunity"
	l.Trades = append(l.Trades, TradeRecord{At: time.Now().UTC(), Type: "ROTATE_EXIT", EventID: best.position.EventID, EventTitle: best.position.EventTitle, MarketID: best.position.MarketID, Bucket: best.position.Bucket, Shares: best.position.Shares, Price: best.signal.BestBid, Dollars: receipt, PnL: pnl, Reason: best.signal.Reason})
	return true
}

func paperExitUnit(cfg domain.Config, s *domain.Signal) float64 {
	rate := 0.0
	if s.BestAsk > 0 && s.BestAsk < 1 {
		rate = s.FeePerShare / (s.BestAsk * (1 - s.BestAsk))
	}
	return math.Max(0, s.BestBid-rate*s.BestBid*(1-s.BestBid)-cfg.ExpectedSlippage)
}

func paperExpectedExitUnit(cfg domain.Config, s *domain.Signal) float64 {
	rate := 0.0
	if s.BestAsk > 0 && s.BestAsk < 1 {
		rate = s.FeePerShare / (s.BestAsk * (1 - s.BestAsk))
	}
	return math.Max(0, s.ExpectedExitBid-rate*s.ExpectedExitBid*(1-s.ExpectedExitBid)-cfg.ExpectedSlippage)
}

func OpenPositions(path string) (map[string]Position, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]Position{}, nil
	}
	if err != nil {
		return nil, err
	}
	var l Ledger
	if err = json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	return l.Positions, nil
}
