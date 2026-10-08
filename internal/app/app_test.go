package app

import (
	"testing"
	"time"

	"weatherbot/internal/domain"
)

func TestSelectActiveStationEventRollover(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	zone := time.FixedZone("test", 8*60*60)
	today := testTemperatureEvent("today", "September 14", .94)
	tomorrow := testTemperatureEvent("tomorrow", "September 15", .25)

	active, ok := selectActiveStationEvent([]domain.Event{today, tomorrow}, now, zone, 21*60)
	if !ok || active.ID != "today" {
		t.Fatalf("94%% convergence should keep today; got %#v, %v", active.ID, ok)
	}

	today.Markets[0].BestBid = .94
	today.Markets[0].BestAsk = .96
	active, ok = selectActiveStationEvent([]domain.Event{today, tomorrow}, now, zone, 21*60)
	if !ok || active.ID != "tomorrow" {
		t.Fatalf("95%% convergence should roll to tomorrow; got %#v, %v", active.ID, ok)
	}

	active, ok = selectActiveStationEvent([]domain.Event{today}, now, zone, 21*60)
	if !ok || active.ID != "today" {
		t.Fatalf("without a published next-day market, today should remain visible; got %#v, %v", active.ID, ok)
	}
}

func TestSelectActiveStationEventRollsAfterLocalEntryWindow(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 30, 0, 0, time.UTC)
	zone := time.UTC
	today := testTemperatureEvent("today", "September 15", .40)
	tomorrow := testTemperatureEvent("tomorrow", "September 16", .20)
	active, ok := selectActiveStationEvent([]domain.Event{today, tomorrow}, now, zone, 12*60)
	if !ok || active.ID != "tomorrow" {
		t.Fatalf("closed local entry window should roll to tomorrow; got %#v, %v", active.ID, ok)
	}
}

func TestFilterLocalTradingHorizonDropsDayAfterTomorrowButKeepsManaged(t *testing.T) {
	now := time.Date(2026, 9, 15, 6, 0, 0, 0, time.UTC)
	report := func(id, date string) domain.EventReport {
		return domain.EventReport{
			Event:   domain.Event{Markets: []domain.Market{{ID: id}}},
			Rule:    domain.Rule{LocalDate: date},
			Station: &domain.Station{Timezone: "Asia/Shanghai"},
		}
	}
	reports := []domain.EventReport{report("today", "2026-09-15"), report("tomorrow", "2026-09-16"), report("later", "2026-09-17"), report("held", "2026-09-14")}
	got := filterLocalTradingHorizon(reports, now, map[string]bool{"held": true})
	if len(got) != 3 || got[0].Rule.LocalDate != "2026-09-15" || got[1].Rule.LocalDate != "2026-09-16" || got[2].Event.Markets[0].ID != "held" {
		t.Fatalf("unexpected local horizon: %+v", got)
	}
}

func testTemperatureEvent(id, date string, midpoint float64) domain.Event {
	return domain.Event{
		ID:      id,
		Title:   "Highest temperature in Test City on " + date + "?",
		EndDate: "2026-09-16T00:00:00Z",
		Description: "The market resolves using the whole degree temperature in degrees Celsius " +
			"reported by station ZUUU.",
		ResolutionSource: "https://www.weather.gov/wrh/timeseries?site=ZUUU",
		Markets:          []domain.Market{{BestBid: midpoint - .01, BestAsk: midpoint + .01}},
	}
}
