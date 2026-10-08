package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"weatherbot/internal/domain"
	"weatherbot/internal/rules"
)

type observation struct {
	At       time.Time
	Bid      float64
	Phase    string
	Strategy string
	Done     uint8
	CityDay  string
	Cost     float64
}
type metric struct {
	Samples             int             `json:"samples"`
	MeanBidMove         float64         `json:"mean_bid_move"`
	PositiveRate        float64         `json:"positive_rate"`
	Sum                 float64         `json:"-"`
	Positive            int             `json:"-"`
	MeanNetExitEdge     float64         `json:"mean_net_exit_edge"`
	NetPositiveRate     float64         `json:"net_positive_rate"`
	IndependentCityDays int             `json:"independent_city_days"`
	NetSum              float64         `json:"-"`
	NetPositive         int             `json:"-"`
	CityDays            map[string]bool `json:"-"`
}
type report struct {
	GeneratedAt time.Time                     `json:"generated_at"`
	Source      string                        `json:"source"`
	Metrics     map[string]map[string]*metric `json:"metrics_by_phase_and_horizon"`
	Limitations []string                      `json:"limitations"`
}

func main() {
	path := filepath.Join("data", "snapshots.jsonl")
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	horizons := []time.Duration{15 * time.Minute, 30 * time.Minute, 60 * time.Minute}
	pending := map[string][]observation{}
	metrics := map[string]map[string]*metric{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 32<<20)
	for scanner.Scan() {
		var snap domain.Snapshot
		if json.Unmarshal(scanner.Bytes(), &snap) != nil {
			continue
		}
		for _, r := range snap.Reports {
			if r.Distribution == nil {
				continue
			}
			for _, s := range r.Signals {
				if s.MarketID == "" || s.BestBid <= 0 {
					continue
				}
				queue := pending[s.MarketID]
				keep := queue[:0]
				for _, old := range queue {
					age := snap.GeneratedAt.Sub(old.At)
					for index, h := range horizons {
						if old.Done&(1<<index) == 0 && age >= h && age < h+10*time.Minute {
							add(metrics, old.Phase+"/"+old.Strategy, h.String(), s.BestBid-old.Bid, s.BestBid-old.Cost-.01, old.CityDay)
							old.Done |= 1 << index
						}
					}
					if old.Done != 7 && age < 70*time.Minute {
						keep = append(keep, old)
					}
				}
				pending[s.MarketID] = keep
				strategy := "ALL_TOP_BUCKETS"
				if earlyAligned(*r.Distribution, r.Signals, s.MarketID) {
					strategy = "SOURCE_ALIGNED_EARLY"
				}
				if isTop(s, r.Signals) {
					pending[s.MarketID] = append(pending[s.MarketID], observation{At: snap.GeneratedAt, Bid: s.BestBid, Cost: s.BestAsk + s.FeePerShare + .01, Phase: stage(*r.Distribution), Strategy: strategy, CityDay: r.Rule.StationICAO + "/" + r.Rule.LocalDate})
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	for _, byHorizon := range metrics {
		for _, m := range byHorizon {
			if m.Samples > 0 {
				m.MeanBidMove = m.Sum / float64(m.Samples)
				m.PositiveRate = float64(m.Positive) / float64(m.Samples)
				m.MeanNetExitEdge = m.NetSum / float64(m.Samples)
				m.NetPositiveRate = float64(m.NetPositive) / float64(m.Samples)
				m.IndependentCityDays = len(m.CityDays)
			}
		}
	}
	out := report{GeneratedAt: time.Now().UTC(), Source: path, Metrics: metrics, Limitations: []string{
		"Price-path replay uses locally archived snapshots and executable displayed Bid; it is not a fill simulation.",
		"Historical snapshots predate the new strategy label, so SOURCE_ALIGNED_EARLY is reconstructed from the stored three-source peaks and top-bucket rankings.",
		"No conclusion is promoted to live entry until each phase has enough independent city-days and out-of-sample positive net performance.",
		"Observations overlap; sample count is not independent trades. Net exit edge subtracts entry Ask, stored entry fee, and 1c per side slippage but does not model all exit fees or order-book depth.",
	}}
	b, _ := json.MarshalIndent(out, "", "  ")
	output := filepath.Join("data", "backtest", "stage-price-path.json")
	if err := os.WriteFile(output, b, 0644); err != nil {
		panic(err)
	}
	keys := make([]string, 0, len(metrics))
	for k := range metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("wrote %s (%d stage/strategy groups)\n", output, len(keys))
}

func add(all map[string]map[string]*metric, group, horizon string, move, net float64, cityDay string) {
	if all[group] == nil {
		all[group] = map[string]*metric{}
	}
	if all[group][horizon] == nil {
		all[group][horizon] = &metric{CityDays: map[string]bool{}}
	}
	m := all[group][horizon]
	m.Samples++
	m.Sum += move
	m.NetSum += net
	m.CityDays[cityDay] = true
	if net > 0 {
		m.NetPositive++
	}
	if move > 0 {
		m.Positive++
	}
}

func stage(d domain.Distribution) string {
	if d.Phase == "PRE_DAY" {
		return "T_MINUS_24"
	}
	if d.Phase == "OVERNIGHT" {
		return "OVERNIGHT"
	}
	if d.Phase == "MORNING" {
		return "MORNING_06_09"
	}
	if d.Phase == "LATE_DAY" || d.Phase == "AWAITING_SETTLEMENT" {
		return "POST_PEAK_STATE"
	}
	if d.LocalMinute < 360 {
		return "OVERNIGHT"
	}
	if d.LocalMinute < 540 {
		return "MORNING_06_09"
	}
	if d.LocalMinute < 720 {
		return "WARMING_09_12"
	}
	return "PRE_PEAK_CONVERGENCE"
}
func isTop(s domain.Signal, signals []domain.Signal) bool {
	for _, other := range signals {
		if other.WeatherProbability > s.WeatherProbability || other.MarketConsensus > s.MarketConsensus {
			return false
		}
	}
	return true
}
func earlyAligned(d domain.Distribution, signals []domain.Signal, id string) bool {
	if !(d.Phase == "PRE_DAY" || d.Phase == "OVERNIGHT" || d.Phase == "MORNING" || (d.Phase == "INTRADAY" && d.LocalMinute > 0 && d.LocalMinute < 600)) || len(d.SourcePeaksC) < 3 {
		return false
	}
	var target *domain.Signal
	for i := range signals {
		if signals[i].MarketID == id {
			target = &signals[i]
			break
		}
	}
	if target == nil || !isTop(*target, signals) {
		return false
	}
	b, err := rules.ParseBucket(target.Bucket)
	if err != nil {
		return false
	}
	isF := strings.Contains(strings.ToUpper(target.Bucket), "F")
	for _, c := range d.SourcePeaksC {
		v := c
		if isF {
			v = c*9/5 + 32
		}
		if b.Low != nil && v < *b.Low-.55 {
			return false
		}
		if b.High != nil && v > *b.High+.55 {
			return false
		}
	}
	return !math.IsNaN(target.BestBid)
}
