package client

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"time"
)

type Level struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}
type Book struct {
	Token     string  `json:"asset_id"`
	Timestamp string  `json:"timestamp"`
	Bids      []Level `json:"bids"`
	Asks      []Level `json:"asks"`
	Minimum   string  `json:"min_order_size"`
	Tick      string  `json:"tick_size"`
	NegRisk   bool    `json:"neg_risk"`
}

func (p *Polymarket) Book(ctx context.Context, token string) (Book, error) {
	var b Book
	err := p.http.GetJSON(ctx, "https://clob.polymarket.com/book?token_id="+url.QueryEscape(token), &b)
	if err != nil {
		return b, err
	}
	if b.Token != token {
		return b, fmt.Errorf("book token mismatch")
	}
	return b, nil
}
func (b Book) Quote(now time.Time) (bid, ask, depth, minimum float64, err error) {
	ms, e := strconv.ParseInt(b.Timestamp, 10, 64)
	if e != nil || now.Sub(time.UnixMilli(ms)) > 60*time.Second || time.UnixMilli(ms).After(now.Add(5*time.Second)) {
		err = fmt.Errorf("stale book")
		return
	}
	minimum, e = strconv.ParseFloat(b.Minimum, 64)
	if e != nil || minimum <= 0 || math.IsNaN(minimum) || math.IsInf(minimum, 0) {
		err = fmt.Errorf("invalid minimum shares")
		return
	}
	ask = 1
	for _, l := range b.Asks {
		p, e := strconv.ParseFloat(l.Price, 64)
		q, e2 := strconv.ParseFloat(l.Size, 64)
		if e != nil || e2 != nil || p <= 0 || p >= 1 || q <= 0 || math.IsNaN(q) || math.IsInf(q, 0) {
			continue
		}
		if p < ask {
			ask = p
			depth = q
		} else if p == ask {
			depth += q
		}
	}
	for _, l := range b.Bids {
		p, _ := strconv.ParseFloat(l.Price, 64)
		q, _ := strconv.ParseFloat(l.Size, 64)
		if q > 0 && p > bid && p < 1 {
			bid = p
		}
	}
	if bid <= 0 || ask >= 1 || bid >= ask {
		err = fmt.Errorf("one-sided or crossed book")
	}
	return
}

// ExecutableBuyQuote walks the ask side and returns the volume-weighted price
// for spending rawDollars. bidDepth is the shares currently available at the
// best Bid and is used as a conservative exit-liquidity check by paper mode.
func (b Book) ExecutableBuyQuote(now time.Time, rawDollars float64) (bid, bidDepth, averageAsk, boughtShares, minimum float64, err error) {
	if rawDollars <= 0 || math.IsNaN(rawDollars) || math.IsInf(rawDollars, 0) {
		err = fmt.Errorf("invalid paper order dollars")
		return
	}
	ms, e := strconv.ParseInt(b.Timestamp, 10, 64)
	if e != nil || now.Sub(time.UnixMilli(ms)) > 60*time.Second || time.UnixMilli(ms).After(now.Add(5*time.Second)) {
		err = fmt.Errorf("stale book")
		return
	}
	minimum, e = strconv.ParseFloat(b.Minimum, 64)
	if e != nil || minimum <= 0 || math.IsNaN(minimum) || math.IsInf(minimum, 0) {
		err = fmt.Errorf("invalid minimum shares")
		return
	}
	type level struct{ price, size float64 }
	asks := make([]level, 0, len(b.Asks))
	for _, l := range b.Asks {
		p, e1 := strconv.ParseFloat(l.Price, 64)
		q, e2 := strconv.ParseFloat(l.Size, 64)
		if e1 == nil && e2 == nil && p > 0 && p < 1 && q > 0 && !math.IsNaN(q) && !math.IsInf(q, 0) {
			asks = append(asks, level{p, q})
		}
	}
	sort.Slice(asks, func(i, j int) bool { return asks[i].price < asks[j].price })
	if len(asks) == 0 {
		err = fmt.Errorf("one-sided or crossed book")
		return
	}
	bestAsk := asks[0].price
	for _, l := range b.Bids {
		p, e1 := strconv.ParseFloat(l.Price, 64)
		q, e2 := strconv.ParseFloat(l.Size, 64)
		if e1 != nil || e2 != nil || p <= 0 || p >= 1 || q <= 0 {
			continue
		}
		if p > bid {
			bid, bidDepth = p, q
		} else if p == bid {
			bidDepth += q
		}
	}
	if bid <= 0 || bid >= bestAsk {
		err = fmt.Errorf("one-sided or crossed book")
		return
	}
	spent := 0.0
	for _, l := range asks {
		remaining := rawDollars - spent
		if remaining <= 1e-9 {
			break
		}
		take := math.Min(l.size, remaining/l.price)
		boughtShares += take
		spent += take * l.price
	}
	if spent+1e-6 < rawDollars || boughtShares <= 0 {
		err = fmt.Errorf("insufficient ask depth for $%.2f paper fill", rawDollars)
		return
	}
	averageAsk = spent / boughtShares
	if boughtShares+1e-9 < minimum {
		err = fmt.Errorf("minimum exchange shares exceed paper budget")
	}
	return
}
