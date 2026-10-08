package execution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
	"weatherbot/internal/domain"
)

func TestRestartDeduplicationAndBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	cfg := domain.Config{BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumExposureDollars: 3, DailyLossLimitDollars: 1, MaximumCityDayRiskFraction: .067}
	fresh := func(id string) domain.Snapshot {
		return domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: id, MarketID: id, BestAsk: .1, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}}}}
	}
	s := fresh("one")
	if err := Process(path, cfg, &s); err != nil {
		t.Fatal(err)
	}
	s = fresh("one")
	if err := Process(path, cfg, &s); err != nil {
		t.Fatal(err)
	}
	if s.Reports[0].Signals[0].Action != "SKIP" {
		t.Fatal("duplicate order on restart")
	}
	s = fresh("two")
	if err := Process(path, cfg, &s); err != nil {
		t.Fatal(err)
	}
	s = fresh("three")
	if err := Process(path, cfg, &s); err != nil {
		t.Fatal(err)
	}
	if s.Reports[0].Signals[0].Action != "SKIP" {
		t.Fatal("loss budget exceeded")
	}
	b, _ := os.ReadFile(path)
	var l Ledger
	json.Unmarshal(b, &l)
	if l.Cash != 14 || l.Orders != 2 {
		t.Fatalf("wrong ledger: %+v", l)
	}
}

func TestDailySubmissionLimitIsSeparateFromLossLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	cfg := domain.Config{BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumExposureDollars: 3, DailyLossLimitDollars: 1, DailySubmissionLimitDollars: 2, MaximumCityDayRiskFraction: .067}
	for i := 0; i < 4; i++ {
		id := string(rune('a' + i))
		s := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: id, MarketID: id, BestAsk: .1, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}}}}
		if err := Process(path, cfg, &s); err != nil {
			t.Fatal(err)
		}
		if s.Reports[0].Signals[0].Action != "PAPER_FILLED" {
			t.Fatalf("submission %d was incorrectly tied to loss limit: %+v", i+1, s.Reports[0].Signals[0])
		}
	}
	s := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "e", MarketID: "e", BestAsk: .1, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}}}}
	if err := Process(path, cfg, &s); err != nil {
		t.Fatal(err)
	}
	if s.Reports[0].Signals[0].Action != "SKIP" || s.Reports[0].Signals[0].Reason != "daily buy submission cap reached" {
		t.Fatalf("fifth submission should hit the independent $2 cap: %+v", s.Reports[0].Signals[0])
	}
}

func TestUserRequestedPaperLiquidationUsesVerifiedBid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.json")
	cfg := domain.Config{BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumExposureDollars: 3, DailyLossLimitDollars: 5, DailySubmissionLimitDollars: 5, MaximumCityDayRiskFraction: .1}
	entry := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "e", MarketID: "m", BestAsk: .1, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}}}}
	if err := Process(path, cfg, &entry); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "LIQUIDATE_PAPER"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	exit := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{MarketID: "m", BestBid: .06, BestAsk: .07, BookVerified: true, GeneratedAt: time.Now()}}}}}
	if err := Process(path, cfg, &exit); err != nil {
		t.Fatal(err)
	}
	positions, err := OpenPositions(path)
	if err != nil || len(positions) != 0 {
		t.Fatalf("requested liquidation did not close verified position: %+v %v", positions, err)
	}
	b, _ := os.ReadFile(path)
	var ledger Ledger
	if err := json.Unmarshal(b, &ledger); err != nil {
		t.Fatal(err)
	}
	if len(ledger.Trades) != 2 || ledger.Trades[1].Type != "EXIT" || ledger.Trades[1].Price != .06 || ledger.Trades[1].Reason != "user-requested paper portfolio liquidation" {
		t.Fatalf("liquidation audit record is wrong: %+v", ledger.Trades)
	}
	if _, err := os.Stat(filepath.Join(dir, "LIQUIDATE_PAPER")); !os.IsNotExist(err) {
		t.Fatal("liquidation marker must clear after every position exits")
	}
}

func TestStaleLiquidationRequestWritesOffPositionWithoutBid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.json")
	cfg := domain.Config{BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumExposureDollars: 3, DailyLossLimitDollars: 5, DailySubmissionLimitDollars: 5, MaximumCityDayRiskFraction: .1}
	entry := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "e", MarketID: "m", BestAsk: .1, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}}}}
	if err := Process(path, cfg, &entry); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "LIQUIDATE_PAPER")
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-3 * time.Minute)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatal(err)
	}
	if err := Process(path, cfg, &domain.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var ledger Ledger
	if err := json.Unmarshal(b, &ledger); err != nil {
		t.Fatal(err)
	}
	if len(ledger.Positions) != 0 || len(ledger.Trades) != 2 || ledger.Trades[1].Price != 0 || ledger.Trades[1].PnL != -.5 {
		t.Fatalf("unexecutable liquidation must be conservatively written off: %+v", ledger)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("write-off must complete and clear liquidation marker")
	}
}

