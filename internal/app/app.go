package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"weatherbot/internal/client"
	"weatherbot/internal/domain"
	"weatherbot/internal/execution"
	"weatherbot/internal/model"
	"weatherbot/internal/pricing"
	"weatherbot/internal/rules"
	"weatherbot/internal/store"
)

type App struct {
	cfg       domain.Config
	stations  map[string]domain.Station
	poly      *client.Polymarket
	weather   *client.Weather
	store     *store.Store
	stationMu sync.RWMutex
	quoteMu   sync.Mutex
	quotes    map[string]quoteSample
}

type quoteSample struct {
	Bid         float64
	Ask         float64
	Probability float64
	At          time.Time
}

func New(cfg domain.Config, stations map[string]domain.Station, st *store.Store) *App {
	client.ConfigureWeatherCache(filepath.Join(cfg.DataDirectory, "weather-http-cache.json"))
	h := client.NewHTTP(time.Duration(cfg.HTTPTimeoutSeconds) * time.Second)
	a := &App{cfg: cfg, stations: stations, poly: client.NewPolymarket(h), weather: client.NewWeather(h), store: st, quotes: make(map[string]quoteSample)}
	a.loadCityWeatherCalibration()
	return a
}

func (a *App) loadCityWeatherCalibration() {
	var payload struct {
		Version   string `json:"version"`
		LeadHours int    `json:"lead_hours"`
		Stations  map[string]struct {
			Samples      int                `json:"samples"`
			Holdout      int                `json:"holdout_samples"`
			SigmaC       float64            `json:"sigma_c"`
			Model        string             `json:"model"`
			Validated    bool               `json:"validated"`
			HoldoutMAE   float64            `json:"holdout_mae_c"`
			BaselineMAE  float64            `json:"baseline_mae_c"`
			HoldoutLoss  float64            `json:"holdout_log_loss"`
			BaselineLoss float64            `json:"baseline_log_loss"`
			SourceBiasC  map[string]float64 `json:"source_bias_c"`
			SourceWeight map[string]float64 `json:"source_weight"`
		} `json:"stations"`
	}
	b, err := os.ReadFile(filepath.Join("configs", "city-weather-calibration.json"))
	if err != nil || json.Unmarshal(b, &payload) != nil {
		return
	}
	for icao, calibration := range payload.Stations {
		station := a.stations[icao]
		station.ICAO = icao
		station.CityCalibrationVersion = payload.Version
		station.CityCalibrationLeadH = payload.LeadHours
		station.CityCalibrationSamples = calibration.Samples
		station.CityCalibrationHoldout = calibration.Holdout
		station.CityCalibrationSigmaC = calibration.SigmaC
		station.CityCalibrationModel = calibration.Model
		station.CityCalibrationValid = calibration.Validated
		station.CityHoldoutMAEC = calibration.HoldoutMAE
		station.CityBaselineMAEC = calibration.BaselineMAE
		station.CityHoldoutLogLoss = calibration.HoldoutLoss
		station.CityBaselineLogLoss = calibration.BaselineLoss
		station.CitySourceBiasC = calibration.SourceBiasC
		station.CitySourceWeight = calibration.SourceWeight
		a.stations[icao] = station
	}
}

