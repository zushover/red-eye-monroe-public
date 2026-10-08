package store

import (
	"testing"
	"time"
	"weatherbot/internal/domain"
)

func TestCompactDoesNotDestroyExecutionInputs(t *testing.T) {
	now := time.Now()
	s := domain.Snapshot{Reports: []domain.EventReport{{Signals: []domain.Signal{{TokenID: "token"}}, Observations: []domain.Observation{{ObservedAt: now, TempC: 21}, {ObservedAt: now.Add(-time.Hour), TempC: 20}}}}}
	c := compactSnapshot(s)
	if s.Reports[0].Signals[0].TokenID != "token" {
		t.Fatal("compaction mutated caller's order token")
	}
	if len(c.Reports[0].Observations) != 0 || len(c.Reports[0].Curves["METAR"]) != 2 {
		t.Fatal("expected observations to be retained as compact curve points")
	}
}
