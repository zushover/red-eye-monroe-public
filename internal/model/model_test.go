package model

import (
	"math"
	"testing"
	"time"

	"weatherbot/internal/domain"
)

func TestPhaseForTimeUsesOvernightMorningAndWarmingStages(t *testing.T) {
	target := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	peak := target.Add(15 * time.Hour)
	tests := []struct {
		hour int
		want string
	}{{1, "OVERNIGHT"}, {5, "OVERNIGHT"}, {6, "MORNING"}, {8, "MORNING"}, {9, "INTRADAY"}, {14, "INTRADAY"}, {16, "LATE_DAY"}}
	for _, tt := range tests {
		if got := phaseForTime(target.Add(time.Duration(tt.hour)*time.Hour), target, peak); got != tt.want {
			t.Fatalf("hour %d: got %s want %s", tt.hour, got, tt.want)
		}
	}
}

func TestIntegerDistributionLocksObservedFloor(t *testing.T) {
	p := integerDistribution(30.2, 1.0, 30.0)
	var sum float64
	for n, v := range p {
		if n < 30 {
			t.Fatalf("probability below reached maximum: %d=%v", n, v)
		}
		sum += v
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Fatalf("distribution sum=%v", sum)
	}
}

func TestLateDayReanchorsStaleForecastToObservedMaximum(t *testing.T) {
	loc := time.UTC
	target := time.Date(2026, 9, 14, 0, 0, 0, 0, loc)
	now := target.Add(17 * time.Hour)
	forecast := func(peak float64) domain.Forecast {
		f := domain.Forecast{ReceivedAt: now.Add(-10 * time.Minute), Timezone: "UTC"}
		for h := 0; h < 24; h++ {
			temp := 15.0
			if h >= 10 && h <= 14 {
				temp = peak - math.Abs(float64(h-14))
			}
			f.Hourly = append(f.Hourly, domain.ForecastH{LocalTime: target.Add(time.Duration(h) * time.Hour), TempC: temp, CloudAvailable: true, PrecipAvailable: true})
		}
		return f
	}
	obs := []domain.Observation{}
	for _, h := range []int{12, 14, 16} {
		obs = append(obs, domain.Observation{Station: "TEST", ObservedAt: target.Add(time.Duration(h) * time.Hour), ReceivedAt: target.Add(time.Duration(h)*time.Hour + time.Minute), TempC: 22})
	}
	d, err := BuildEnsemble(
		domain.Rule{LocalDate: "2026-09-14", Unit: "C"},
		domain.Station{ICAO: "TEST", Timezone: "UTC", BaseSigmaC: 1.5},
		obs, forecast(25),
		map[string]domain.Forecast{"ECMWF": forecast(25), "GFS": forecast(24.5)}, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if d.Phase != "LATE_DAY" || math.Abs(d.MeanC-22) > .01 || d.SigmaC > .66 {
		t.Fatalf("late distribution kept stale forecast: %+v", d)
	}
}
