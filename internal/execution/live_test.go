package execution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weatherbot/internal/domain"
)

func TestWriteLiveIntentsRequiresCalibration(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	dist := &domain.Distribution{CalibrationReady: false, ProbabilityStatus: "UNCALIBRATED", DataQuality: .9}
	signal := domain.Signal{Action: "BUY_LIVE", BookVerified: true, NetEdge: .2, ExpectedTradeEdge: .02, MarketConsensus: .2, SuggestedDollars: 1, EventID: "e", MarketID: "m", TokenID: "12345678901", BestAsk: .4, GeneratedAt: now}
	snapshot := domain.Snapshot{Reports: []domain.EventReport{{Distribution: dist, Signals: []domain.Signal{signal}}}}
	count, err := WriteLiveIntents(dir, snapshot)
	if err != nil || count != 0 {
		t.Fatalf("uncalibrated intent written: count=%d err=%v", count, err)
	}
	dist.CalibrationReady, dist.ProbabilityStatus = true, "CALIBRATED"
	count, err = WriteLiveIntents(dir, snapshot)
	if err != nil || count != 1 {
		t.Fatalf("calibrated intent not written: count=%d err=%v", count, err)
	}
	files, err := os.ReadDir(filepath.Join(dir, "live-outbox"))
	if err != nil || len(files) != 1 {
		t.Fatalf("unexpected outbox: files=%d err=%v", len(files), err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "live-outbox", files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var intent LiveIntent
	if err := json.Unmarshal(body, &intent); err != nil {
		t.Fatal(err)
	}
	if intent.AmountDollars != 1 || intent.MaxSpendDollars != 1.10 {
		t.Fatalf("wrong principal/fee sizing: %+v", intent)
	}
}

func TestLiveDedupIsExactDatedMarket(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "live-ledger.json"), []byte(`{"attempts":[{"market_id":"day15-bin27","side":"BUY","status":"matched"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	d := &domain.Distribution{CalibrationReady: true, ProbabilityStatus: "CALIBRATED", DataQuality: .9}
	s := domain.Signal{Action: "BUY_LIVE", BookVerified: true, NetEdge: .1, MarketConsensus: .2, SuggestedDollars: 1, MarketID: "day15-bin27", TokenID: "12345678901", GeneratedAt: time.Now()}
	next := s
	next.MarketID = "day16-bin27"
	n, err := WriteLiveIntents(dir, domain.Snapshot{Reports: []domain.EventReport{{Distribution: d, Signals: []domain.Signal{s, next}}}})
	if err != nil || n != 1 {
		t.Fatalf("dated dedup count=%d err=%v", n, err)
	}
}

func TestLiveOneTemperaturePerCityDay(t *testing.T) {
	dir:=t.TempDir();d:=&domain.Distribution{CalibrationReady:true,ProbabilityStatus:"CALIBRATED",DataQuality:.9}
	s:=domain.Signal{Action:"BUY_LIVE",BookVerified:true,NetEdge:.1,MarketConsensus:.2,SuggestedDollars:1,MarketID:"bin26",TokenID:"12345678901",GeneratedAt:time.Now()};other:=s;other.MarketID="bin27";other.TokenID="12345678902";other.NetEdge=.2
	n,err:=WriteLiveIntents(dir,domain.Snapshot{Reports:[]domain.EventReport{{Rule:domain.Rule{StationICAO:"TEST",LocalDate:"2026-09-15"},Distribution:d,Signals:[]domain.Signal{s,other}}}})
	if err!=nil||n!=1{t.Fatalf("same city/date emitted %d err=%v",n,err)}
}