func TestResetArchivesOldPaperLedgerAndRestoresBankroll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paper-ledger.json")
	cfg := domain.Config{Mode: "paper", BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumExposureDollars: 3, DailyLossLimitDollars: 5, DailySubmissionLimitDollars: 10, MaximumCityDayRiskFraction: .1}
	entry := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "e", MarketID: "m", BestAsk: .1, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}}}}
	if err := Process(path, cfg, &entry); err != nil {
		t.Fatal(err)
	}
	backup, err := Reset(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("existing ledger was not archived")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var ledger Ledger
	if err := json.Unmarshal(b, &ledger); err != nil {
		t.Fatal(err)
	}
	if ledger.Cash != 15 || ledger.Initial != 15 || len(ledger.Positions) != 0 || ledger.Orders != 0 || ledger.DaySubmitted != 0 || ledger.RealizedPnL != 0 {
		t.Fatalf("paper account did not reset cleanly: %+v", ledger)
	}
}
func TestMinimumSharesCannotForceOversizing(t *testing.T) {
	cfg := domain.Config{BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumExposureDollars: 3, DailyLossLimitDollars: 1, MaximumCityDayRiskFraction: .067}
	s := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "a", MarketID: "a", BestAsk: .5, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5}}}}}
	if err := Process(filepath.Join(t.TempDir(), "ledger.json"), cfg, &s); err != nil {
		t.Fatal(err)
	}
	if s.Reports[0].Signals[0].Action != "SKIP" {
		t.Fatal("$2.50 minimum must not be forced into $0.50 budget")
	}
}

func TestSettlementRequiresFinalResolutionAndCannotDoubleCredit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	cfg := domain.Config{BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumExposureDollars: 3, DailyLossLimitDollars: 1, MaximumCityDayRiskFraction: .067}
	s := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "e", MarketID: "m", TokenID: "yes", BestAsk: .1, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5}}}}}
	if err := Process(path, cfg, &s); err != nil {
		t.Fatal(err)
	}
	m := domain.Market{ID: "m", Closed: true, ResolutionStatus: "disputed", ClobTokenIDsJSON: `["no","yes"]`, OutcomePricesJSON: `["0","1"]`}
	s = domain.Snapshot{Reports: []domain.EventReport{{Event: domain.Event{Markets: []domain.Market{m}}}}}
	if err := Process(path, cfg, &s); err != nil {
		t.Fatal(err)
	}
	positions, err := OpenPositions(path)
	if err != nil || len(positions) != 1 {
		t.Fatal("unresolved market must remain held")
	}
	s.Reports[0].Event.Markets[0].ResolutionStatus = "resolved"
	for i := 0; i < 2; i++ {
		if err := Process(path, cfg, &s); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var l Ledger
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	if l.Cash != 19.5 || l.RealizedPnL != 4.5 || len(l.Positions) != 0 || !l.Closed["m"] {
		t.Fatalf("incorrect settlement: %+v", l)
	}
}

func TestProfitablePositionUsesDynamicTrail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	cfg := domain.Config{BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumExposureDollars: 3, DailyLossLimitDollars: 5, MaximumCityDayRiskFraction: .1}
	entry := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "e", MarketID: "m", TokenID: "yes", BestBid: .08, BestAsk: .1, ModelProbability: .5, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}}}}
	if err := Process(path, cfg, &entry); err != nil {
		t.Fatal(err)
	}
	update := func(bid float64) domain.Snapshot {
		return domain.Snapshot{Reports: []domain.EventReport{{Distribution: &domain.Distribution{Phase: "INTRADAY"}, Signals: []domain.Signal{{MarketID: "m", BestBid: bid, BestAsk: bid + .02, ModelProbability: .5, BookVerified: true}}}}}
	}
	high := update(.14)
	if err := Process(path, cfg, &high); err != nil {
		t.Fatal(err)
	}
	positions, _ := OpenPositions(path)
	if len(positions) != 1 {
		t.Fatal("a fresh high should remain open while the dynamic trail advances")
	}
	retrace := update(.115)
	if err := Process(path, cfg, &retrace); err != nil {
		t.Fatal(err)
	}
	positions, _ = OpenPositions(path)
	if len(positions) != 0 || retrace.Reports[0].Signals[0].Action != "PAPER_EXITED" {
		t.Fatal("a profitable retrace from an armed high should exit")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ledger Ledger
	if err := json.Unmarshal(b, &ledger); err != nil {
		t.Fatal(err)
	}
	if len(ledger.Trades) != 2 || ledger.Trades[0].Type != "ENTRY" || ledger.Trades[1].Type != "EXIT" {
		t.Fatalf("expected an entry and exit audit trail: %+v", ledger.Trades)
	}
	if ledger.Trades[1].PnL <= 0 || ledger.Trades[1].Price != .115 {
		t.Fatalf("exit record must use executable Bid and preserve realized profit: %+v", ledger.Trades[1])
	}
}

