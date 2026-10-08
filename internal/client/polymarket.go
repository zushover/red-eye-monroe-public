package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"weatherbot/internal/domain"
)

type Polymarket struct{ http *HTTP }

func NewPolymarket(h *HTTP) *Polymarket { return &Polymarket{http: h} }

func (p *Polymarket) Market(ctx context.Context, id string) (domain.Market, error) {
	var m domain.Market
	err := p.http.GetJSON(ctx, "https://gamma-api.polymarket.com/markets/"+url.PathEscape(id), &m)
	return m, err
}

func (p *Polymarket) WeatherEvents(ctx context.Context, limit int, from, to time.Time) ([]domain.Event, error) {
	// Gamma caps event pages at 100 even when a larger limit is requested.
	// Respecting that cap prevents later pages (often the next day's markets)
	// from being mistaken for an empty tail.
	pageSize := limit
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 100
	}
	q := url.Values{}
	q.Set("tag_id", "84")
	q.Set("active", "true")
	q.Set("closed", "false")
	q.Set("limit", strconv.Itoa(pageSize))
	q.Set("end_date_min", from.UTC().Format(time.RFC3339))
	q.Set("end_date_max", to.UTC().Format(time.RFC3339))
	q.Set("order", "endDate")
	q.Set("ascending", "true")
	var events []domain.Event
	seen := map[string]bool{}
	for offset := 0; offset < 10000; offset += pageSize {
		q.Set("offset", strconv.Itoa(offset))
		var page []domain.Event
		if err := p.http.GetJSON(ctx, "https://gamma-api.polymarket.com/events?"+q.Encode(), &page); err != nil {
			return nil, err
		}
		added := 0
		for _, e := range page {
			if !seen[e.ID] {
				seen[e.ID] = true
				events = append(events, e)
				added++
			}
		}
		if len(page) < pageSize {
			return events, nil
		}
		if added == 0 {
			return nil, fmt.Errorf("market pagination made no progress")
		}
	}
	return nil, fmt.Errorf("market discovery pagination cap reached")
}

func (p *Polymarket) GeoBlocked(ctx context.Context) (*bool, error) {
	var v struct {
		Blocked bool `json:"blocked"`
	}
	if err := p.http.GetJSON(ctx, "https://polymarket.com/api/geoblock", &v); err != nil {
		return nil, err
	}
	return &v.Blocked, nil
}

func FilterTradeable(events []domain.Event, now time.Time, lookaheadDays int) []domain.Event {
	cutoff := now.Add(time.Duration(lookaheadDays) * 24 * time.Hour)
	out := make([]domain.Event, 0, len(events))
	for _, e := range events {
		end, err := time.Parse(time.RFC3339, e.EndDate)
		if err != nil || end.Before(now.Add(-24*time.Hour)) || end.After(cutoff) {
			continue
		}
		markets := e.Markets[:0]
		for _, m := range e.Markets {
			if m.Active && !m.Closed && m.AcceptingOrders {
				markets = append(markets, m)
			}
		}
		e.Markets = markets
		if len(markets) > 0 {
			out = append(out, e)
		}
	}
	return out
}

func DecodeStringArray(raw string) ([]string, error) {
	var values []string
	if err := jsonUnmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("decode JSON string array: %w", err)
	}
	return values, nil
}
