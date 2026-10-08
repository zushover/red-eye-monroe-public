package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type weatherTransport func(*http.Request) (*http.Response, error)

func (f weatherTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestWeatherSharedCacheAndQuota(t *testing.T) {
	meteoGate.Lock()
	meteoGate.cache = map[string]meteoResponse{}
	meteoGate.until = time.Time{}
	meteoGate.path = ""
	meteoGate.Unlock()
	defer func() {
		meteoGate.Lock()
		meteoGate.cache = map[string]meteoResponse{}
		meteoGate.until = time.Time{}
		meteoGate.Unlock()
	}()
	calls := 0
	h := NewHTTP(time.Second)
	h.c.Transport = weatherTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		status := 200
		body := `{"value":1}`
		if strings.Contains(r.URL.RawQuery, "quota") {
			status = 429
			body = `{"reason":"Daily API request limit exceeded"}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var dst json.RawMessage
			if e := h.GetJSON(context.Background(), "https://api.open-meteo.com/test", &dst); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("concurrent cache calls=%d", calls)
	}
	var dst json.RawMessage
	if h.GetJSON(context.Background(), "https://api.open-meteo.com/test?quota", &dst) == nil {
		t.Fatal("expected quota error")
	}
	if h.GetJSON(context.Background(), "https://ensemble-api.open-meteo.com/test?city=other", &dst) == nil {
		t.Fatal("expected global cooldown")
	}
	if calls != 2 {
		t.Fatalf("quota kept sending requests: %d", calls)
	}
	if e := h.GetJSON(context.Background(), "https://api.open-meteo.com/test", &dst); e != nil {
		t.Fatal("fresh cached data should remain usable", e)
	}
}