func TestExpectedExitEdgeNeedsConfirmationUnlessClearlyNegative(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	cfg := domain.Config{BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumExposureDollars: 3, DailyLossLimitDollars: 5, MaximumCityDayRiskFraction: .1}
	entry := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "e", MarketID: "m", TokenID: "yes", BestBid: .10, BestAsk: .11, ModelProbability: .30, MinimumShares: 4, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}}}}
	if err := Process(path, cfg, &entry); err != nil {
		t.Fatal(err)
	}
	update := func(edge float64) domain.Snapshot {
		return domain.Snapshot{Reports: []domain.EventReport{{Distribution: &domain.Distribution{Phase: "INTRADAY"}, Signals: []domain.Signal{{MarketID: "m", BestBid: .10, BestAsk: .11, ModelProbability: .30, ConsensusGap: .05, ExpectedTradeEdge: edge, BookVerified: true}}}}}
	}
	first := update(-.002)
	if err := Process(path, cfg, &first); err != nil {
		t.Fatal(err)
	}
	positions, _ := OpenPositions(path)
	if len(positions) != 1 {
		t.Fatal("small one-scan edge noise must not force an exit")
	}
	second := update(-.002)
	if err := Process(path, cfg, &second); err != nil {
		t.Fatal(err)
	}
	positions, _ = OpenPositions(path)
	if len(positions) != 0 || second.Reports[0].Signals[0].Action != "PAPER_EXITED" {
		t.Fatal("two non-positive edge scans must exit")
	}
}

func TestClearlyNegativeExpectedExitEdgeExitsImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	cfg := domain.Config{BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumExposureDollars: 3, DailyLossLimitDollars: 5, MaximumCityDayRiskFraction: .1}
	entry := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "e", MarketID: "m", TokenID: "yes", BestBid: .10, BestAsk: .11, ModelProbability: .30, MinimumShares: 4, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}}}}
	if err := Process(path, cfg, &entry); err != nil {
		t.Fatal(err)
	}
	bad := domain.Snapshot{Reports: []domain.EventReport{{Distribution: &domain.Distribution{Phase: "INTRADAY"}, Signals: []domain.Signal{{MarketID: "m", BestBid: .08, BestAsk: .10, ModelProbability: .27, ConsensusGap: .05, ExpectedTradeEdge: -.011, BookVerified: true}}}}}
	if err := Process(path, cfg, &bad); err != nil {
		t.Fatal(err)
	}
	positions, _ := OpenPositions(path)
	if len(positions) != 0 {
		t.Fatal("edge below -1pp must exit immediately")
	}
}

func TestEarlySessionExitWaitsForGraceAndDistinctScans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	cfg := domain.Config{BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5, MaximumExposureDollars: 3, DailyLossLimitDollars: 5, MaximumCityDayRiskFraction: .1}
	now := time.Now().UTC()
	entry := domain.Snapshot{GeneratedAt: now, Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "e", MarketID: "m", TokenID: "yes", BestBid: .10, BestAsk: .11, ModelProbability: .30, MinimumShares: 4, BookVerified: true, SuggestedDollars: .5, GeneratedAt: now}}}}}
	if err := Process(path, cfg, &entry); err != nil {
		t.Fatal(err)
	}
	for _, minute := range []int{5, 10, 15} {
		update := domain.Snapshot{GeneratedAt: now.Add(time.Duration(minute) * time.Minute), Reports: []domain.EventReport{{Distribution: &domain.Distribution{Phase: "INTRADAY", LocalMinute: 408}, Signals: []domain.Signal{{MarketID: "m", BestBid: .10, BestAsk: .11, ModelProbability: .30, ExpectedTradeEdge: -.015, BookVerified: true}}}}}
		if err := Process(path, cfg, &update); err != nil {
			t.Fatal(err)
		}
		positions, _ := OpenPositions(path)
		if len(positions) != 1 {
			t.Fatal("early negative-edge noise must not exit during 30-minute grace")
		}
	}
	update := domain.Snapshot{GeneratedAt: now.Add(35 * time.Minute), Reports: []domain.EventReport{{Distribution: &domain.Distribution{Phase: "INTRADAY", LocalMinute: 440}, Signals: []domain.Signal{{MarketID: "m", BestBid: .10, BestAsk: .11, ModelProbability: .30, ExpectedTradeEdge: -.015, BookVerified: true}}}}}
	if err := Process(path, cfg, &update); err != nil {
		t.Fatal(err)
	}
	positions, _ := OpenPositions(path)
	if len(positions) != 0 {
		t.Fatal("persistent early edge failure must exit after grace")
	}
}

