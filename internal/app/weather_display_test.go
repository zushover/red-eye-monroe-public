package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
	"weatherbot/internal/domain"
)

func TestStaleDisplayCannotTrade(t *testing.T) {
	dir := t.TempDir()
	at := time.Now().Add(-2 * time.Hour)
	old := domain.EventReport{Event: domain.Event{ID: "one"}, Status: "READY", Signals: []domain.Signal{{Action: "BUY_LIVE", NewEntryAllowed: true, GeneratedAt: at}}}
	b, _ := json.Marshal(map[string]domain.EventReport{"one": old})
	os.WriteFile(filepath.Join(dir, "weather-last-good.json"), b, 0600)
	a := App{cfg: domain.Config{DataDirectory: dir}}
	reports := []domain.EventReport{{Event: domain.Event{ID: "one"}, Status: "ERROR", Error: "quota"}}
	a.preserveWeatherDisplay(reports)
	r := reports[0]
	if r.Status != "STALE" || r.Signals[0].Action != "SKIP" || r.Signals[0].NewEntryAllowed || !r.Signals[0].GeneratedAt.Equal(at) {
		t.Fatalf("unsafe fallback: %+v", r)
	}
}