func (a *App) RunOnce(ctx context.Context) (domain.Snapshot, error) {
	now := time.Now().UTC()
	events, err := a.poly.WeatherEvents(ctx, 200, now.Add(-24*time.Hour), now.Add(time.Duration(a.cfg.LookaheadDays)*24*time.Hour))
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("discover markets: %w", err)
	}
	events = client.FilterTradeable(events, now, a.cfg.LookaheadDays)
	blocked, geoErr := a.poly.GeoBlocked(ctx)
	if geoErr != nil {
		log.Printf("geoblock check unavailable: %v", geoErr)
	}
	snapshot := domain.Snapshot{GeneratedAt: now, Mode: a.cfg.Mode, GeoBlocked: blocked}
	byStation := make(map[string][]domain.Event)
	unsafe := make([]domain.Event, 0)
	for _, event := range events {
		if !strings.Contains(strings.ToLower(event.Title), "highest temperature") {
			continue
		}
		rule := rules.ParseEvent(event)
		if !rule.Safe {
			unsafe = append(unsafe, event)
			continue
		}
		byStation[rule.StationICAO] = append(byStation[rule.StationICAO], event)
	}
	selected := append([]domain.Event(nil), unsafe...)
	for icao, stationEvents := range byStation {
		station, ok := a.cachedStation(icao)
		if !ok || station.Timezone == "" {
			selected = append(selected, stationEvents...)
			continue
		}
		zone, zoneErr := time.LoadLocation(station.Timezone)
		if zoneErr != nil {
			selected = append(selected, stationEvents...)
			continue
		}
		if active, found := selectActiveStationEvent(stationEvents, now, zone, a.entryDeadlineMinute(icao)); found {
			selected = append(selected, active)
		}
	}
	// Rolling the displayed market must not abandon the previous day's managed
	// position. Keep its event in the execution snapshot until it can be exited.
	managed := map[string]bool{}
	if positions, e := execution.OpenPositions(filepath.Join(a.cfg.DataDirectory, "paper-ledger.json")); e == nil {
		for id := range positions {
			managed[id] = true
		}
	}
	var liveLedger struct {
		Attempts []struct {
			MarketID string `json:"market_id"`
			Side     string `json:"side"`
			OrderID  string `json:"order_id"`
		} `json:"attempts"`
	}
	if b, e := os.ReadFile(filepath.Join(a.cfg.DataDirectory, "live-ledger.json")); e == nil && json.Unmarshal(b, &liveLedger) == nil {
		for _, attempt := range liveLedger.Attempts {
			if attempt.Side == "BUY" && attempt.OrderID != "" {
				managed[attempt.MarketID] = true
			}
		}
	}
	selectedIDs := map[string]bool{}
	for _, event := range selected {
		selectedIDs[event.ID] = true
	}
	for _, event := range events {
		if selectedIDs[event.ID] {
			continue
		}
		for _, market := range event.Markets {
			if managed[market.ID] {
				selected = append(selected, event)
				selectedIDs[event.ID] = true
				break
			}
		}
	}
	reports := make([]domain.EventReport, len(selected))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, event := range selected {
		wg.Add(1)
		go func(i int, event domain.Event) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			reports[i] = a.processEvent(ctx, event, now)
		}(i, event)
	}
	wg.Wait()
	a.preserveWeatherDisplay(reports)
	if err := ctx.Err(); err != nil {
		return domain.Snapshot{}, err
	}
	reports = filterLocalTradingHorizon(reports, now, managed)
	snapshot.Reports = reports
	snapshot.GeneratedAt = time.Now().UTC()
	a.annotateQuoteChanges(&snapshot)
	held, err := execution.OpenPositions(filepath.Join(a.cfg.DataDirectory, "paper-ledger.json"))
	if err != nil {
		return snapshot, err
	}
	if len(held) > 0 {
		for id, position := range held {
			m, marketErr := a.poly.Market(ctx, id)
			if marketErr != nil {
				continue
			}
			if m.Closed && m.ResolutionStatus == "resolved" {
				snapshot.Reports = append(snapshot.Reports, domain.EventReport{Event: domain.Event{ID: position.EventID, Title: "Paper position settlement", Markets: []domain.Market{m}}, Status: "RESOLVED"})
			}
		}
	}
	if a.cfg.Mode == "live" {
		snapshot.ExecutionStatus = "live gateway armed separately; only calibrated, verified intents may leave the outbox"
	} else {
		snapshot.ExecutionStatus = "paper simulation; no real orders"
	}
	for i := range snapshot.Reports {
		r := &snapshot.Reports[i]
		for j := range r.Signals {
			s := &r.Signals[j]
			position, isHeld := held[s.MarketID]
			if s.Action != "BUY_PAPER" && s.Action != "BUY_LIVE" && !isHeld {
				continue
			}
			b, err := a.poly.Book(ctx, s.TokenID)
			if err != nil {
				s.Action = "SKIP"
				s.Reason = "order book unavailable"
				s.Reasons = append(s.Reasons, s.Reason)
				continue
			}
			bid, _, _, minimum, err := b.Quote(time.Now().UTC())
			if err != nil {
				s.Action = "SKIP"
				s.Reason = err.Error()
				s.Reasons = append(s.Reasons, s.Reason)
				continue
			}
			s.MinimumShares = minimum
			if isHeld {
				sellDepth := 0.0
				for _, level := range b.Bids {
					price, _ := strconv.ParseFloat(level.Price, 64)
					size, _ := strconv.ParseFloat(level.Size, 64)
					if price == bid {
						sellDepth += size
					}
				}
				if sellDepth >= position.Shares {
					s.BestBid = bid
					s.BookVerified = true
				}
				continue
			}
			bid, bidDepth, executableAsk, _, minimum, err := b.ExecutableBuyQuote(time.Now().UTC(), s.SuggestedDollars)
			if err != nil {
				s.Action = "SKIP"
				s.Reason = err.Error()
				s.Reasons = append(s.Reasons, s.Reason)
				continue
			}
			s.MinimumShares = minimum
			pricing.RepriceCandidate(a.cfg, *r.Distribution, s, bid, executableAsk)
			estimatedShares := math.Floor(s.SuggestedDollars/(s.BestAsk+s.FeePerShare+a.cfg.ExpectedSlippage)*100) / 100
			if s.Action == "SKIP" || executableAsk-bid > pricing.AllowedSpread(a.cfg, executableAsk) || bidDepth < 1.5*estimatedShares {
				s.Action = "SKIP"
				if s.Reason == "" || !strings.Contains(s.Reason, "repricing") {
					s.Reason = "fresh executable buy or 1.5x exit depth insufficient"
				}
				s.Reasons = append(s.Reasons, s.Reason)
				continue
			}
			s.BookVerified = true
		}
	}
	if a.cfg.Mode == "paper" {
		if err := execution.Process(filepath.Join(a.cfg.DataDirectory, "paper-ledger.json"), a.cfg, &snapshot); err != nil {
			return snapshot, fmt.Errorf("paper ledger: %w", err)
		}
	} else {
		count, liveErr := execution.WriteLiveIntents(a.cfg.DataDirectory, snapshot)
		if liveErr != nil {
			return snapshot, fmt.Errorf("live outbox: %w", liveErr)
		}
		if count > 0 {
			snapshot.ExecutionStatus = fmt.Sprintf("%d calibrated live intent(s) published; gateway performs independent risk checks", count)
		}
		// Keep the shadow portfolio managed while live execution is active. Live
		// intents are emitted first, so paper bookkeeping can never suppress a
		// real candidate. The shadow ledger remains isolated from wallet state.
		if err := execution.Process(filepath.Join(a.cfg.DataDirectory, "paper-ledger.json"), a.cfg, &snapshot); err != nil {
			return snapshot, fmt.Errorf("shadow paper ledger: %w", err)
		}
	}
	sort.Slice(snapshot.Reports, func(i, j int) bool { return snapshot.Reports[i].Event.Title < snapshot.Reports[j].Event.Title })
	if err := a.store.Save(snapshot); err != nil {
		return snapshot, fmt.Errorf("persist snapshot: %w", err)
	}
	return snapshot, nil
}

