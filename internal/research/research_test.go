package research

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestDatesUseStationCalendar(t *testing.T) {
	now := time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)
	dates, e := LocalDates(now, "America/New_York")
	if e != nil || dates[0] != "2025-12-31" || dates[1] != "2026-01-01" {
		t.Fatalf("wrong station dates %v %v", dates, e)
	}
}
func TestEmptySnapshotReturnsUnavailable(t *testing.T) {
	s := New(nil, t.TempDir())
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/weather", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
