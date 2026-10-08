package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"weatherbot/internal/domain"
)

type Weather struct {
	http         *HTTP
	mu           sync.Mutex
	forecasts    map[string]domain.Forecast
	observations map[string][]domain.Observation
	memberPeaks  map[string][]float64
	memberAt     map[string]time.Time
	memberFailed map[string]time.Time
}

func NewWeather(h *HTTP) *Weather {
	return &Weather{http: h, forecasts: map[string]domain.Forecast{}, observations: map[string][]domain.Observation{}, memberPeaks: map[string][]float64{}, memberAt: map[string]time.Time{}, memberFailed: map[string]time.Time{}}
}

type openMeteoEnsemble struct {
	Timezone string                       `json:"timezone"`
	Hourly   map[string][]json.RawMessage `json:"hourly"`
}

// EnsembleDailyMax returns one local-day maximum per perturbed ensemble
// member.  The control member is included; missing member values are ignored.
func (w *Weather) EnsembleDailyMax(ctx context.Context, s domain.Station, localDate, ensembleModel string, days int) ([]float64, error) {
	cacheKey := s.ICAO + ":members:" + ensembleModel + ":" + localDate
	w.mu.Lock()
	cached := append([]float64(nil), w.memberPeaks[cacheKey]...)
	cachedAt := w.memberAt[cacheKey]
	failedAt := w.memberFailed[cacheKey]
	w.mu.Unlock()
	if len(cached) > 0 && time.Since(cachedAt) < 30*time.Minute {
		return cached, nil
	}
	if !failedAt.IsZero() && time.Since(failedAt) < 30*time.Minute {
		return nil, fmt.Errorf("ensemble endpoint temporarily unavailable (cached)")
	}
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(s.Latitude, 'f', 5, 64))
	q.Set("longitude", strconv.FormatFloat(s.Longitude, 'f', 5, 64))
	q.Set("hourly", "temperature_2m")
	q.Set("models", ensembleModel)
	q.Set("forecast_days", strconv.Itoa(days))
	q.Set("past_days", "1")
	tz := s.Timezone
	if tz == "" {
		tz = "auto"
	}
	q.Set("timezone", tz)
	var raw openMeteoEnsemble
	if err := w.http.GetJSON(ctx, "https://ensemble-api.open-meteo.com/v1/ensemble?"+q.Encode(), &raw); err != nil {
		w.mu.Lock()
		w.memberFailed[cacheKey] = time.Now()
		w.mu.Unlock()
		return nil, err
	}
	timesRaw := raw.Hourly["time"]
	times := make([]string, len(timesRaw))
	for i, value := range timesRaw {
		_ = json.Unmarshal(value, &times[i])
	}
	peaks := []float64{}
	for name, values := range raw.Hourly {
		if !strings.HasPrefix(name, "temperature_2m") {
			continue
		}
		peak, found := -100.0, false
		for i, value := range values {
			if i >= len(times) || !strings.HasPrefix(times[i], localDate+"T") || string(value) == "null" {
				continue
			}
			var temperature float64
			if json.Unmarshal(value, &temperature) == nil && temperature > peak {
				peak, found = temperature, true
			}
		}
		if found {
			peaks = append(peaks, peak)
		}
	}
	if len(peaks) < 20 {
		w.mu.Lock()
		w.memberFailed[cacheKey] = time.Now()
		w.mu.Unlock()
		return nil, fmt.Errorf("ensemble member coverage incomplete: %d", len(peaks))
	}
	w.mu.Lock()
	w.memberPeaks[cacheKey] = append([]float64(nil), peaks...)
	w.memberAt[cacheKey] = time.Now()
	delete(w.memberFailed, cacheKey)
	w.mu.Unlock()
	return peaks, nil
}

type metarRow struct {
	ICAOId  string      `json:"icaoId"`
	ObsTime int64       `json:"obsTime"`
	Temp    *float64    `json:"temp"`
	Dewp    float64     `json:"dewp"`
	Wdir    flexibleInt `json:"wdir"`
	Wspd    flexibleInt `json:"wspd"`
	RawOb   string      `json:"rawOb"`
}

type flexibleInt int

func (v *flexibleInt) UnmarshalJSON(b []byte) error {
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		*v = flexibleInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		n, _ = strconv.Atoi(s)
		*v = flexibleInt(n)
		return nil
	}
	if string(b) == "null" {
		*v = 0
		return nil
	}
	return fmt.Errorf("invalid integer %s", string(b))
}

func (w *Weather) Observations(ctx context.Context, station string, hours int) ([]domain.Observation, error) {
	w.mu.Lock()
	cached := w.observations[station]
	w.mu.Unlock()
	if len(cached) > 0 && time.Since(cached[0].ReceivedAt) < 90*time.Second {
		return cached, nil
	}
	q := url.Values{}
	q.Set("ids", station)
	q.Set("format", "json")
	q.Set("hours", strconv.Itoa(hours))
	var rows []metarRow
	if err := w.http.GetJSON(ctx, "https://aviationweather.gov/api/data/metar?"+q.Encode(), &rows); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]domain.Observation, 0, len(rows))
	for _, r := range rows {
		if r.Temp == nil {
			continue
		}
		out = append(out, domain.Observation{
			Station: r.ICAOId, ObservedAt: time.Unix(r.ObsTime, 0).UTC(), ReceivedAt: now,
			TempC: *r.Temp, DewpointC: r.Dewp, WindDir: int(r.Wdir), WindSpeed: int(r.Wspd), Raw: r.RawOb,
		})
	}
	w.mu.Lock()
	w.observations[station] = out
	w.mu.Unlock()
	return out, nil
}