// filterLocalTradingHorizon keeps strategy and UI data on each station's local
// today/tomorrow markets. A previously managed market is retained outside that
// horizon so an open position can still be monitored and exited safely.
func filterLocalTradingHorizon(reports []domain.EventReport, now time.Time, managed map[string]bool) []domain.EventReport {
	out := make([]domain.EventReport, 0, len(reports))
	for _, report := range reports {
		isManaged := false
		for _, market := range report.Event.Markets {
			if managed[market.ID] {
				isManaged = true
				break
			}
		}
		if isManaged || report.Rule.LocalDate == "" {
			out = append(out, report)
			continue
		}
		zone := time.UTC
		if report.Station != nil && report.Station.Timezone != "" {
			if loaded, err := time.LoadLocation(report.Station.Timezone); err == nil {
				zone = loaded
			}
		}
		localNow := now.In(zone)
		today := localNow.Format("2006-01-02")
		tomorrow := localNow.AddDate(0, 0, 1).Format("2006-01-02")
		if report.Rule.LocalDate == today || report.Rule.LocalDate == tomorrow {
			out = append(out, report)
		}
	}
	return out
}

// selectActiveStationEvent keeps each station on its local current-day market
// until that market is at least 95% converged or its historically useful entry
// window has closed. It then rolls to the station's local next-day market, but
// only when Polymarket has actually published it.
func selectActiveStationEvent(events []domain.Event, now time.Time, zone *time.Location, entryDeadlineMinute int) (domain.Event, bool) {
	localToday := now.In(zone).Format("2006-01-02")
	localTomorrow := now.In(zone).AddDate(0, 0, 1).Format("2006-01-02")
	var today, tomorrow *domain.Event
	for i := range events {
		rule := rules.ParseEvent(events[i])
		switch rule.LocalDate {
		case localToday:
			today = &events[i]
		case localTomorrow:
			tomorrow = &events[i]
		}
	}
	localMinute := now.In(zone).Hour()*60 + now.In(zone).Minute()
	windowOpen := entryDeadlineMinute <= 0 || localMinute < entryDeadlineMinute
	if today != nil && pricing.MarketConvergence(today.Markets) < .95 && windowOpen {
		return *today, true
	}
	if tomorrow != nil {
		return *tomorrow, true
	}
	if today != nil {
		return *today, true
	}
	return domain.Event{}, false
}

