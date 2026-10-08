package client

import (
	"math"
	"strconv"
	"testing"
	"time"
)

func TestExecutableBuyQuoteWalksAskLevels(t *testing.T) {
	now := time.Now().UTC()
	b := Book{
		Timestamp: strconv.FormatInt(now.UnixMilli(), 10), Minimum: "5",
		Bids: []Level{{Price: "0.08", Size: "40"}},
		Asks: []Level{{Price: "0.12", Size: "10"}, {Price: "0.10", Size: "5"}},
	}
	bid, bidDepth, ask, shares, minimum, err := b.ExecutableBuyQuote(now, 1)
	if err != nil {
		t.Fatal(err)
	}
	// $0.50 buys 5 shares at 10c, then $0.50 buys 4.1667 at 12c.
	if bid != .08 || bidDepth != 40 || minimum != 5 || math.Abs(shares-9.1666667) > 1e-6 || math.Abs(ask-1/shares) > 1e-6 {
		t.Fatalf("unexpected executable quote: bid=%v depth=%v ask=%v shares=%v min=%v", bid, bidDepth, ask, shares, minimum)
	}
}

func TestExecutableBuyQuoteRejectsUnfillableDollar(t *testing.T) {
	now := time.Now().UTC()
	b := Book{Timestamp: strconv.FormatInt(now.UnixMilli(), 10), Minimum: "5", Bids: []Level{{Price: ".08", Size: "20"}}, Asks: []Level{{Price: ".10", Size: "2"}}}
	if _, _, _, _, _, err := b.ExecutableBuyQuote(now, 1); err == nil {
		t.Fatal("thin ask book must not create a hypothetical fill")
	}
}
