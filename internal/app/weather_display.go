package app

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"weatherbot/internal/domain"
)

// Saved reports are display-only: original observation/quote timestamps remain,
// status STALE blocks model exits and every entry signal is explicitly disabled.
func (a *App) preserveWeatherDisplay(reports []domain.EventReport) {
	path := filepath.Join(a.cfg.DataDirectory, "weather-last-good.json")
	good := map[string]domain.EventReport{}
	if b, e := os.ReadFile(path); e == nil {
		_ = json.Unmarshal(b, &good)
	} else {
		files, _ := filepath.Glob(filepath.Join(a.cfg.DataDirectory, "archive", "*", "*.json.gz"))
		sort.Sort(sort.Reverse(sort.StringSlice(files)))
		for i, file := range files {
			if i >= 500 {
				break
			}
			f, e := os.Open(file)
			if e != nil {
				continue
			}
			z, e := gzip.NewReader(f)
			if e != nil {
				f.Close()
				continue
			}
			var s domain.Snapshot
			e = json.NewDecoder(z).Decode(&s)
			z.Close()
			f.Close()
			if e != nil {
				continue
			}
			for _, r := range s.Reports {
				if r.Status == "READY" && len(r.Signals) > 0 {
					if _, ok := good[r.Event.ID]; !ok {
						good[r.Event.ID] = r
					}
				}
			}
			if len(good) >= 60 {
				break
			}
		}
	}
	for i := range reports {
		r := &reports[i]
		if r.Status == "READY" && len(r.Signals) > 0 {
			good[r.Event.ID] = *r
			continue
		}
		if r.Status != "ERROR" && r.Status != "WAITING" {
			continue
		}
		old, ok := good[r.Event.ID]
		if !ok {
			continue
		}
		old.Status = "STALE"
		old.Error = "历史快照，仅供观察；" + r.Error
		for j := range old.Signals {
			old.Signals[j].Action = "SKIP"
			old.Signals[j].NewEntryAllowed = false
			old.Signals[j].Reason = "stale weather display only"
		}
		*r = old
	}
	if b, e := json.Marshal(good); e == nil {
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0600) == nil {
			_ = os.Rename(tmp, path)
		}
	}
}
