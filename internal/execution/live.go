package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"weatherbot/internal/domain"
)

// LiveIntent is a deliberately narrow handoff to the separately authenticated
// execution gateway. It contains public market data only, never wallet secrets.
type LiveIntent struct {
	Version           int       `json:"version"`
	Phase             string    `json:"phase"`
	IntentID          string    `json:"intent_id"`
	AssetID           string    `json:"asset_id"`
	MarketID          string    `json:"market_id"`
	GroupKey          string    `json:"group_key"`
	Side              string    `json:"side"`
	AmountDollars     float64   `json:"amount_dollars"`
	MaxSpendDollars   float64   `json:"max_spend_dollars"`
	MaxPrice          float64   `json:"max_price"`
	NetEdge           float64   `json:"net_edge"`
	ModelProbability  float64   `json:"model_probability"`
	MarketConsensus   float64   `json:"market_consensus_probability"`
	ExpectedTradeEdge float64   `json:"expected_trade_edge"`
	EntryWindow       string    `json:"entry_window"`
	DataQuality       float64   `json:"data_quality"`
	ModelCalibrated   bool      `json:"model_calibrated"`
	ProbabilityState  string    `json:"probability_status"`
	BookVerified      bool      `json:"book_verified"`
	GeneratedAt       time.Time `json:"generated_at"`
}

func WriteLiveIntents(dataDir string, snapshot domain.Snapshot) (int, error) {
	// All temperature bins for one station/local date share one position slot.
	blocked := map[string]bool{}
	marketGroups, tokenGroups := map[string]string{}, map[string]string{}
	for _, r := range snapshot.Reports {
		for _, s := range r.Signals {
			key := r.Rule.StationICAO + "|" + r.Rule.LocalDate
			if r.Rule.StationICAO == "" || r.Rule.LocalDate == "" {
				key = s.MarketID
			}
			marketGroups[s.MarketID] = key
			tokenGroups[s.TokenID] = key
		}
	}
	var ledger struct {
		Attempts []struct {
			MarketID  string    `json:"market_id"`
			Side      string    `json:"side"`
			Status    string    `json:"status"`
			AssetID   string    `json:"asset_id"`
			GroupKey  string    `json:"group_key"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"attempts"`
	}
	if body, err := os.ReadFile(filepath.Join(dataDir, "live-ledger.json")); err == nil {
		if err := json.Unmarshal([]byte(strings.TrimPrefix(string(body), "\uFEFF")), &ledger); err != nil {
			return 0, fmt.Errorf("read live ledger: %w", err)
		}
		closed := map[string]time.Time{}
		for _, a := range ledger.Attempts {
			if a.Side == "SELL" && strings.EqualFold(a.Status,"CLOSED") {
				closed[a.AssetID] = a.CreatedAt
			}
		}
		for _, a := range ledger.Attempts {
			if a.Side == "BUY" && a.Status != "REJECTED" {
				if closed[a.AssetID].After(a.CreatedAt) && a.AssetID != "" {
					continue
				}
				key := a.GroupKey
				if key == "" {
					key = marketGroups[a.MarketID]
				}
				if key == "" {
					key = a.MarketID
				}
				blocked[key] = true
			}
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	var state struct {
		CheckedAt time.Time `json:"checked_at"`
		Positions []struct {
			AssetID string  `json:"asset_id"`
			Size    float64 `json:"current_size"`
			Status  string  `json:"status"`
		} `json:"positions"`
		Orders []struct {
			AssetID string `json:"asset_id"`
		} `json:"orders"`
	}
	if body, err := os.ReadFile(filepath.Join(dataDir, "live-status.json")); err == nil {
		if err := json.Unmarshal(body, &state); err != nil {
			return 0, err
		}
		if time.Since(state.CheckedAt) > 2*time.Minute {
			return 0, nil
		}
		for _, p := range state.Positions {
			if p.Size > 0 && !strings.EqualFold(p.Status, "REDEEMABLE") {
				blocked[tokenGroups[p.AssetID]] = true
			}
		}
		for _, o := range state.Orders {
			blocked[tokenGroups[o.AssetID]] = true
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	outbox := filepath.Join(dataDir, "live-outbox")
	if err := os.MkdirAll(outbox, 0700); err != nil {
		return 0, err
	}
	written := 0
	for _, report := range snapshot.Reports {
		if report.Distribution == nil || !report.Distribution.CalibrationReady || report.Distribution.ProbabilityStatus != "CALIBRATED" || report.Distribution.DataQuality < .65 {
			continue
		}
		signals := append([]domain.Signal(nil), report.Signals...)
		sort.SliceStable(signals, func(i, j int) bool { return signals[i].NetEdge > signals[j].NetEdge })
		for _, signal := range signals {
			key := marketGroups[signal.MarketID]
			if blocked[key] {
				continue
			}
			if signal.Action != "BUY_LIVE" || !signal.BookVerified || signal.NetEdge <= 0 || signal.MarketConsensus < .10 || signal.SuggestedDollars != 1 {
				continue
			}
			seed := strings.Join([]string{signal.EventID, signal.MarketID, signal.TokenID, signal.GeneratedAt.UTC().Format(time.RFC3339Nano)}, "|")
			sum := sha256.Sum256([]byte(seed))
			id := "weather-" + hex.EncodeToString(sum[:12])
			intent := LiveIntent{
				GroupKey: key,
				Version:  1, Phase: report.Distribution.Phase, IntentID: id, AssetID: signal.TokenID, MarketID: signal.MarketID,
				Side: "BUY", AmountDollars: signal.SuggestedDollars, MaxSpendDollars: 1.10,
				MaxPrice: signal.BestAsk, NetEdge: signal.NetEdge, ModelProbability: signal.ModelProbability,
				MarketConsensus:   signal.MarketConsensus,
				ExpectedTradeEdge: signal.ExpectedTradeEdge, EntryWindow: signal.EntryWindow,
				DataQuality: report.Distribution.DataQuality, ModelCalibrated: report.Distribution.CalibrationReady,
				ProbabilityState: report.Distribution.ProbabilityStatus, BookVerified: true, GeneratedAt: signal.GeneratedAt,
			}
			body, err := json.MarshalIndent(intent, "", "  ")
			if err != nil {
				return written, err
			}
			path := filepath.Join(outbox, id+".json")
			tmp := path + ".tmp"
			if err := os.WriteFile(tmp, body, 0600); err != nil {
				return written, err
			}
			if err := os.Rename(tmp, path); err != nil {
				_ = os.Remove(tmp)
				if os.IsExist(err) {
					continue
				}
				return written, fmt.Errorf("publish live intent: %w", err)
			}
			written++
			blocked[key] = true
		}
	}
	return written, nil
}
