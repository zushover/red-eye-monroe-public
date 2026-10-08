package model

import (
	"math"
	"testing"
	"time"

	"weatherbot/internal/domain"
)

func TestExplicitModelsSetEnsembleCentre(t *testing.T) {
	now := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)
	station := domain.Station{ICAO: "TEST", Timezone: "UTC", BaseSigmaC: 1}
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	best := completeForecast(now, day, 40)
	sources := map[string]domain.Forecast{
		"ECMWF": completeForecast(now, day, 20),
		"GFS":   completeForecast(now, day, 22),
	}
	d, err := BuildEnsemble(domain.Rule{LocalDate: "2026-09-15", Unit: "C"}, station, nil, best, sources, now)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(d.ForecastPeakC-21) > 1e-9 || d.EnsembleSourceCount != 2 || math.Abs(d.EnsembleSpreadC-2) > 1e-9 {
		t.Fatalf("unexpected ensemble summary: %+v", d)
	}
	if d.ProbabilityStatus != "UNCALIBRATED_EXPERIMENTAL" || d.CalibrationReady {
		t.Fatalf("unvalidated model must stay visibly uncalibrated: %+v", d)
	}
}

func TestIncompleteOrStaleExplicitModelIsRejected(t *testing.T) {
	now := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	station := domain.Station{ICAO: "TEST", Timezone: "UTC", BaseSigmaC: 1}
	best := completeForecast(now, day, 20)
	incomplete := completeForecast(now, day, 20)
	incomplete.Hourly = incomplete.Hourly[:23]
	_, err := BuildEnsemble(domain.Rule{LocalDate: "2026-09-15", Unit: "C"}, station, nil, best, map[string]domain.Forecast{"ECMWF": incomplete, "GFS": best}, now)
	if err == nil {
		t.Fatal("incomplete explicit model must be rejected")
	}
	stale := completeForecast(now.Add(-3*time.Hour), day, 20)
	_, err = BuildEnsemble(domain.Rule{LocalDate: "2026-09-15", Unit: "C"}, station, nil, best, map[string]domain.Forecast{"ECMWF": stale, "GFS": best}, now)
	if err == nil {
		t.Fatal("stale explicit model must be rejected")
	}
}

func TestMemberDistributionReplacesGuessedWidth(t *testing.T) {
	now := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	station := domain.Station{ICAO: "TEST", Timezone: "UTC", BaseSigmaC: 1.5}
	best := completeForecast(now, day, 20)
	sources := map[string]domain.Forecast{"ECMWF": completeForecast(now, day, 20), "GFS": completeForecast(now, day, 22)}
	baseline, err := BuildEnsemble(domain.Rule{LocalDate: "2026-09-15", Unit: "C"}, station, nil, best, sources, now)
	if err != nil {
		t.Fatal(err)
	}
	members := make([]float64, 51)
	for i := range members {
		members[i] = 20 + float64(i%3-1)*.1
	}
	probabilistic, err := BuildProbabilistic(domain.Rule{LocalDate: "2026-09-15", Unit: "C"}, station, nil, best, sources, members, now)
	if err != nil {
		t.Fatal(err)
	}
	if probabilistic.EnsembleMemberCount != 51 || probabilistic.IntegerProbByUnit[20] <= baseline.IntegerProbByUnit[20] {
		t.Fatalf("member distribution was not used: baseline=%v member=%+v", baseline.IntegerProbByUnit[20], probabilistic)
	}
}

