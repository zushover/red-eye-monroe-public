package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type pilot struct {
	ICAO       string  `json:"icao"`
	Name       string  `json:"name"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Timezone   string  `json:"timezone"`
	ISDStation string  `json:"isd_station"`
}

type openMeteo struct {
	Hourly struct {
		Time        []string   `json:"time"`
		Temperature []*float64 `json:"temperature_2m_previous_day1"`
	} `json:"hourly"`
}

type daySample struct {
	Date       string             `json:"date"`
	ObservedC  float64            `json:"observed_c"`
	METARCount int                `json:"metar_count"`
	Forecasts  map[string]float64 `json:"forecasts_c"`
}

type sourceFit struct {
	BiasC       float64 `json:"bias_c"`
	MAEC        float64 `json:"train_mae_c"`
	Weight      float64 `json:"weight"`
	Redundancy  int     `json:"redundancy_count"`
	MaxPeerCorr float64 `json:"max_peer_correlation"`
}

type scores struct {
	N       int     `json:"n"`
	MAEC    float64 `json:"mae_c"`
	RMSEC   float64 `json:"rmse_c"`
	LogLoss float64 `json:"rounded_degree_log_loss"`
	Cover68 float64 `json:"coverage_68pct"`
}

type stationResult struct {
	ICAO                   string               `json:"icao"`
	Name                   string               `json:"name"`
	Timezone               string               `json:"timezone"`
	TrainingStart          string               `json:"training_start"`
	TrainingEnd            string               `json:"training_end"`
	TrainN                 int                  `json:"train_n"`
	HoldoutN               int                  `json:"holdout_n"`
	Sources                map[string]sourceFit `json:"sources"`
	SigmaC                 float64              `json:"sigma_c"`
	BaselineSigma          float64              `json:"baseline_sigma_c"`
	Baseline               scores               `json:"baseline_holdout"`
	Calibrated             scores               `json:"city_calibrated_holdout"`
	SelectedModel          string               `json:"selected_model"`
	SelectionN             int                  `json:"selection_holdout_n"`
	Candidates             []candidateScore     `json:"candidate_selection_scores"`
	Accepted               bool                 `json:"accepted"`
	EmpiricalKernelLogLoss float64              `json:"empirical_kernel_holdout_log_loss"`
	SeasonalKernelLogLoss  float64              `json:"seasonal_kernel_holdout_log_loss"`
	Samples                []daySample          `json:"samples,omitempty"`
}

type candidateScore struct {
	Name    string   `json:"name"`
	Sources []string `json:"sources"`
	MAEC    float64  `json:"mae_c"`
	LogLoss float64  `json:"rounded_degree_log_loss"`
}

type modelSpec struct {
	Name          string
	Sources       []string
	BiasCorrected bool
	ErrorWeighted bool
}

type report struct {
	GeneratedAt string              `json:"generated_at"`
	Method      string              `json:"method"`
	ForecastCut string              `json:"forecast_cutoff"`
	Source      string              `json:"observation_source"`
	AutoApply   bool                `json:"auto_apply"`
	Stations    []stationResult     `json:"stations"`
	Failures    []map[string]string `json:"failures,omitempty"`
}

var sourceIDs = []struct{ Name, Model string }{
	{"ECMWF IFS 0.25°", "ecmwf_ifs025"},
	{"NCEP GFS seamless", "gfs_seamless"},
	{"Open-Meteo best match", ""},
}

func main() {
	start, end := "2025-01-01", "2025-08-20"
	if v := flagValue("--start="); v != "" {
		start = v
	}
	if v := flagValue("--end="); v != "" {
		end = v
	}
	pilotPath := filepath.Join("configs", "calibration-pilots.json")
	if v := flagValue("--pilots="); v != "" {
		pilotPath = v
	}
	b, err := os.ReadFile(pilotPath)
	must(err)
	var pilots []pilot
	must(json.Unmarshal(b, &pilots))
	client := httpClient()
	if v := flagValue("--timeout-seconds="); v != "" {
		seconds, err := strconv.Atoi(v)
		must(err)
		if seconds < 1 {
			panic("timeout must be positive")
		}
		client.Timeout = time.Duration(seconds) * time.Second
	}
	out := report{GeneratedAt: time.Now().UTC().Format(time.RFC3339), Method: "nested chronological model selection inside the first 70%; untouched final 30% holdout; candidates include single-source, pair, and three-source bias-corrected equal/error-weighted models", ForecastCut: "previous_day1: values issued 24 hours before each valid hour", Source: "NOAA NCEI Global Hourly; FM-15 rows matching the airport ICAO; daily maximum of quality-screened METAR temperature", AutoApply: false}
	reused := map[string][]daySample{}
	if path := flagValue("--reuse-report="); path != "" {
		var old report
		data, err := os.ReadFile(path)
		must(err)
		must(json.Unmarshal(data, &old))
		for _, station := range old.Stations {
			reused[station.ICAO] = station.Samples
		}
	}
	outputPath := filepath.Join("data", "backtest", "city-weather-calibration.json")
	if v := flagValue("--output="); v != "" {
		outputPath = v
	}
	for _, p := range pilots {
		fmt.Printf("Calibrating %s...\n", p.ICAO)
		var result stationResult
		var err error
		if samples := reused[p.ICAO]; len(samples) > 0 {
			result, err = calibrateSamples(p, samples)
		} else {
			result, err = calibrate(client, p, start, end)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", p.ICAO, err)
			out.Failures = append(out.Failures, map[string]string{"icao": p.ICAO, "reason": err.Error()})
			continue
		}
		out.Stations = append(out.Stations, result)
		must(os.MkdirAll(filepath.Dir(outputPath), 0755))
		checkpoint, checkpointErr := json.MarshalIndent(out, "", "  ")
		must(checkpointErr)
		must(os.WriteFile(outputPath, checkpoint, 0644))
		fmt.Printf("  selected=%s holdout n=%d baseline MAE %.3f -> city MAE %.3f, log loss %.3f -> %.3f, accepted=%v\n", result.SelectedModel, result.HoldoutN, result.Baseline.MAEC, result.Calibrated.MAEC, result.Baseline.LogLoss, result.Calibrated.LogLoss, result.Accepted)
	}
	must(os.MkdirAll(filepath.Join("data", "backtest"), 0755))
	encoded, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile(outputPath, encoded, 0644))
}

func calibrate(client *http.Client, p pilot, start, end string) (stationResult, error) {
	observed, counts, err := metarDailyMax(client, p, start, end)
	if err != nil {
		return stationResult{}, err
	}
	forecasts := map[string]map[string]float64{}
	for _, src := range sourceIDs {
		values, err := forecastDailyMax(client, p, start, end, src.Model)
		if err != nil {
			return stationResult{}, fmt.Errorf("%s: %w", src.Name, err)
		}
		forecasts[src.Name] = values
	}
	dates := make([]string, 0, len(observed))
	for date := range observed {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	all := []daySample{}
	for _, date := range dates {
		if counts[date] < 12 {
			continue
		}
		s := daySample{Date: date, ObservedC: observed[date], METARCount: counts[date], Forecasts: map[string]float64{}}
		complete := true
		for _, src := range sourceIDs {
			v, ok := forecasts[src.Name][date]
			if !ok {
				complete = false
				break
			}
			s.Forecasts[src.Name] = v
		}
		if complete {
			all = append(all, s)
		}
	}
	if len(all) < 60 {
		return stationResult{}, fmt.Errorf("only %d complete daily samples", len(all))
	}
	return calibrateSamples(p, all)
}

func calibrateSamples(p pilot, all []daySample) (stationResult, error) {
	if len(all) < 60 {
		return stationResult{}, fmt.Errorf("only %d complete daily samples", len(all))
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Date < all[j].Date })
	split := int(float64(len(all)) * .7)
	train, test := all[:split], all[split:]
	innerSplit := int(float64(len(train)) * .7)
	fitRows, selectRows := train[:innerSplit], train[innerSplit:]
	specs := candidateSpecs()
	candidates := make([]candidateScore, 0, len(specs))
	selected := specs[0]
	bestObjective := math.Inf(1)
	for _, spec := range specs {
		fit := fitModel(fitRows, spec)
		sigma := robustSigma(modelResiduals(fitRows, fit))
		score := evaluateFit(selectRows, fit, sigma, p.ICAO)
		candidates = append(candidates, candidateScore{Name: spec.Name, Sources: append([]string(nil), spec.Sources...), MAEC: score.MAEC, LogLoss: score.LogLoss})
		// Exact-bin probability quality is primary; MAE breaks close ties.
		objective := score.LogLoss + .05*score.MAEC
		if objective < bestObjective {
			bestObjective, selected = objective, spec
		}
	}
	baselineFit := fitModel(train, specs[0])
	fits := fitModel(train, selected)
	baseSigma := robustSigma(modelResiduals(train, baselineFit))
	sigma := robustSigma(modelResiduals(train, fits))
	baseline := evaluateFit(test, baselineFit, baseSigma, p.ICAO)
	calibrated := evaluateFit(test, fits, sigma, p.ICAO)
	empiricalLoss := evaluateKernelLogLoss(test, train, fits, p.ICAO, false)
	seasonalLoss := evaluateKernelLogLoss(test, train, fits, p.ICAO, true)
	// A selected city model must improve exact-bucket probability and may not
	// materially damage peak MAE. The final holdout never participates in selection.
	accepted := selected.Name != "GENERIC_ECMWF_GFS_EQUAL" && calibrated.MAEC <= baseline.MAEC*1.02 && calibrated.LogLoss <= baseline.LogLoss*.995 && len(test) >= 100
	return stationResult{ICAO: p.ICAO, Name: p.Name, Timezone: p.Timezone, TrainingStart: all[0].Date, TrainingEnd: all[len(all)-1].Date, TrainN: len(train), HoldoutN: len(test), Sources: fits, SigmaC: sigma, BaselineSigma: baseSigma, Baseline: baseline, Calibrated: calibrated, SelectedModel: selected.Name, SelectionN: len(selectRows), Candidates: candidates, Accepted: accepted, EmpiricalKernelLogLoss: empiricalLoss, SeasonalKernelLogLoss: seasonalLoss, Samples: all}, nil
}

func evaluateKernelLogLoss(test, train []daySample, fits map[string]sourceFit, icao string, seasonal bool) float64 {
	type residualSample struct {
		value float64
		month time.Month
	}
	residuals := make([]residualSample, 0, len(train))
	for _, s := range train {
		at, err := time.Parse("2006-01-02", s.Date)
		if err != nil {
			continue
		}
		residuals = append(residuals, residualSample{value: s.ObservedC - fittedPeak(s, fits), month: at.Month()})
	}
	if len(residuals) < 60 {
		return math.Inf(1)
	}
	values := make([]float64, len(residuals))
	for i := range residuals {
		values[i] = residuals[i].value
	}
	bandwidth := math.Max(.20, robustSigma(values)*math.Pow(float64(len(values)), -.20)*1.06)
	loss, n := 0.0, 0
	for _, s := range test {
		at, err := time.Parse("2006-01-02", s.Date)
		if err != nil {
			continue
		}
		lo, hi := verificationBucketBounds(s.ObservedC, icao)
		pred := fittedPeak(s, fits)
		prob, used := 0.0, 0
		for _, residual := range residuals {
			if seasonal {
				delta := int(residual.month) - int(at.Month())
				if delta < 0 {
					delta = -delta
				}
				if delta > 1 && delta < 11 {
					continue
				}
			}
			centre := pred + residual.value
			prob += normalCDF((hi-centre)/bandwidth) - normalCDF((lo-centre)/bandwidth)
			used++
		}
		if used < 30 {
			prob, used = 0, 0
			for _, residual := range residuals {
				centre := pred + residual.value
				prob += normalCDF((hi-centre)/bandwidth) - normalCDF((lo-centre)/bandwidth)
				used++
			}
		}
		loss += -math.Log(math.Max(1e-9, prob/float64(used)))
		n++
	}
	if n == 0 {
		return math.Inf(1)
	}
	return loss / float64(n)
}

func fittedPeak(s daySample, fits map[string]sourceFit) float64 {
	pred := 0.0
	for name, fit := range fits {
		pred += fit.Weight * (s.Forecasts[name] + fit.BiasC)
	}
	return pred
}

func candidateSpecs() []modelSpec {
	e, g, b := sourceIDs[0].Name, sourceIDs[1].Name, sourceIDs[2].Name
	return []modelSpec{
		{Name: "GENERIC_ECMWF_GFS_EQUAL", Sources: []string{e, g}},
		{Name: "ECMWF_ONLY_BIAS", Sources: []string{e}, BiasCorrected: true},
		{Name: "GFS_ONLY_BIAS", Sources: []string{g}, BiasCorrected: true},
		{Name: "BEST_MATCH_ONLY_BIAS", Sources: []string{b}, BiasCorrected: true},
		{Name: "ECMWF_GFS_EQUAL_BIAS", Sources: []string{e, g}, BiasCorrected: true},
		{Name: "ECMWF_BEST_EQUAL_BIAS", Sources: []string{e, b}, BiasCorrected: true},
		{Name: "GFS_BEST_EQUAL_BIAS", Sources: []string{g, b}, BiasCorrected: true},
		{Name: "ALL_EQUAL_BIAS", Sources: []string{e, g, b}, BiasCorrected: true},
		{Name: "ECMWF_GFS_ERROR_WEIGHTED", Sources: []string{e, g}, BiasCorrected: true, ErrorWeighted: true},
		{Name: "ECMWF_BEST_ERROR_WEIGHTED", Sources: []string{e, b}, BiasCorrected: true, ErrorWeighted: true},
		{Name: "GFS_BEST_ERROR_WEIGHTED", Sources: []string{g, b}, BiasCorrected: true, ErrorWeighted: true},
		{Name: "ALL_ERROR_WEIGHTED", Sources: []string{e, g, b}, BiasCorrected: true, ErrorWeighted: true},
	}
}

func fitModel(rows []daySample, spec modelSpec) map[string]sourceFit {
	fits := map[string]sourceFit{}
	weightTotal := 0.0
	for _, sourceName := range spec.Sources {
		res := make([]float64, len(rows))
		for i, s := range rows {
			res[i] = s.ObservedC - s.Forecasts[sourceName]
		}
		bias := 0.0
		if spec.BiasCorrected {
			bias = median(res)
		}
		abs := make([]float64, len(res))
		for i, x := range res {
			abs[i] = math.Abs(x - bias)
		}
		mae := mean(abs)
		maxCorr, redundancy := 0.0, 1
		for _, peerName := range spec.Sources {
			if peerName == sourceName {
				continue
			}
			a, b := make([]float64, len(rows)), make([]float64, len(rows))
			for i, s := range rows {
				a[i] = s.Forecasts[sourceName]
				b[i] = s.Forecasts[peerName]
			}
			corr := pearson(a, b)
			if corr > maxCorr {
				maxCorr = corr
			}
			if corr >= .995 {
				redundancy++
			}
		}
		weight := 1.0
		if spec.ErrorWeighted {
			weight = 1 / math.Pow(math.Max(.35, mae), 2) / float64(redundancy)
		}
		fits[sourceName] = sourceFit{BiasC: bias, MAEC: mae, Weight: weight, Redundancy: redundancy, MaxPeerCorr: maxCorr}
		weightTotal += weight
	}
	for name, fit := range fits {
		fit.Weight /= weightTotal
		fits[name] = fit
	}
	return fits
}

func modelResiduals(rows []daySample, fits map[string]sourceFit) []float64 {
	residuals := make([]float64, len(rows))
	for i, s := range rows {
		pred := 0.0
		for name, fit := range fits {
			pred += fit.Weight * (s.Forecasts[name] + fit.BiasC)
		}
		residuals[i] = s.ObservedC - pred
	}
	return residuals
}

func evaluateFit(rows []daySample, fits map[string]sourceFit, sigma float64, icao string) scores {
	var abs, sq, loss, covered float64
	for _, s := range rows {
		pred := 0.0
		for name, fit := range fits {
			pred += fit.Weight * (s.Forecasts[name] + fit.BiasC)
		}
		err := s.ObservedC - pred
		abs += math.Abs(err)
		sq += err * err
		if math.Abs(err) <= sigma {
			covered++
		}
		lo, hi := verificationBucketBounds(s.ObservedC, icao)
		prob := normalCDF((hi-pred)/sigma) - normalCDF((lo-pred)/sigma)
		loss += -math.Log(math.Max(1e-9, prob))
	}
	n := float64(len(rows))
	return scores{N: len(rows), MAEC: abs / n, RMSEC: math.Sqrt(sq / n), LogLoss: loss / n, Cover68: covered / n}
}

// verificationBucketBounds mirrors the contract observation operator. US
// airport markets use adjacent two-degree Fahrenheit buckets (for example
// 88-89F), while the other configured markets use whole Celsius degrees.
// Calibration must score the probability of the traded bucket, not an
// unrelated one-degree-Celsius interval.
func verificationBucketBounds(observedC float64, icao string) (float64, float64) {
	if strings.HasPrefix(strings.ToUpper(icao), "K") {
		observedF := observedC*9/5 + 32
		bucketLowF := math.Floor(math.Round(observedF)/2) * 2
		return (bucketLowF - .5 - 32) * 5 / 9, (bucketLowF + 1.5 - 32) * 5 / 9
	}
	roundedC := math.Round(observedC)
	return roundedC - .5, roundedC + .5
}

func metarDailyMax(client *http.Client, p pilot, start, end string) (map[string]float64, map[string]int, error) {
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return nil, nil, err
	}
	maxes := map[string]float64{}
	counts := map[string]int{}
	startYear, _ := strconv.Atoi(strings.Split(start, "-")[0])
	endYear, _ := strconv.Atoi(strings.Split(end, "-")[0])
	for year := startYear; year <= endYear; year++ {
		u := fmt.Sprintf("https://www.ncei.noaa.gov/data/global-hourly/access/%d/%s.csv", year, p.ISDStation)
		res, err := client.Get(u)
		if err != nil {
			return nil, nil, err
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return nil, nil, fmt.Errorf("NCEI %d status %d", year, res.StatusCode)
		}
		r := csv.NewReader(res.Body)
		header, err := r.Read()
		if err != nil {
			res.Body.Close()
			return nil, nil, err
		}
		idx := map[string]int{}
		for i, k := range header {
			idx[k] = i
		}
		for {
			row, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				continue
			}
			callMatches := strings.TrimSpace(row[idx["CALL_SIGN"]]) == p.ICAO
			if remIndex, ok := idx["REM"]; ok && remIndex < len(row) {
				callMatches = callMatches || strings.Contains(row[remIndex], "METAR "+p.ICAO+" ")
			}
			if row[idx["REPORT_TYPE"]] != "FM-15" || !callMatches {
				continue
			}
			at, err := time.Parse("2006-01-02T15:04:05", row[idx["DATE"]])
			if err != nil {
				continue
			}
			date := at.In(loc).Format("2006-01-02")
			if date < start || date > end {
				continue
			}
			parts := strings.Split(row[idx["TMP"]], ",")
			if len(parts) < 2 || parts[0] == "+9999" || parts[0] == "-9999" || strings.Contains("2367", parts[1]) {
				continue
			}
			raw, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}
			v := float64(raw) / 10
			if counts[date] == 0 || v > maxes[date] {
				maxes[date] = v
			}
			counts[date]++
		}
		res.Body.Close()
	}
	return maxes, counts, nil
}

func forecastDailyMax(client *http.Client, p pilot, start, end, model string) (map[string]float64, error) {
	q := url.Values{"latitude": {strconv.FormatFloat(p.Latitude, 'f', 5, 64)}, "longitude": {strconv.FormatFloat(p.Longitude, 'f', 5, 64)}, "start_date": {start}, "end_date": {end}, "hourly": {"temperature_2m_previous_day1"}, "timezone": {p.Timezone}}
	if model != "" {
		q.Set("models", model)
	}
	res, err := client.Get("https://previous-runs-api.open-meteo.com/v1/forecast?" + q.Encode())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", res.StatusCode)
	}
	var x openMeteo
	if err := json.NewDecoder(res.Body).Decode(&x); err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for i, t := range x.Hourly.Time {
		if i >= len(x.Hourly.Temperature) || x.Hourly.Temperature[i] == nil {
			continue
		}
		date := t[:10]
		v := *x.Hourly.Temperature[i]
		if old, ok := out[date]; !ok || v > old {
			out[date] = v
		}
	}
	return out, nil
}

func httpClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if b, err := os.ReadFile("config.json"); err == nil {
		var c struct {
			Proxy string `json:"proxy_url"`
		}
		if json.Unmarshal(b, &c) == nil && c.Proxy != "" {
			if u, err := url.Parse(c.Proxy); err == nil {
				tr.Proxy = http.ProxyURL(u)
			}
		}
	}
	return &http.Client{Timeout: 2 * time.Minute, Transport: tr}
}
func robustSigma(a []float64) float64 {
	m := median(a)
	d := make([]float64, len(a))
	for i, x := range a {
		d[i] = math.Abs(x - m)
	}
	return math.Max(.5, 1.4826*median(d))
}
func median(a []float64) float64 {
	b := append([]float64(nil), a...)
	sort.Float64s(b)
	if len(b)%2 == 1 {
		return b[len(b)/2]
	}
	return (b[len(b)/2-1] + b[len(b)/2]) / 2
}
func mean(a []float64) float64 {
	s := 0.0
	for _, x := range a {
		s += x
	}
	return s / float64(len(a))
}
func normalCDF(x float64) float64 { return .5 * (1 + math.Erf(x/math.Sqrt2)) }
func pearson(a, b []float64) float64 {
	if len(a) != len(b) || len(a) < 3 {
		return 0
	}
	am, bm := mean(a), mean(b)
	num, da, db := 0.0, 0.0, 0.0
	for i := range a {
		x, y := a[i]-am, b[i]-bm
		num += x * y
		da += x * x
		db += y * y
	}
	if da == 0 || db == 0 {
		return 0
	}
	return num / math.Sqrt(da*db)
}
func flagValue(prefix string) string {
	for _, x := range os.Args[1:] {
		if strings.HasPrefix(x, prefix) {
			return strings.TrimPrefix(x, prefix)
		}
	}
	return ""
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
