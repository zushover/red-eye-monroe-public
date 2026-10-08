package rules

import (
	"math"
	"testing"

	"weatherbot/internal/domain"
)

func TestParseNOAAEvent(t *testing.T) {
	e := domain.Event{
		Title:            "Highest temperature in NYC on September 14?",
		EndDate:          "2026-09-14T12:00:00Z",
		ResolutionSource: "https://www.weather.gov/wrh/timeseries?site=klga",
		Description:      "This resolves at LaGuardia in degrees Fahrenheit on 14 Sep '26. The source measures temperatures to whole degrees Fahrenheit. Revisions will be considered.",
	}
	r := ParseEvent(e)
	if !r.Safe {
		t.Fatalf("expected safe rule, reasons=%v", r.Reasons)
	}
	if r.StationICAO != "KLGA" || r.Unit != "F" || r.LocalDate != "2026-09-14" || r.Source != "NOAA" {
		t.Fatalf("unexpected rule: %+v", r)
	}
}

func TestParseWundergroundEvent(t *testing.T) {
	e := domain.Event{
		Title:            "Highest temperature in Busan on September 14?",
		EndDate:          "2026-09-14T12:00:00Z",
		ResolutionSource: "https://www.wunderground.com/history/daily/kr/busan/RKPK",
		Description:      "Highest temperature in degrees Celsius on 14 Sep '26. The source measures temperatures to whole degrees Celsius.",
	}
	r := ParseEvent(e)
	if !r.Safe || r.StationICAO != "RKPK" || r.Unit != "C" {
		t.Fatalf("unexpected rule: %+v", r)
	}
}

func TestBuckets(t *testing.T) {
	probs := map[int]float64{29: .1, 30: .2, 31: .4, 32: .2, 33: .1}
	cases := []struct {
		label string
		want  float64
	}{
		{"29°C or below", .1}, {"30-31°C", .6}, {"32°C", .2}, {"33°C or higher", .1},
	}
	for _, tc := range cases {
		b, err := ParseBucket(tc.label)
		if err != nil {
			t.Fatal(err)
		}
		if got := BucketProbability(b, probs); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: got %v want %v", tc.label, got, tc.want)
		}
	}
}