func TestCityCalibrationPriorContinuesIntoIntraday(t *testing.T) {
	now := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)
	station := domain.Station{
		ICAO: "TEST", Timezone: "UTC", BaseSigmaC: 2,
		CityCalibrationVersion: "city-test-v1", CityCalibrationLeadH: 24,
		CityCalibrationSamples: 500, CityCalibrationHoldout: 150, CityCalibrationSigmaC: 1, CityCalibrationValid: true,
		CityCalibrationModel: "ALL_ERROR_WEIGHTED", CityHoldoutMAEC: .8, CityBaselineMAEC: 1.1, CityHoldoutLogLoss: 1.4, CityBaselineLogLoss: 1.8,
		CitySourceBiasC: map[string]float64{
			"ECMWF IFS 0.25°": 1, "NCEP GFS seamless": -.5, "Open-Meteo best match": .2,
		},
		CitySourceWeight: map[string]float64{
			"ECMWF IFS 0.25°": .6, "NCEP GFS seamless": .2, "Open-Meteo best match": .2,
		},
	}
	tomorrow := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	best := completeForecast(now, tomorrow, 40)
	sources := map[string]domain.Forecast{
		"ECMWF IFS 0.25°":   completeForecast(now, tomorrow, 20),
		"NCEP GFS seamless": completeForecast(now, tomorrow, 22),
	}
	d, err := BuildEnsemble(domain.Rule{LocalDate: "2026-09-15", Unit: "C"}, station, nil, best, sources, now)
	if err != nil {
		t.Fatal(err)
	}
	want := .6*21 + .2*21.5 + .2*40.2
	if math.Abs(d.ForecastPeakC-want) > 1e-9 || math.Abs(d.SigmaC-1) > 1e-9 || !d.CalibrationReady || d.ProbabilityStatus != "CALIBRATED" {
		t.Fatalf("pre-day city calibration not applied: want centre %.3f, got %+v", want, d)
	}
	if d.CalibrationSamples != 500 || d.CalibrationHoldout != 150 || d.ModelVersion != "city-test-v1" {
		t.Fatalf("calibration provenance missing: %+v", d)
	}

	today := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	best = completeForecast(now, today, 40)
	sources = map[string]domain.Forecast{
		"ECMWF IFS 0.25°":   completeForecast(now, today, 20),
		"NCEP GFS seamless": completeForecast(now, today, 22),
	}
	obs := []domain.Observation{{Station: "TEST", ObservedAt: now, ReceivedAt: now, TempC: 18}}
	d, err = BuildEnsemble(domain.Rule{LocalDate: "2026-09-14", Unit: "C"}, station, obs, best, sources, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.CalibrationReady || d.ProbabilityStatus != "CITY_CALIBRATED_PRIOR_INTRADAY_RESEARCH" || d.ModelVersion != "city-test-v1" || !d.IndependentModelActive {
		t.Fatalf("intraday prior should remain active but require trajectory evidence: %+v", d)
	}
	obs = []domain.Observation{
		{Station: "TEST", ObservedAt: today.Add(4 * time.Hour), ReceivedAt: now, TempC: 23.84},
		{Station: "TEST", ObservedAt: today.Add(5 * time.Hour), ReceivedAt: now, TempC: 23.94},
		{Station: "TEST", ObservedAt: today.Add(6 * time.Hour), ReceivedAt: now, TempC: 24.04},
	}
	d, err = BuildEnsemble(domain.Rule{LocalDate: "2026-09-14", Unit: "C"}, station, obs, best, sources, now)
	if err != nil {
		t.Fatal(err)
	}
	if !d.CalibrationReady || d.ProbabilityStatus != "CALIBRATED" || d.TrajectoryPoints != 3 || d.TrajectoryFit < .55 {
		t.Fatalf("well-fitted fresh intraday observations should activate calibrated city prior: %+v", d)
	}
}

func TestIntradayProbabilityConfidenceIsDrivenByTrajectoryFit(t *testing.T) {
	station := domain.Station{
		CityCalibrationValid: true, CityCalibrationHoldout: 160,
		CityHoldoutMAEC: .8, CityBaselineMAEC: 1.2,
		CityHoldoutLogLoss: 1.4, CityBaselineLogLoss: 1.9,
	}
	closeFit := trajectoryScore{Score: .92, MAE: .25, Bias: .1, Direction: .95, N: 8}
	poorFit := trajectoryScore{Score: .22, MAE: 2.8, Bias: 2.4, Direction: .15, N: 8}
	closeConfidence := probabilityConfidence(station, .85, .75, "INTRADAY", closeFit)
	poorConfidence := probabilityConfidence(station, .85, .75, "INTRADAY", poorFit)
	if closeConfidence <= poorConfidence+.35 || closeConfidence < .75 || poorConfidence > .55 {
		t.Fatalf("trajectory fit must dominate intraday confidence: close=%.3f poor=%.3f", closeConfidence, poorConfidence)
	}
}
