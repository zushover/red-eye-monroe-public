// Package research is a standalone meteorological viewer; no market or execution imports.
package research

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
	"weatherbot/internal/client"
	"weatherbot/internal/domain"
)

//go:embed index.html
var page []byte

type Point struct {
	At time.Time `json:"at"`
	C  float64   `json:"c"`
}
type Run struct {
	Model    string    `json:"model"`
	Received time.Time `json:"received"`
	Points   []Point   `json:"points"`
	Error    string    `json:"error,omitempty"`
}
type Day struct {
	Quality  Quality `json:"quality"`
	Date     string  `json:"date"`
	Observed []Point `json:"observed"`
	Runs     []Run   `json:"runs"`
}
type Site struct {
	ICAO  string `json:"icao"`
	Name  string `json:"name"`
	Zone  string `json:"zone"`
	Days  []Day  `json:"days"`
	Error string `json:"error,omitempty"`
}
type Snapshot struct {
	At    time.Time `json:"at"`
	Sites []Site    `json:"sites"`
}
type Server struct {
	mu       sync.RWMutex
	current  Snapshot
	history  []Snapshot
	stations []domain.Station
	weather  *client.Weather
	http     *client.HTTP
	root     string
}

func New(stations []domain.Station, root string) *Server {
	h := client.NewHTTP(20 * time.Second)
	return &Server{stations: stations, http: h, weather: client.NewWeather(h), root: root}
}
func (s *Server) forecast(ctx context.Context, st domain.Station, model string) Run {
	r := Run{Model: model, Points: []Point{}}
	q := url.Values{"latitude": {strconv.FormatFloat(st.Latitude, 'f', 5, 64)}, "longitude": {strconv.FormatFloat(st.Longitude, 'f', 5, 64)}, "hourly": {"temperature_2m"}, "forecast_days": {"2"}, "timezone": {st.Timezone}, "models": {model}, "timeformat": {"unixtime"}}
	var raw struct {
		Hourly struct {
			Time []int64    `json:"time"`
			Temp []*float64 `json:"temperature_2m"`
		} `json:"hourly"`
	}
	if err := s.http.GetJSON(ctx, "https://api.open-meteo.com/v1/forecast?"+q.Encode(), &raw); err != nil {
		r.Error = "forecast unavailable"
		return r
	}
	r.Received = time.Now().UTC()
	for i, t := range raw.Hourly.Time {
		if i < len(raw.Hourly.Temp) && raw.Hourly.Temp[i] != nil {
			r.Points = append(r.Points, Point{time.Unix(t, 0).UTC(), *raw.Hourly.Temp[i]})
		}
	}
	if len(r.Points) == 0 {
		r.Error = "no valid temperature samples"
	}
	return r
}

func LocalDates(now time.Time, zone string) ([]string, error) {
	l, e := time.LoadLocation(zone)
	if e != nil {
		return nil, e
	}
	n := now.In(l)
	return []string{n.Format("2006-01-02"), n.AddDate(0, 0, 1).Format("2006-01-02")}, nil
}
func (s *Server) Collect(ctx context.Context) error {
	s.mu.RLock()
	previous := append([]Snapshot(nil), s.history...)
	s.mu.RUnlock()
	snap := Snapshot{At: time.Now().UTC(), Sites: []Site{}}
	for _, st := range s.stations {
		dates, err := LocalDates(time.Now(), st.Timezone)
		if err != nil {
			return err
		}
		loc, _ := time.LoadLocation(st.Timezone)
		site := Site{ICAO: st.ICAO, Name: st.Name, Zone: st.Timezone, Days: []Day{}}
		obs, err := s.weather.Observations(ctx, st.ICAO, 36)
		if err != nil {
			site.Error = "METAR unavailable"
		}
		runs := []Run{s.forecast(ctx, st, "ecmwf_ifs025"), s.forecast(ctx, st, "gfs_seamless")}
		for _, date := range dates {
			day := Day{Date: date, Observed: []Point{}, Runs: []Run{}}
			for _, o := range obs {
				if o.Station == st.ICAO && !o.ObservedAt.After(time.Now()) && o.ObservedAt.In(loc).Format("2006-01-02") == date {
					day.Observed = append(day.Observed, Point{o.ObservedAt, o.TempC})
				}
			}
			sort.Slice(day.Observed, func(i, j int) bool { return day.Observed[i].At.Before(day.Observed[j].At) })
			for _, r := range runs {
				filtered := Run{Model: r.Model, Received: r.Received, Points: []Point{}, Error: r.Error}
				for _, p := range r.Points {
					if p.At.In(loc).Format("2006-01-02") == date {
						filtered.Points = append(filtered.Points, p)
					}
				}
				day.Runs = append(day.Runs, filtered)
			}
			site.Days = append(site.Days, day)
		}
		snap.Sites = append(snap.Sites, site)
	}
	snap.At = time.Now().UTC()
	for i := range snap.Sites {
		for j := range snap.Sites[i].Days {
			st := &snap.Sites[i]
			st.Days[j].Quality = Assess(st.ICAO, st.Zone, st.Days[j], previous, snap.At)
		}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	dir := filepath.Join(s.root, "archive", snap.At.Format("2006-01-02"))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, snap.At.Format("150405.000000000")+".json"), b, 0600); err != nil {
		return err
	}
	s.mu.Lock()
	s.current = snap
	s.history = append(s.history, snap)
	if len(s.history) > 144 {
		s.history = s.history[len(s.history)-144:]
	}
	s.mu.Unlock()
	return nil
}
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	m.HandleFunc("/api/weather", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if s.current.At.IsZero() {
			http.Error(w, "collecting", 503)
			return
		}
		json.NewEncoder(w).Encode(s.current)
	})
	m.HandleFunc("/api/history", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.history)
	})
	return m
}
func (s *Server) Run(ctx context.Context) {
	for {
		if err := s.Collect(ctx); err != nil {
			fmt.Println("Weather collection failed:", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute):
		}
	}
}
