package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"weatherbot/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWeatherEventsHonorsGammaHundredRowCap(t *testing.T) {
	requestedOffsets := []string{}
	h := &HTTP{c: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		offset := req.URL.Query().Get("offset")
		requestedOffsets = append(requestedOffsets, offset)
		if got := req.URL.Query().Get("limit"); got != "100" {
			t.Fatalf("Gamma page size = %s, want 100", got)
		}
		count := 100
		if offset == "100" {
			count = 1
		}
		page := make([]domain.Event, count)
		base, _ := strconv.Atoi(offset)
		for i := range page {
			page[i].ID = strconv.Itoa(base + i)
		}
		body, _ := json.Marshal(page)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})}}
	p := &Polymarket{http: h}
	events, err := p.WeatherEvents(context.Background(), 200, time.Now(), time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 101 {
		t.Fatalf("got %d events, want 101", len(events))
	}
	if strings.Join(requestedOffsets, ",") != "0,100" {
		t.Fatalf("offsets = %v, want [0 100]", requestedOffsets)
	}
}
