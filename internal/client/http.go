package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Shared across weather and research clients. Never retry a quota rejection
// per city: one rejected provider pauses all its requests.
var meteoGate = struct {
	sync.Mutex
	until time.Time
	cache map[string]meteoResponse
	path  string
}{cache: map[string]meteoResponse{}}

type meteoResponse struct {
	At   time.Time
	Body []byte
}

func ConfigureWeatherCache(path string) {
	meteoGate.Lock()
	defer meteoGate.Unlock()
	meteoGate.path = path
	var disk struct {
		Until time.Time
		Cache map[string]meteoResponse
	}
	if b, e := os.ReadFile(path); e == nil && json.Unmarshal(b, &disk) == nil {
		meteoGate.until = disk.Until
		if disk.Cache != nil {
			meteoGate.cache = disk.Cache
		}
	}
}
func saveWeatherCache() {
	if meteoGate.path == "" {
		return
	}
	b, e := json.Marshal(struct {
		Until time.Time
		Cache map[string]meteoResponse
	}{meteoGate.until, meteoGate.cache})
	if e == nil {
		tmp := meteoGate.path + ".tmp"
		if os.WriteFile(tmp, b, 0600) == nil {
			_ = os.Rename(tmp, meteoGate.path)
		}
	}
}

type HTTP struct {
	c       *http.Client
	mu      sync.Mutex
	nextAWC time.Time
}

func NewHTTP(timeout time.Duration) *HTTP {
	return &HTTP{c: &http.Client{Timeout: timeout}}
}

func (h *HTTP) GetJSON(ctx context.Context, rawURL string, dst any) error {
	u, _ := url.Parse(rawURL)
	meteo := u != nil && strings.HasSuffix(u.Hostname(), "open-meteo.com")
	if meteo {
		meteoGate.Lock()
		defer meteoGate.Unlock() // Coalesce concurrent identical requests.
		if meteoGate.path != "" {
			if _, err := os.Stat(meteoGate.path + ".resume"); err == nil {
				meteoGate.until = time.Time{}
				_ = os.Remove(meteoGate.path + ".resume")
				saveWeatherCache()
			}
		}
		if c, ok := meteoGate.cache[rawURL]; ok && time.Since(c.At) < time.Hour {
			return json.Unmarshal(c.Body, dst)
		}
		if time.Now().Before(meteoGate.until) {
			return fmt.Errorf("weather provider quota cooldown until %s", meteoGate.until.UTC().Format(time.RFC3339))
		}
	}
	if strings.HasPrefix(rawURL, "https://aviationweather.gov/") {
		h.mu.Lock()
		at := h.nextAWC
		if at.Before(time.Now()) {
			at = time.Now()
		}
		h.nextAWC = at.Add(700 * time.Millisecond)
		h.mu.Unlock()
		timer := time.NewTimer(time.Until(at))
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "weatherbot/0.1 (+local paper-trading research)")
	res, err := h.c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		if meteo && res.StatusCode == 429 {
			wait := time.Hour
			if strings.Contains(strings.ToLower(string(body)), "daily") {
				wait = 24 * time.Hour
			}
			if seconds, e := strconv.Atoi(res.Header.Get("Retry-After")); e == nil && time.Duration(seconds)*time.Second > wait {
				wait = time.Duration(seconds) * time.Second
			}
			meteoGate.until = time.Now().Add(wait)
			saveWeatherCache()
		}
		return fmt.Errorf("GET %s: status %d: %s", rawURL, res.StatusCode, string(body))
	}
	if meteo {
		body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
		if err != nil {
			return err
		}
		if err = json.Unmarshal(body, dst); err != nil {
			return fmt.Errorf("decode %s: %w", rawURL, err)
		}
		meteoGate.cache[rawURL] = meteoResponse{time.Now(), body}
		saveWeatherCache()
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}