func TestFullPaperPortfolioRotatesOnlyProfitableWeakerPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	cfg := domain.Config{
		BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5,
		MaximumExposureDollars: 3, MaximumPaperPositions: 1,
		DailyLossLimitDollars: 5, DailySubmissionLimitDollars: 5,
		MaximumCityDayRiskFraction: .1, PaperRotationMinProfit: .02, PaperRotationMinUpgrade: .015,
	}
	entry := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "old-event", EventTitle: "Old", MarketID: "old", TokenID: "old-token", BestBid: .09, BestAsk: .1, ModelProbability: .5, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}}}}
	if err := Process(path, cfg, &entry); err != nil {
		t.Fatal(err)
	}
	rotation := domain.Snapshot{Reports: []domain.EventReport{
		{Signals: []domain.Signal{{MarketID: "old", BestBid: .105, BestAsk: .11, ModelProbability: .5, ConsensusGap: .1, ExpectedExitBid: .11, BookVerified: true}}},
		{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "new-event", EventTitle: "New", MarketID: "new", TokenID: "new-token", BestBid: .09, BestAsk: .1, ModelProbability: .6, ExpectedTradeEdge: .05, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}},
	}}
	if err := Process(path, cfg, &rotation); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var ledger Ledger
	if err := json.Unmarshal(b, &ledger); err != nil {
		t.Fatal(err)
	}
	if _, oldHeld := ledger.Positions["old"]; oldHeld {
		t.Fatalf("profitable weaker position was not rotated: %+v", ledger)
	}
	if _, newHeld := ledger.Positions["new"]; !newHeld || ledger.RealizedPnL <= 0 || len(ledger.Trades) != 3 || ledger.Trades[1].Type != "ROTATE_EXIT" {
		t.Fatalf("rotation did not preserve realized profit and enter replacement: %+v", ledger)
	}
}

func TestFullPaperPortfolioNeverRotatesALosingPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	cfg := domain.Config{
		BankrollDollars: 15, MinimumOrderDollars: .5, MaximumOrderDollars: .5,
		MaximumExposureDollars: 3, MaximumPaperPositions: 1,
		DailyLossLimitDollars: 5, DailySubmissionLimitDollars: 5,
		MaximumCityDayRiskFraction: .1, PaperRotationMinProfit: .02, PaperRotationMinUpgrade: .015,
	}
	entry := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "old-event", EventTitle: "Old", MarketID: "old", TokenID: "old-token", BestBid: .09, BestAsk: .1, ModelProbability: .5, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}}}}
	if err := Process(path, cfg, &entry); err != nil {
		t.Fatal(err)
	}
	rotation := domain.Snapshot{Reports: []domain.EventReport{
		{Signals: []domain.Signal{{MarketID: "old", BestBid: .08, BestAsk: .09, ModelProbability: .5, ConsensusGap: .02, ExpectedExitBid: .085, BookVerified: true}}},
		{Signals: []domain.Signal{{Action: "BUY_PAPER", EventID: "new-event", EventTitle: "New", MarketID: "new", TokenID: "new-token", BestBid: .09, BestAsk: .1, ModelProbability: .7, ExpectedTradeEdge: .08, MinimumShares: 5, BookVerified: true, SuggestedDollars: .5, GeneratedAt: time.Now()}}},
	}}
	if err := Process(path, cfg, &rotation); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var ledger Ledger
	if err := json.Unmarshal(b, &ledger); err != nil {
		t.Fatal(err)
	}
	if _, oldHeld := ledger.Positions["old"]; !oldHeld {
		t.Fatalf("losing position must not be rotated out: %+v", ledger)
	}
	if _, newHeld := ledger.Positions["new"]; newHeld || len(ledger.Trades) != 1 {
		t.Fatalf("replacement must be skipped when no profitable slot is available: %+v", ledger)
	}
}
