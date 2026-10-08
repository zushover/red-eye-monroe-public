package model

import (
	"testing"
	"time"
	_ "time/tzdata"
	"weatherbot/internal/domain"
)

func TestPreDayAndProxyFloor(t *testing.T) {
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	station := domain.Station{ICAO: "TEST", Timezone: "UTC", BaseSigmaC: 1}
	forecast := completeForecast(now, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), 30)
	sources := map[string]domain.Forecast{"ECMWF": forecast, "GFS": forecast}
	d, err := BuildEnsemble(domain.Rule{LocalDate: "2026-09-15", Unit: "C"}, station, nil, forecast, sources, now)
	if err != nil || d.Phase != "PRE_DAY" || d.HasObservation {
		t.Fatalf("pre-day forecast unavailable: %+v %v", d, err)
	}
	total := 0.0
	for _, p := range d.IntegerProbByUnit {
		total += p
	}
	if total < .999999 || total > 1.000001 {
		t.Fatal("probability mass lost")
	}
	forecast = completeForecast(now, time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), 30)
	sources = map[string]domain.Forecast{"ECMWF": forecast, "GFS": forecast}
	obs := []domain.Observation{{Station: "TEST", ObservedAt: now, ReceivedAt: now, TempC: 30}}
	d, err = BuildEnsemble(domain.Rule{LocalDate: "2026-09-14", Unit: "C"}, station, obs, forecast, sources, now)
	if err != nil || d.IntegerProbByUnit[29] <= 0 {
		t.Fatalf("METAR must not set settlement probability to zero: %+v %v", d, err)
	}
	obs[0].ObservedAt = now.Add(-2 * time.Hour)
	d, err = BuildEnsemble(domain.Rule{LocalDate: "2026-09-14", Unit: "C"}, station, obs, forecast, sources, now)
	if err != nil || d.Confidence != 0 {
		t.Fatal("stale data must block intraday trading")
	}
}

func completeForecast(received, day time.Time, peak float64) domain.Forecast {
	f := domain.Forecast{ReceivedAt: received, Timezone: "UTC"}
	for i := 0; i < 24; i++ {
		f.Hourly = append(f.Hourly, domain.ForecastH{LocalTime: day.Add(time.Duration(i) * time.Hour), TempC: peak - float64(absInt(i-15))*.1, CloudAvailable: true, PrecipAvailable: true})
	}
	return f
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
