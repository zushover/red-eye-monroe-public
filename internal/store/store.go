package store

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"weatherbot/internal/domain"
)

type Store struct {
	dir         string
	mu          sync.Mutex
	lastHistory time.Time
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Save(snapshot domain.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	compact := compactSnapshot(snapshot)
	// Complete timestamped inputs are necessary for forward-only replay.
	archiveDir := filepath.Join(s.dir, "archive", snapshot.GeneratedAt.UTC().Format("2006-01-02"))
	if err := os.MkdirAll(archiveDir, 0700); err != nil {
		return err
	}
	fraw, err := os.OpenFile(filepath.Join(archiveDir, snapshot.GeneratedAt.UTC().Format("150405.000000000")+".json.gz"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(fraw)
	encodeErr := json.NewEncoder(gz).Encode(snapshot)
	zipErr := gz.Close()
	closeErr := fraw.Close()
	if encodeErr != nil {
		return encodeErr
	}
	if zipErr != nil {
		return zipErr
	}
	if closeErr != nil {
		return closeErr
	}
	b, err := json.MarshalIndent(compact, "", "  ")
	if err != nil {
		return err
	}
	latest := filepath.Join(s.dir, "latest.json")
	tmp := latest + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, latest); err != nil {
		if removeErr := os.Remove(latest); removeErr != nil && !os.IsNotExist(removeErr) {
			return removeErr
		}
		if err := os.Rename(tmp, latest); err != nil {
			return err
		}
	}
	if !s.lastHistory.IsZero() && snapshot.GeneratedAt.Sub(s.lastHistory) < 5*time.Minute {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(s.dir, "snapshots.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, _ := json.Marshal(compact)
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append snapshot: %w", err)
	}
	s.lastHistory = snapshot.GeneratedAt
	return nil
}

func compactSnapshot(snapshot domain.Snapshot) domain.Snapshot {
	out := snapshot
	out.Reports = append([]domain.EventReport(nil), snapshot.Reports...)
	for i := range out.Reports {
		r := &out.Reports[i]
		r.Event.Description = ""
		r.Event.Markets = nil
		r.Curves = compactCurves(*r)
		r.Observations = nil
		r.Forecast = nil
		r.SourceForecasts = nil
		r.Signals = append([]domain.Signal(nil), r.Signals...)
		for j := range r.Signals {
			r.Signals[j].EventTitle = ""
			r.Signals[j].MarketQuestion = ""
		}
	}
	return out
}

func compactCurves(r domain.EventReport) map[string][]domain.CurvePoint {
	curves := make(map[string][]domain.CurvePoint)
	appendForecast := func(label string, forecast *domain.Forecast) {
		if forecast == nil {
			return
		}
		for _, h := range forecast.Hourly {
			if r.Rule.LocalDate == "" || h.LocalTime.Format("2006-01-02") == r.Rule.LocalDate {
				curves[label] = append(curves[label], domain.CurvePoint{At: h.LocalTime, C: h.TempC})
			}
		}
	}
	appendForecast("Open-Meteo best match", r.Forecast)
	for label, forecast := range r.SourceForecasts {
		f := forecast
		appendForecast(label, &f)
	}
	zone := time.UTC
	if r.Station != nil && r.Station.Timezone != "" {
		if loaded, err := time.LoadLocation(r.Station.Timezone); err == nil {
			zone = loaded
		}
	}
	for _, o := range r.Observations {
		if r.Rule.LocalDate == "" || o.ObservedAt.In(zone).Format("2006-01-02") == r.Rule.LocalDate {
			curves["METAR"] = append(curves["METAR"], domain.CurvePoint{At: o.ObservedAt, C: o.TempC})
		}
	}
	if len(curves) == 0 {
		return nil
	}
	return curves
}

func (s *Store) Latest() ([]byte, error) { return os.ReadFile(filepath.Join(s.dir, "latest.json")) }

func (s *Store) Ledger() ([]byte, error) {
	return os.ReadFile(filepath.Join(s.dir, "paper-ledger.json"))
}

func (s *Store) LiveStatus() ([]byte, error) {
	return os.ReadFile(filepath.Join(s.dir, "live-status.json"))
}

func (s *Store) LiveLedger() ([]byte, error) {
	return os.ReadFile(filepath.Join(s.dir, "live-ledger.json"))
}