func (a *App) entryDeadlineMinute(icao string) int {
	type metric struct{ N, P25, Median, P75 int }
	type stationWindow struct {
		Samples int            `json:"samples"`
		Median  map[string]int `json:"median_crossing_relative_local_minute"`
	}
	var payload struct {
		Pooled   map[string]metric        `json:"pooled_crossing_relative_local_minute"`
		Stations map[string]stationWindow `json:"stations"`
	}
	b, err := os.ReadFile(filepath.Join(a.cfg.DataDirectory, "backtest", "convergence-windows.json"))
	if err == nil && json.Unmarshal(b, &payload) == nil {
		if station, ok := payload.Stations[icao]; ok && station.Samples >= 20 && station.Median["0.65"] > 0 {
			return station.Median["0.65"]
		}
		if pooled, ok := payload.Pooled["0.65"]; ok && pooled.N >= 50 && pooled.Median > 0 {
			return pooled.Median
		}
	}
	return 14*60 + 30
}

// annotateQuoteChanges supplies a short-horizon price-path layer.  It does not
// turn momentum into fair value; it shows whether a theoretically attractive
// bucket is actually beginning to converge and gives exits a stateful trail.
func (a *App) annotateQuoteChanges(snapshot *domain.Snapshot) {
	a.quoteMu.Lock()
	defer a.quoteMu.Unlock()
	now := snapshot.GeneratedAt
	for i := range snapshot.Reports {
		for j := range snapshot.Reports[i].Signals {
			s := &snapshot.Reports[i].Signals[j]
			key := s.TokenID
			if key == "" {
				key = s.MarketID
			}
			if previous, ok := a.quotes[key]; ok {
				age := now.Sub(previous.At).Minutes()
				if age > 0 && age <= 15 {
					s.PreviousBestBid, s.PreviousBestAsk = previous.Bid, previous.Ask
					s.BidChange, s.AskChange, s.QuoteChangeMins = s.BestBid-previous.Bid, s.BestAsk-previous.Ask, age
					s.PreviousModelProb, s.ModelProbChange = previous.Probability, s.ModelProbability-previous.Probability
				}
			}
			if s.BestBid > 0 || s.BestAsk > 0 {
				a.quotes[key] = quoteSample{Bid: s.BestBid, Ask: s.BestAsk, Probability: s.ModelProbability, At: now}
			}
		}
	}
}

func (a *App) processEvent(ctx context.Context, event domain.Event, now time.Time) domain.EventReport {
	summary := event
	report := domain.EventReport{Event: summary, Rule: rules.ParseEvent(event), Status: "SKIPPED"}
	if !strings.Contains(strings.ToLower(event.Title), "highest temperature") {
		report.Error = "unsupported weather market type"
		return report
	}
	if !report.Rule.Safe {
		report.Error = "unsafe rule: " + strings.Join(report.Rule.Reasons, ", ")
		return report
	}
	station, ok := a.cachedStation(report.Rule.StationICAO)
	// Calibration-only entries intentionally carry no geography. Discover the
	// actual station before weather calls, while retaining the learned fields.
	if !ok || station.Timezone == "" || station.BaseSigmaC <= 0 {
		calibrated := station
		discovered, discoverErr := a.weather.Station(ctx, report.Rule.StationICAO)
		if discoverErr != nil {
			report.Error = "station metadata: " + discoverErr.Error()
			return report
		}
		station = discovered
		if calibrated.CityCalibrationVersion != "" {
			station.CityCalibrationVersion = calibrated.CityCalibrationVersion
			station.CityCalibrationLeadH = calibrated.CityCalibrationLeadH
			station.CityCalibrationSamples = calibrated.CityCalibrationSamples
			station.CityCalibrationHoldout = calibrated.CityCalibrationHoldout
			station.CityCalibrationSigmaC = calibrated.CityCalibrationSigmaC
			station.CityCalibrationModel = calibrated.CityCalibrationModel
			station.CityCalibrationValid = calibrated.CityCalibrationValid
			station.CityHoldoutMAEC = calibrated.CityHoldoutMAEC
			station.CityBaselineMAEC = calibrated.CityBaselineMAEC
			station.CityHoldoutLogLoss = calibrated.CityHoldoutLogLoss
			station.CityBaselineLogLoss = calibrated.CityBaselineLogLoss
			station.CitySourceBiasC = calibrated.CitySourceBiasC
			station.CitySourceWeight = calibrated.CitySourceWeight
		}
		a.stationMu.Lock()
		a.stations[station.ICAO] = station
		a.stationMu.Unlock()
	}
	report.Station = &station
	obs, err := a.weather.Observations(ctx, station.ICAO, 36)
	if err != nil {
		report.Status, report.Error = "ERROR", "observations: "+err.Error()
		return report
	}
	report.Observations = obs
	forecast, err := a.weather.Forecast(ctx, station, a.cfg.LookaheadDays)
	if err != nil {
		report.Status, report.Error = "ERROR", "forecast: "+err.Error()
		return report
	}
	station.Timezone = forecast.Timezone
	report.Station = &station
	report.Forecast = &forecast
	sourceForecasts := map[string]domain.Forecast{}
	for _, source := range []struct{ label, id string }{{"ECMWF IFS 0.25°", "ecmwf_ifs025"}, {"NCEP GFS seamless", "gfs_seamless"}} {
		f, sourceErr := a.weather.ForecastModel(ctx, station, a.cfg.LookaheadDays, source.id)
		if sourceErr != nil {
			continue
		}
		sourceForecasts[source.label] = f
	}
	if len(sourceForecasts) != 2 {
		report.Status, report.Error = "WAITING", "two explicit weather models are required"
		return report
	}
	report.SourceForecasts = sourceForecasts
	memberPeaks, memberErr := a.weather.EnsembleDailyMax(ctx, station, report.Rule.LocalDate, "ecmwf_ifs025", a.cfg.LookaheadDays)
	if memberErr != nil {
		// The deterministic models and same-station METAR still provide a valid,
		// auditable degraded distribution. A third-party ensemble quota must not
		// blind monitoring or exit management for an existing position.
		memberPeaks = nil
	}
	dist, err := model.BuildProbabilistic(report.Rule, station, obs, forecast, sourceForecasts, memberPeaks, time.Now().UTC())
	if err != nil {
		if strings.Contains(err.Error(), "no observations") || strings.Contains(err.Error(), "no forecast") {
			report.Status, report.Error = "WAITING", err.Error()
		} else {
			report.Status, report.Error = "ERROR", "model: "+err.Error()
		}
		return report
	}
	a.applyHistoricalConvergenceWindow(&dist, station.ICAO)
	report.Distribution = &dist
	report.Signals = pricing.PriceEvent(a.cfg, event, dist)
	report.Status = "READY"
	return report
}

