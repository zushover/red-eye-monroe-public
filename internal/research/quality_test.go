package research

import (
	"testing"
	"time"
)

func fixture(now time.Time) Day {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	d := Day{Date: start.Format("2006-01-02"), Observed: []Point{{now.Add(-10 * time.Minute), 20}}}
	for _, name := range []string{"a", "b"} {
		r := Run{Model: name, Received: now}
		for i := 0; i < 24; i++ {
			r.Points = append(r.Points, Point{start.Add(time.Duration(i) * time.Hour), 20})
		}
		d.Runs = append(d.Runs, r)
	}
	return d
}
func TestQualityNeedsRepeatedEvidence(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	d := fixture(now)
	if Assess("TEST", "UTC", d, nil, now).State != "继续观察" {
		t.Fatal("one sample cannot prove stability")
	}
	var h []Snapshot
	for _, mins := range []int{21, 10} {
		at := now.Add(-time.Duration(mins) * time.Minute)
		h = append(h, Snapshot{At: at, Sites: []Site{{ICAO: "TEST", Days: []Day{fixture(at)}}}})
	}
	if Assess("TEST", "UTC", d, h, now).State != "趋于稳定" {
		t.Fatal("repeated stable samples not recognized")
	}
	d.Runs[0].Points = nil
	if Assess("TEST", "UTC", d, h, now).State != "数据不足" {
		t.Fatal("missing forecast marked stable")
	}
}
func TestQualityStaleObservationsAndDisagreement(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	d := fixture(now)
	d.Runs[0].Points[0].C = 22
	if Assess("TEST", "UTC", d, nil, now).State != "变化明显" {
		t.Fatal("disagreement missed")
	}
	d.Observed[0].At = now.Add(-2 * time.Hour)
	if Assess("TEST", "UTC", d, nil, now).State != "数据不足" {
		t.Fatal("stale observation allowed")
	}
}