type openMeteo struct {
	Timezone string `json:"timezone"`
	Hourly   struct {
		Time              []string   `json:"time"`
		Temperature       []*float64 `json:"temperature_2m"`
		CloudCover        []*float64 `json:"cloud_cover"`
		PrecipProbability []*float64 `json:"precipitation_probability"`
		WindSpeed         []*float64 `json:"wind_speed_10m"`
	} `json:"hourly"`
}

func (w *Weather) Forecast(ctx context.Context, s domain.Station, days int) (domain.Forecast, error) {
	return w.forecast(ctx, s, days, "")
}

// ForecastModel retrieves a named model rather than Open-Meteo's automatic best match.
func (w *Weather) ForecastModel(ctx context.Context, s domain.Station, days int, model string) (domain.Forecast, error) {
	if strings.TrimSpace(model) == "" {
		return domain.Forecast{}, fmt.Errorf("weather model is required")
	}
	return w.forecast(ctx, s, days, model)
}

func (w *Weather) forecast(ctx context.Context, s domain.Station, days int, model string) (domain.Forecast, error) {
	cacheKey := fmt.Sprintf("%s:%s:%d", s.ICAO, model, days)
	w.mu.Lock()
	cached, ok := w.forecasts[cacheKey]
	w.mu.Unlock()
	if ok && time.Since(cached.ReceivedAt) < time.Hour {
		return cached, nil
	}
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(s.Latitude, 'f', 5, 64))
	q.Set("longitude", strconv.FormatFloat(s.Longitude, 'f', 5, 64))
	q.Set("hourly", "temperature_2m,cloud_cover,precipitation_probability,wind_speed_10m")
	q.Set("forecast_days", strconv.Itoa(days))
	q.Set("past_days", "1")
	if model != "" {
		q.Set("models", model)
	}
	tz := s.Timezone
	if tz == "" {
		tz = "auto"
	}
	q.Set("timezone", tz)
	var raw openMeteo
	if err := w.http.GetJSON(ctx, "https://api.open-meteo.com/v1/forecast?"+q.Encode(), &raw); err != nil {
		return domain.Forecast{}, err
	}
	loc, err := time.LoadLocation(raw.Timezone)
	if err != nil {
		return domain.Forecast{}, fmt.Errorf("load timezone: %w", err)
	}
	f := domain.Forecast{ReceivedAt: time.Now().UTC(), Timezone: raw.Timezone}
	for i, ts := range raw.Hourly.Time {
		if i >= len(raw.Hourly.Temperature) || raw.Hourly.Temperature[i] == nil {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02T15:04", ts, loc)
		if err != nil {
			continue
		}
		h := domain.ForecastH{LocalTime: t, TempC: *raw.Hourly.Temperature[i]}
		if i < len(raw.Hourly.CloudCover) && raw.Hourly.CloudCover[i] != nil {
			h.CloudPct = *raw.Hourly.CloudCover[i]
			h.CloudAvailable = true
		}
		if i < len(raw.Hourly.PrecipProbability) && raw.Hourly.PrecipProbability[i] != nil {
			h.PrecipProbPct = *raw.Hourly.PrecipProbability[i]
			h.PrecipAvailable = true
		}
		if i < len(raw.Hourly.WindSpeed) && raw.Hourly.WindSpeed[i] != nil {
			h.WindSpeedKPH = *raw.Hourly.WindSpeed[i]
			h.WindAvailable = true
		}
		f.Hourly = append(f.Hourly, h)
	}
	if len(f.Hourly) == 0 {
		return f, fmt.Errorf("empty hourly forecast")
	}
	w.mu.Lock()
	w.forecasts[cacheKey] = f
	w.mu.Unlock()
	return f, nil
}

type stationInfo struct {
	ICAOID string  `json:"icaoId"`
	Site   string  `json:"site"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
}

func (w *Weather) Station(ctx context.Context, icao string) (domain.Station, error) {
	q := url.Values{}
	q.Set("ids", strings.ToUpper(icao))
	q.Set("format", "json")
	var payload json.RawMessage
	if err := w.http.GetJSON(ctx, "https://aviationweather.gov/api/data/stationinfo?"+q.Encode(), &payload); err != nil {
		return domain.Station{}, err
	}
	var raw stationInfo
	trimmed := strings.TrimSpace(string(payload))
	if strings.HasPrefix(trimmed, "[") {
		var rows []stationInfo
		if err := json.Unmarshal(payload, &rows); err != nil {
			return domain.Station{}, err
		}
		if len(rows) > 0 {
			raw = rows[0]
		}
	} else if err := json.Unmarshal(payload, &raw); err != nil {
		return domain.Station{}, err
	}
	if raw.ICAOID == "" || raw.Lat == 0 || raw.Lon == 0 {
		return domain.Station{}, fmt.Errorf("incomplete station metadata for %s", icao)
	}
	return domain.Station{ICAO: strings.ToUpper(raw.ICAOID), Name: raw.Site, Latitude: raw.Lat, Longitude: raw.Lon, BaseSigmaC: 1.25}, nil
}
