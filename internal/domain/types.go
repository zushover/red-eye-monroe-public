package domain

import "time"

type Config struct {
	MinimumOrderDollars         float64 `json:"minimum_order_dollars"`
	MaximumExposureDollars      float64 `json:"maximum_exposure_dollars"`
	DailyLossLimitDollars       float64 `json:"daily_loss_limit_dollars"`
	DailySubmissionLimitDollars float64 `json:"daily_submission_limit_dollars"`
	MaximumPaperPositions       int     `json:"maximum_paper_positions"`
	PaperIntradayTradeEdge      float64 `json:"paper_intraday_trade_edge"`
	PaperPredayTradeEdge        float64 `json:"paper_preday_trade_edge"`
	PaperRotationMinProfit      float64 `json:"paper_rotation_min_profit"`
	PaperRotationMinUpgrade     float64 `json:"paper_rotation_min_upgrade"`
	PaperEntriesPaused          bool    `json:"paper_entries_paused"`
	Mode                        string  `json:"mode"`
	PollIntervalSeconds         int     `json:"poll_interval_seconds"`
	HTTPTimeoutSeconds          int     `json:"http_timeout_seconds"`
	LookaheadDays               int     `json:"lookahead_days"`
	MinimumNetEdge              float64 `json:"minimum_net_edge"`
	UncertaintyBuffer           float64 `json:"uncertainty_buffer"`
	SettlementRiskBuffer        float64 `json:"settlement_risk_buffer"`
	ExpectedSlippage            float64 `json:"expected_slippage"`
	DefaultWeatherTakerFeeRate  float64 `json:"default_weather_taker_fee_rate"`
	MaximumSpread               float64 `json:"maximum_spread"`
	MinimumLiquidity            float64 `json:"minimum_liquidity"`
	MaximumOrderDollars         float64 `json:"maximum_order_dollars"`
	BankrollDollars             float64 `json:"bankroll_dollars"`
	MaximumMarketRiskFraction   float64 `json:"maximum_market_risk_fraction"`
	MaximumCityDayRiskFraction  float64 `json:"maximum_city_day_risk_fraction"`
	KellyFraction               float64 `json:"kelly_fraction"`
	ListenAddress               string  `json:"listen_address"`
	DataDirectory               string  `json:"data_directory"`
	StationsFile                string  `json:"stations_file"`
	ResearchStationsFile        string  `json:"research_stations_file"`
}

type Station struct {
	ICAO                   string             `json:"icao"`
	Name                   string             `json:"name"`
	Latitude               float64            `json:"latitude"`
	Longitude              float64            `json:"longitude"`
	Timezone               string             `json:"timezone"`
	ForecastBiasC          float64            `json:"forecast_bias_c"`
	BaseSigmaC             float64            `json:"base_sigma_c"`
	Calibrated             bool               `json:"calibrated"`
	CityCalibrationVersion string             `json:"city_calibration_version,omitempty"`
	CityCalibrationLeadH   int                `json:"city_calibration_lead_hours,omitempty"`
	CityCalibrationSamples int                `json:"city_calibration_samples,omitempty"`
	CityCalibrationHoldout int                `json:"city_calibration_holdout_samples,omitempty"`
	CityCalibrationSigmaC  float64            `json:"city_calibration_sigma_c,omitempty"`
	CityCalibrationModel   string             `json:"city_calibration_model,omitempty"`
	CityCalibrationValid   bool               `json:"city_calibration_validated,omitempty"`
	CityHoldoutMAEC        float64            `json:"city_holdout_mae_c,omitempty"`
	CityBaselineMAEC       float64            `json:"city_baseline_mae_c,omitempty"`
	CityHoldoutLogLoss     float64            `json:"city_holdout_log_loss,omitempty"`
	CityBaselineLogLoss    float64            `json:"city_baseline_log_loss,omitempty"`
	CitySourceBiasC        map[string]float64 `json:"city_source_bias_c,omitempty"`
	CitySourceWeight       map[string]float64 `json:"city_source_weight,omitempty"`
}

type Event struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	ResolutionSource string   `json:"resolutionSource"`
	EndDate          string   `json:"endDate"`
	Active           bool     `json:"active"`
	Closed           bool     `json:"closed"`
	Markets          []Market `json:"markets"`
}

type FeeSchedule struct {
	Rate      float64 `json:"rate"`
	TakerOnly bool    `json:"takerOnly"`
}