func (a *App) applyHistoricalConvergenceWindow(dist *domain.Distribution, icao string) {
	type metric struct{ N, P25, Median, P75 int }
	type stationWindow struct {
		Samples int            `json:"samples"`
		Median  map[string]int `json:"median_crossing_relative_local_minute"`
	}
	var payload struct {
		Pooled   map[string]metric        `json:"pooled_crossing_relative_local_minute"`
		Stations map[string]stationWindow `json:"stations"`
	}
	b, err := os.ReadFile(filepath.Join(a.cfg.DataDirectory, "backtest", "convergence-windows.json"))
	if err != nil || json.Unmarshal(b, &payload) != nil {
		return
	}
	if station, ok := payload.Stations[icao]; ok && station.Samples >= 20 && station.Median["0.65"] > 0 {
		dist.EntryDeadlineMinute, dist.HistoricalPriceSamples = station.Median["0.65"], station.Samples
		dist.EntryCautionMinute = 13*60 + 5
		if dist.EntryCautionMinute >= dist.EntryDeadlineMinute {
			dist.EntryCautionMinute = max(0, dist.EntryDeadlineMinute-30)
		}
		dist.ConvergenceWindowSource = "station median sustained 65% crossing"
		return
	}
	if pooled, ok := payload.Pooled["0.65"]; ok && pooled.N >= 50 && pooled.P25 > 0 && pooled.Median > 0 {
		dist.EntryCautionMinute, dist.EntryDeadlineMinute, dist.HistoricalPriceSamples = pooled.P25, pooled.Median, pooled.N
		dist.ConvergenceWindowSource = "pooled 25th-percentile caution / median hard stop at sustained 65% crossing"
	}
}

func (a *App) cachedStation(icao string) (domain.Station, bool) {
	a.stationMu.RLock()
	defer a.stationMu.RUnlock()
	s, ok := a.stations[icao]
	return s, ok
}

func (a *App) Run(ctx context.Context) error {
	if _, err := a.RunOnce(ctx); err != nil {
		log.Printf("initial cycle failed: %v", err)
	}
	ticker := time.NewTicker(time.Duration(a.cfg.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			snapshot, err := a.RunOnce(ctx)
			if err != nil {
				log.Printf("cycle failed: %v", err)
				continue
			}
			log.Printf("cycle complete: %d weather events", len(snapshot.Reports))
		}
	}
}
