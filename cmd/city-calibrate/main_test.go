package main

import (
	"math"
	"testing"
	"time"
)

func TestNestedSelectionCanChooseSingleCitySource(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]daySample, 400)
	for i := range rows {
		observed := 25 + 4*math.Sin(float64(i)/19) + .2*math.Cos(float64(i)/5)
		rows[i] = daySample{
			Date:      start.AddDate(0, 0, i).Format("2006-01-02"),
			ObservedC: observed,
			Forecasts: map[string]float64{
				"ECMWF IFS 0.25°":       observed - .8 + .08*math.Sin(float64(i)*1.7),
				"NCEP GFS seamless":     observed + 2.8*math.Sin(float64(i)*.41),
				"Open-Meteo best match": observed + 3.2*math.Cos(float64(i)*.33),
			},
		}
	}
	result, err := calibrateSamples(pilot{ICAO: "TEST", Name: "Test", Timezone: "UTC"}, rows)
	if err != nil {
		t.Fatal(err)
	}
	if result.SelectedModel != "ECMWF_ONLY_BIAS" || !result.Accepted || len(result.Sources) != 1 {
		t.Fatalf("expected stable single-source city model, got model=%s accepted=%v sources=%+v", result.SelectedModel, result.Accepted, result.Sources)
	}
	if result.Calibrated.LogLoss >= result.Baseline.LogLoss || result.SelectionN == 0 || len(result.Candidates) < 10 {
		t.Fatalf("nested selection provenance or holdout improvement missing: %+v", result)
	}
}

func TestVerificationBucketBoundsMatchMarketUnits(t *testing.T) {
	lo, hi := verificationBucketBounds((89-32)*5.0/9.0, "KMIA")
	if math.Abs(lo-(87.5-32)*5/9) > 1e-9 || math.Abs(hi-(89.5-32)*5/9) > 1e-9 {
		t.Fatalf("US 88-89F bucket bounds are wrong: %.4f %.4f", lo, hi)
	}
	lo, hi = verificationBucketBounds(29.2, "ZBAA")
	if math.Abs(lo-28.5) > 1e-9 || math.Abs(hi-29.5) > 1e-9 {
		t.Fatalf("Celsius bucket bounds are wrong: %.4f %.4f", lo, hi)
	}
}