type Market struct {
	ResolutionStatus      string       `json:"umaResolutionStatus"`
	ID                    string       `json:"id"`
	Question              string       `json:"question"`
	ConditionID           string       `json:"conditionId"`
	Slug                  string       `json:"slug"`
	ResolutionSource      string       `json:"resolutionSource"`
	Description           string       `json:"description"`
	GroupItemTitle        string       `json:"groupItemTitle"`
	OutcomesJSON          string       `json:"outcomes"`
	OutcomePricesJSON     string       `json:"outcomePrices"`
	ClobTokenIDsJSON      string       `json:"clobTokenIds"`
	BestBid               float64      `json:"bestBid"`
	BestAsk               float64      `json:"bestAsk"`
	Spread                float64      `json:"spread"`
	LiquidityNum          float64      `json:"liquidityNum"`
	Active                bool         `json:"active"`
	Closed                bool         `json:"closed"`
	AcceptingOrders       bool         `json:"acceptingOrders"`
	FeesEnabled           bool         `json:"feesEnabled"`
	FeeSchedule           *FeeSchedule `json:"feeSchedule"`
	OrderPriceMinTickSize float64      `json:"orderPriceMinTickSize"`
}

type Bucket struct {
	Label string   `json:"label"`
	Low   *float64 `json:"low,omitempty"`
	High  *float64 `json:"high,omitempty"`
}

type Rule struct {
	Safe             bool     `json:"safe"`
	Reasons          []string `json:"reasons,omitempty"`
	StationICAO      string   `json:"station_icao"`
	Unit             string   `json:"unit"`
	Source           string   `json:"source"`
	LocalDate        string   `json:"local_date"`
	WholeDegree      bool     `json:"whole_degree"`
	RevisionExpected bool     `json:"revision_expected"`
}

type Observation struct {
	Station    string    `json:"station"`
	ObservedAt time.Time `json:"observed_at"`
	ReceivedAt time.Time `json:"received_at"`
	TempC      float64   `json:"temp_c"`
	DewpointC  float64   `json:"dewpoint_c"`
	WindDir    int       `json:"wind_dir"`
	WindSpeed  int       `json:"wind_speed_kt"`
	Raw        string    `json:"raw"`
}

type Forecast struct {
	ReceivedAt time.Time   `json:"received_at"`
	Timezone   string      `json:"timezone"`
	Hourly     []ForecastH `json:"hourly"`
}

type ForecastH struct {
	LocalTime       time.Time `json:"local_time"`
	TempC           float64   `json:"temp_c"`
	CloudPct        float64   `json:"cloud_pct"`
	CloudAvailable  bool      `json:"cloud_available"`
	PrecipProbPct   float64   `json:"precip_probability_pct"`
	PrecipAvailable bool      `json:"precip_available"`
	WindSpeedKPH    float64   `json:"wind_speed_kph"`
	WindAvailable   bool      `json:"wind_available"`
}

// CurvePoint is the deliberately small time-series payload retained for the
// dashboard. It lets the UI draw every live market without persisting the much
// larger weather response on every collector cycle.
type CurvePoint struct {
	At time.Time `json:"at"`
	C  float64   `json:"c"`
}

type Distribution struct {
	LocalTime               string             `json:"local_time"`
	LocalMinute             int                `json:"local_minute"`
	PeakWindowEndLocal      string             `json:"peak_window_end_local"`
	EntryDeadlineMinute     int                `json:"entry_deadline_local_minute"`
	EntryCautionMinute      int                `json:"entry_caution_local_minute"`
	ConvergenceWindowSource string             `json:"convergence_window_source,omitempty"`
	HistoricalPriceSamples  int                `json:"historical_price_samples"`
	ForecastResidualC       float64            `json:"forecast_residual_c"`
	WarmingRateCPerHour     float64            `json:"warming_rate_c_per_hour"`
	UnstableWeather         bool               `json:"unstable_weather"`
	ModelVersion            string             `json:"model_version"`
	Phase                   string             `json:"phase"`
	HasObservation          bool               `json:"has_observation"`
	ObservationAgeMinutes   float64            `json:"observation_age_minutes"`
	MeanC                   float64            `json:"mean_c"`
	SigmaC                  float64            `json:"sigma_c"`
	ObservedMaxC            float64            `json:"observed_max_c"`
	LatestTempC             float64            `json:"latest_temp_c"`
	ForecastPeakC           float64            `json:"forecast_peak_c"`
	RemainingHours          float64            `json:"remaining_heating_hours"`
	Confidence              float64            `json:"confidence"`
	DataQuality             float64            `json:"data_quality"`
	ProbabilityConfidence   float64            `json:"probability_confidence"`
	TrajectoryFit           float64            `json:"trajectory_fit"`
	TrajectoryMAEC          float64            `json:"trajectory_mae_c"`
	TrajectoryBiasC         float64            `json:"trajectory_bias_c"`
	TrajectoryDirectionFit  float64            `json:"trajectory_direction_fit"`
	TrajectoryPoints        int                `json:"trajectory_points"`
	IndependentModelReady   bool               `json:"independent_model_ready"`
	IndependentModelActive  bool               `json:"independent_model_active"`
	CalibrationModel        string             `json:"calibration_model,omitempty"`
	CalibrationReady        bool               `json:"calibration_ready"`
	ProbabilityStatus       string             `json:"probability_status"`
	CalibrationVersion      string             `json:"calibration_version,omitempty"`
	CalibrationSamples      int                `json:"calibration_samples,omitempty"`
	CalibrationHoldout      int                `json:"calibration_holdout_samples,omitempty"`
	SourcePeaksC            map[string]float64 `json:"source_peaks_c,omitempty"`
	EnsembleSpreadC         float64            `json:"ensemble_spread_c"`
	EnsembleAgreement       float64            `json:"ensemble_agreement"`
	EnsembleSourceCount     int                `json:"ensemble_source_count"`
	EnsembleMemberCount     int                `json:"ensemble_member_count"`
	IntegerProbByUnit       map[int]float64    `json:"integer_probability_by_unit"`
}

type Signal struct {
	MinimumShares      float64   `json:"minimum_shares"`
	BookVerified       bool      `json:"book_verified"`
	EventID            string    `json:"event_id"`
	EventTitle         string    `json:"event_title"`
	MarketID           string    `json:"market_id"`
	MarketQuestion     string    `json:"market_question"`
	Bucket             string    `json:"bucket"`
	TokenID            string    `json:"yes_token_id,omitempty"`
	WeatherProbability float64   `json:"weather_probability"`
	MarketConsensus    float64   `json:"market_consensus_probability"`
	WeatherWeight      float64   `json:"weather_weight"`
	ModelProbability   float64   `json:"model_probability"`
	ConsensusGap       float64   `json:"consensus_gap"`
	PreviousModelProb  float64   `json:"previous_model_probability"`
	ModelProbChange    float64   `json:"model_probability_change"`
	BestBid            float64   `json:"best_bid"`
	BestAsk            float64   `json:"best_ask"`
	GrossEdge          float64   `json:"gross_edge"`
	FeePerShare        float64   `json:"fee_per_share"`
	NetEdge            float64   `json:"net_edge"`
	SuggestedDollars   float64   `json:"suggested_dollars"`
	MarketConvergence  float64   `json:"market_convergence"`
	ConvergenceScore   float64   `json:"convergence_trade_score"`
	ConservativeValue  float64   `json:"conservative_value"`
	PotentialMove      float64   `json:"potential_move"`
	ExpectedExitBid    float64   `json:"expected_exit_bid"`
	ExpectedTradeEdge  float64   `json:"expected_trade_edge"`
	ConvergenceCapture float64   `json:"convergence_capture"`
	PreviousBestBid    float64   `json:"previous_best_bid"`
	PreviousBestAsk    float64   `json:"previous_best_ask"`
	BidChange          float64   `json:"bid_change"`
	AskChange          float64   `json:"ask_change"`
	QuoteChangeMins    float64   `json:"quote_change_minutes"`
	NewEntryAllowed    bool      `json:"new_entry_allowed"`
	EntryWindow        string    `json:"entry_window"`
	Strategy           string    `json:"strategy,omitempty"`
	Action             string    `json:"action"`
	Reason             string    `json:"reason"`
	Reasons            []string  `json:"reasons,omitempty"`
	GeneratedAt        time.Time `json:"generated_at"`
}

type EventReport struct {
	Event           Event                   `json:"event"`
	Rule            Rule                    `json:"rule"`
	Station         *Station                `json:"station,omitempty"`
	Observations    []Observation           `json:"observations,omitempty"`
	Forecast        *Forecast               `json:"forecast,omitempty"`
	SourceForecasts map[string]Forecast     `json:"-"`
	Curves          map[string][]CurvePoint `json:"curves,omitempty"`
	Distribution    *Distribution           `json:"distribution,omitempty"`
	Signals         []Signal                `json:"signals,omitempty"`
	Status          string                  `json:"status"`
	Error           string                  `json:"error,omitempty"`
}

type Snapshot struct {
	ExecutionStatus string        `json:"execution_status"`
	GeneratedAt     time.Time     `json:"generated_at"`
	Mode            string        `json:"mode"`
	GeoBlocked      *bool         `json:"geo_blocked,omitempty"`
	Reports         []EventReport `json:"reports"`
}
