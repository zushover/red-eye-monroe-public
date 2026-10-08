package model

import (
	"fmt"
	"math"
	"sort"
	"time"

	"weatherbot/internal/domain"
)

func Build(rule domain.Rule, station domain.Station, obs []domain.Observation, forecast domain.Forecast, now time.Time) (domain.Distribution, error) {
	return BuildEnsemble(rule, station, obs, forecast, nil, now)
}

// BuildEnsemble keeps the best-match forecast for weather covariates and uses
// explicit global models for the temperature centre when both are available.
func BuildEnsemble(rule domain.Rule, station domain.Station, obs []domain.Observation, forecast domain.Forecast, sources map[string]domain.Forecast, now time.Time) (domain.Distribution, error) {
	return BuildProbabilistic(rule, station, obs, forecast, sources, nil, now)
}

func BuildProbabilistic(rule domain.Rule, station domain.Station, obs []domain.Observation, forecast domain.Forecast, sources map[string]domain.Forecast, memberPeaksC []float64, now time.Time) (domain.Distribution, error) {
	loc, err := time.LoadLocation(station.Timezone)
	if err != nil {
		return domain.Distribution{}, err
	}
	if !finite(station.BaseSigmaC) || station.BaseSigmaC <= 0 || !finite(station.ForecastBiasC) {
		return domain.Distribution{}, fmt.Errorf("invalid model parameters")
	}
	if forecast.ReceivedAt.After(now) || now.Sub(forecast.ReceivedAt) > 2*time.Hour {
		return domain.Distribution{}, fmt.Errorf("forecast not available at decision time or stale")
	}
	target, err := time.ParseInLocation("2006-01-02", rule.LocalDate, loc)
	if err != nil {
		return domain.Distribution{}, err
	}
	if err := validateForecastDay(forecast, target, now); err != nil {
		return domain.Distribution{}, fmt.Errorf("best-match forecast: %w", err)
	}

	dayObs := make([]domain.Observation, 0, len(obs))
	for _, o := range obs {
		if o.Station == station.ICAO && !o.ObservedAt.After(now) && !o.ReceivedAt.After(now) && finite(o.TempC) && sameDate(o.ObservedAt.In(loc), target) {
			dayObs = append(dayObs, o)
		}
	}
	sort.Slice(dayObs, func(i, j int) bool { return dayObs[i].ObservedAt.Before(dayObs[j].ObservedAt) })
	latest := domain.Observation{}
	if len(dayObs) > 0 {
		latest = dayObs[len(dayObs)-1]
	}
	observedMax := latest.TempC
	for _, o := range dayObs {
		if o.TempC > observedMax {
			observedMax = o.TempC
		}
	}

	forecastPeak := -100.0
	futurePeak := -100.0
	lastHeating := target
	localNow := now.In(loc)
	var peakCloud, peakPrecip float64
	var peakCloudAvailable, peakPrecipAvailable bool
	for _, h := range forecast.Hourly {
		if !finite(h.TempC) || !sameDate(h.LocalTime.In(loc), target) {
			continue
		}
		if h.TempC > forecastPeak {
			forecastPeak = h.TempC
			peakCloud, peakPrecip = h.CloudPct, h.PrecipProbPct
			peakCloudAvailable, peakPrecipAvailable = h.CloudAvailable, h.PrecipAvailable
		}
		if !h.LocalTime.Before(localNow) && h.TempC > futurePeak {
			futurePeak = h.TempC
		}
		if h.LocalTime.After(lastHeating) && h.TempC >= forecastPeak-0.5 {
			lastHeating = h.LocalTime
		}
	}
	if forecastPeak < -50 {
		return domain.Distribution{}, fmt.Errorf("no forecast for local date %s", rule.LocalDate)
	}
	bestMatchPeak := forecastPeak
	sourcePeaks := map[string]float64{"Open-Meteo best match": forecastPeak}
	explicitPeaks := make([]float64, 0, len(sources))
	explicitFuture := make([]float64, 0, len(sources))
	sourceFuturePeaks := make(map[string]float64)
	if futurePeak > -50 {
		sourceFuturePeaks["Open-Meteo best match"] = futurePeak
	}
	for name, sourceForecast := range sources {
		if validateForecastDay(sourceForecast, target, now) != nil {
			continue
		}
		peak, future := -100.0, -100.0
		for _, h := range sourceForecast.Hourly {
			if !finite(h.TempC) || !sameDate(h.LocalTime.In(loc), target) {
				continue
			}
			if h.TempC > peak {
				peak = h.TempC
			}
			if !h.LocalTime.Before(localNow) && h.TempC > future {
				future = h.TempC
			}
		}
		if peak > -50 {
			sourcePeaks[name] = peak
			explicitPeaks = append(explicitPeaks, peak)
			if future > -50 {
				explicitFuture = append(explicitFuture, future)
				sourceFuturePeaks[name] = future
			}
		}
	}
	if len(explicitPeaks) < 2 {
		return domain.Distribution{}, fmt.Errorf("two fresh complete explicit model forecasts are required")
	}
	localDate := localNow.Format("2006-01-02")
	tomorrowDate := localNow.AddDate(0, 0, 1).Format("2006-01-02")
	useCityCalibration := (rule.LocalDate == localDate || rule.LocalDate == tomorrowDate) &&
		station.CityCalibrationValid &&
		station.CityCalibrationLeadH == 24 && station.CityCalibrationSamples >= 300 &&
		station.CityCalibrationHoldout >= 100 && station.CityCalibrationSigmaC > 0
	if useCityCalibration {
		weighted, totalWeight, used := 0.0, 0.0, 0
		for name, weight := range station.CitySourceWeight {
			peak, exists := sourcePeaks[name]
			if !exists || !finite(weight) || weight <= 0 {
				continue
			}
			weighted += weight * (peak + station.CitySourceBiasC[name])
			totalWeight += weight
			used++
		}
		if used >= 1 && totalWeight > 0 {
			forecastPeak = weighted / totalWeight
			futurePeak = forecastPeak
		} else {
			useCityCalibration = false
		}
	}
	if !useCityCalibration {
		forecastPeak = average(explicitPeaks)
	}
	if useCityCalibration {
		weighted, totalWeight, used := 0.0, 0.0, 0
		for name, weight := range station.CitySourceWeight {
			peak, exists := sourceFuturePeaks[name]
			if !exists || !finite(weight) || weight <= 0 {
				continue
			}
			weighted += weight * (peak + station.CitySourceBiasC[name])
			totalWeight += weight
			used++
		}
		if used >= 1 && totalWeight > 0 {
			futurePeak = weighted / totalWeight
		}
	} else if len(explicitFuture) >= 2 {
		futurePeak = average(explicitFuture)
	}
	lastHeating = target
	for _, h := range forecast.Hourly {
		if sameDate(h.LocalTime.In(loc), target) && h.TempC >= bestMatchPeak-.5 && h.LocalTime.After(lastHeating) {
			lastHeating = h.LocalTime
		}
	}
	phase := phaseForTime(localNow, target, lastHeating)
	if futurePeak < -50 {
		futurePeak = latest.TempC
	}

	mean := forecastPeak
	if !useCityCalibration {
		mean += station.ForecastBiasC
	}
	// Recent same-station observation errors correct the remaining forecast.
	residuals := []float64{}
	for _, o := range dayObs {
		if now.Sub(o.ObservedAt) > 3*time.Hour {
			continue
		}
		best := time.Hour
		matched := 0.0
		for _, h := range forecast.Hourly {
			delta := h.LocalTime.Sub(o.ObservedAt)
			if delta < 0 {
				delta = -delta
			}
			if delta < best && finite(h.TempC) {
				best = delta
				matched = h.TempC
			}
		}
		if best <= 30*time.Minute {
			residuals = append(residuals, o.TempC-matched-station.ForecastBiasC)
		}
	}
	sort.Float64s(residuals)
	residual := 0.0
	if len(residuals) > 0 {
		residual = residuals[len(residuals)/2]
	}
	correction := math.Max(-2, math.Min(2, residual)) * .6
	trajectory := evaluateTrajectory(dayObs, forecast, sources, station, target, loc)
	if useCityCalibration && trajectory.N > 0 {
		// Trajectory bias is predicted-observed, hence the minus sign. The
		// calibrated T-24 city model remains the prior and same-station METAR
		// supplies a bounded likelihood-style correction during the local day.
		correction = math.Max(-2, math.Min(2, -trajectory.Bias)) * .6
		residual = -trajectory.Bias
	}
	rate := 0.0
	for _, o := range dayObs {
		dt := latest.ObservedAt.Sub(o.ObservedAt).Hours()
		if dt >= .5 && dt <= 2 {
			rate = (latest.TempC - o.TempC) / dt
			break
		}
	}
	spread := rangeWidth(explicitPeaks)
	unstable := (peakPrecipAvailable && peakPrecip >= 40) || spread >= 2.5 || math.Abs(residual) > 2 || math.Abs(rate) > 3
	if (phase == "MORNING" || phase == "INTRADAY" || phase == "LATE_DAY") && len(dayObs) > 0 {
		trajectoryPeak := math.Max(observedMax, futurePeak+station.ForecastBiasC+correction)
		mean = 0.72*trajectoryPeak + 0.28*math.Max(observedMax, forecastPeak+station.ForecastBiasC)
		if mean < observedMax {
			mean = observedMax
		}
	}
	sigma := station.BaseSigmaC
	if useCityCalibration {
		sigma = station.CityCalibrationSigmaC
	}
	if sameDate(localNow, target) {
		hoursLeft := math.Max(0, lastHeating.Sub(localNow).Hours())
		shrink := 0.48 + math.Min(0.52, hoursLeft/12)
		sigma *= shrink
	}
	// A city-calibrated PRE_DAY distribution must reproduce the exact model
	// scored on the chronological holdout. Generic widening and member mixing
	// would silently turn it into a different, unvalidated probability model.
	if !useCityCalibration {
		if peakCloudAvailable && peakCloud >= 70 {
			sigma += 0.15
		}
		if peakPrecipAvailable && peakPrecip >= 40 {
			sigma += 0.25
		}
		sigma += math.Min(1.5, spread*.35)
		if unstable {
			sigma += .5
		}
	}
	if sigma < 0.65 {
		sigma = 0.65
	}
	// Once the local heating window has ended, deterministic forecasts that
	// missed the observed day must no longer keep the distribution centred on
	// their stale peak.  The observed same-station maximum becomes the anchor;
	// a narrow residual uncertainty remains for reporting/rounding differences.
	if sameDate(localNow, target) && len(dayObs) > 0 && !lastHeating.After(localNow) {
		mean = observedMax
		sigma = math.Max(.35, math.Min(.65, station.BaseSigmaC*.35+spread*.08))
	}

	meanUnit, sigmaUnit, observedUnit := mean, sigma, observedMax
	if rule.Unit == "F" {
		meanUnit = cToF(mean)
		sigmaUnit = sigma * 9 / 5
		observedUnit = cToF(observedMax)
	}
	// METAR is a proxy, not verified settlement evidence. Retain downside tails.
	_ = observedUnit
	probs := unboundedDistribution(meanUnit, sigmaUnit)
	// Heavy tails avoid unjustified precision from a single deterministic forecast.
	if !useCityCalibration {
		for n, p := range unboundedDistribution(meanUnit, sigmaUnit*2) {
			probs[n] = .85*probs[n] + .15*p
		}
	}
	if !useCityCalibration && len(memberPeaksC) >= 20 && phaseForTime(localNow, target, lastHeating) != "LATE_DAY" {
		kernelBase := station.BaseSigmaC
		if useCityCalibration {
			kernelBase = station.CityCalibrationSigmaC
		}
		kernelC := math.Max(.4, kernelBase*.45)
		if sameDate(localNow, target) {
			kernelC = math.Max(.3, kernelC*(.55+.45*math.Min(1, math.Max(0, lastHeating.Sub(localNow).Hours())/8)))
		}
		memberProbs := map[int]float64{}
		for _, peakC := range memberPeaksC {
			conditioned := peakC + station.ForecastBiasC
			if useCityCalibration {
				conditioned = peakC + station.CitySourceBiasC["ECMWF IFS 0.25°"]
			}
			if sameDate(localNow, target) && len(dayObs) > 0 {
				conditioned = math.Max(observedMax, conditioned+correction)
			}
			centre, kernel := conditioned, kernelC
			if rule.Unit == "F" {
				centre, kernel = cToF(conditioned), kernelC*9/5
			}
			for n, p := range unboundedDistribution(centre, kernel) {
				memberProbs[n] += p / float64(len(memberPeaksC))
			}
		}
		for n := range probs {
			probs[n] *= .25
		}
		for n, p := range memberProbs {
			probs[n] += .75 * p
		}
	}
	agreement := 0.0
	if len(explicitPeaks) >= 2 {
		agreement = math.Max(0, 1-math.Min(1, spread/3))
	}
	freshMinutes := now.Sub(latest.ObservedAt).Minutes()
	confidence := 0.85
	if freshMinutes > 90 {
		confidence -= 0.2
	}
	if len(dayObs) < 3 {
		confidence -= 0.15
	}
	if peakPrecipAvailable && peakPrecip >= 40 {
		confidence -= 0.15
	}
	if !peakCloudAvailable || !peakPrecipAvailable {
		confidence -= 0.1
	}
	if len(memberPeaksC) < 20 && !useCityCalibration {
		confidence -= 0.12
	}
	if confidence < 0.25 {
		confidence = 0.25
	}
	if localNow.Format("2006-01-02") > rule.LocalDate {
		confidence = 0
	}
	if len(dayObs) == 0 {
		freshMinutes = 0
		if (rule.LocalDate == tomorrowDate || phase == "OVERNIGHT") && useCityCalibration {
			confidence = math.Max(.65, math.Min(.82, .65+.17*agreement))
		} else {
			confidence = .5
		}
	}
	if phase == "INTRADAY" || phase == "LATE_DAY" {
		if len(dayObs) == 0 || freshMinutes > 90 {
			confidence = 0
		}
	}
	if phase == "AWAITING_SETTLEMENT" {
		confidence = 0
	}

	probabilityStatus := "UNCALIBRATED_EXPERIMENTAL"
	modelVersion := "multisource-trajectory-v5-market-anchored"
	calibrationReady := station.Calibrated
	if station.Calibrated {
		probabilityStatus = "CALIBRATED"
	}
	if useCityCalibration {
		// A validated city model can trade tomorrow as a T-24 prior. During the
		// local day it additionally needs fresh observations and a satisfactory
		// same-day trajectory fit; otherwise it remains visible research only.
		calibrationReady = rule.LocalDate == tomorrowDate && agreement >= .30
		if rule.LocalDate == localDate {
			switch phase {
			case "OVERNIGHT":
				calibrationReady = agreement >= .30
			case "MORNING":
				calibrationReady = len(dayObs) >= 1 && freshMinutes <= 60 && trajectory.N >= 1 && trajectory.Score >= .40 && agreement >= .30
			case "INTRADAY", "LATE_DAY":
				calibrationReady = len(dayObs) >= 3 && freshMinutes <= 60 && trajectory.N >= 3 && trajectory.Score >= .55 && agreement >= .30
			}
		}
		if calibrationReady {
			probabilityStatus = "CALIBRATED"
		} else if rule.LocalDate == localDate {
			probabilityStatus = "CITY_CALIBRATED_PRIOR_INTRADAY_RESEARCH"
		} else {
			probabilityStatus = "CITY_CALIBRATED_PRIOR_RESEARCH"
		}
		modelVersion = station.CityCalibrationVersion
	}
	probabilityConfidence := probabilityConfidence(station, confidence, agreement, phase, trajectory)
	return domain.Distribution{
		LocalTime: localNow.Format("2006-01-02 15:04 MST"), LocalMinute: localNow.Hour()*60 + localNow.Minute(), PeakWindowEndLocal: lastHeating.In(loc).Format("2006-01-02 15:04 MST"),
		ModelVersion: modelVersion, ForecastResidualC: residual, WarmingRateCPerHour: rate, UnstableWeather: unstable,
		Phase: phase, HasObservation: len(dayObs) > 0, ObservationAgeMinutes: freshMinutes,
		MeanC: mean, SigmaC: sigma, ObservedMaxC: observedMax, LatestTempC: latest.TempC,
		ForecastPeakC: forecastPeak, RemainingHours: math.Max(0, lastHeating.Sub(localNow).Hours()),
		Confidence: confidence, DataQuality: confidence, ProbabilityConfidence: probabilityConfidence,
		TrajectoryFit: trajectory.Score, TrajectoryMAEC: trajectory.MAE, TrajectoryBiasC: trajectory.Bias, TrajectoryDirectionFit: trajectory.Direction, TrajectoryPoints: trajectory.N,
		CalibrationReady: calibrationReady, ProbabilityStatus: probabilityStatus,
		IndependentModelReady: station.CityCalibrationValid, IndependentModelActive: useCityCalibration, CalibrationModel: station.CityCalibrationModel,
		CalibrationVersion: station.CityCalibrationVersion, CalibrationSamples: station.CityCalibrationSamples, CalibrationHoldout: station.CityCalibrationHoldout,
		SourcePeaksC: sourcePeaks, EnsembleSpreadC: spread, EnsembleAgreement: agreement, EnsembleSourceCount: len(explicitPeaks), EnsembleMemberCount: len(memberPeaksC), IntegerProbByUnit: probs,
	}, nil
}

type trajectoryScore struct {
	Score     float64
	MAE       float64
	Bias      float64
	Direction float64
	N         int
}

func evaluateTrajectory(obs []domain.Observation, bestMatch domain.Forecast, sources map[string]domain.Forecast, station domain.Station, target time.Time, loc *time.Location) trajectoryScore {
	forecasts := map[string]domain.Forecast{"Open-Meteo best match": bestMatch}
	for name, forecast := range sources {
		forecasts[name] = forecast
	}
	weights := station.CitySourceWeight
	useBias := station.CityCalibrationValid && len(weights) > 0
	if !useBias {
		weights = map[string]float64{"ECMWF IFS 0.25°": .5, "NCEP GFS seamless": .5}
	}
	type pair struct{ observed, predicted float64 }
	pairs := make([]pair, 0, len(obs))
	for _, observation := range obs {
		predicted, total := 0.0, 0.0
		for name, weight := range weights {
			forecast, ok := forecasts[name]
			if !ok || weight <= 0 {
				continue
			}
			value, ok := nearestForecastTemperature(forecast, observation.ObservedAt, target, loc)
			if !ok {
				continue
			}
			if useBias {
				value += station.CitySourceBiasC[name]
			}
			predicted += weight * value
			total += weight
		}
		if total > 0 {
			pairs = append(pairs, pair{observed: observation.TempC, predicted: predicted / total})
		}
	}
	if len(pairs) == 0 {
		return trajectoryScore{}
	}
	absError, signedError := 0.0, 0.0
	for _, p := range pairs {
		err := p.predicted - p.observed
		absError += math.Abs(err)
		signedError += err
	}
	mae, bias := absError/float64(len(pairs)), signedError/float64(len(pairs))
	direction := 0.5
	if len(pairs) >= 2 {
		total := 0.0
		for i := 1; i < len(pairs); i++ {
			observedDelta := pairs[i].observed - pairs[i-1].observed
			predictedDelta := pairs[i].predicted - pairs[i-1].predicted
			total += math.Exp(-math.Abs(observedDelta-predictedDelta) / 1.25)
		}
		direction = total / float64(len(pairs)-1)
	}
	coverage := math.Min(1, float64(len(pairs))/6)
	score := .45*math.Exp(-mae/1.5) + .20*math.Exp(-math.Abs(bias)/1.25) + .20*direction + .15*coverage
	return trajectoryScore{Score: math.Max(0, math.Min(1, score)), MAE: mae, Bias: bias, Direction: direction, N: len(pairs)}
}

func nearestForecastTemperature(forecast domain.Forecast, at, target time.Time, loc *time.Location) (float64, bool) {
	bestDelta, value := 46*time.Minute, 0.0
	for _, hour := range forecast.Hourly {
		if !finite(hour.TempC) || !sameDate(hour.LocalTime.In(loc), target) {
			continue
		}
		delta := hour.LocalTime.Sub(at)
		if delta < 0 {
			delta = -delta
		}
		if delta < bestDelta {
			bestDelta, value = delta, hour.TempC
		}
	}
	return value, bestDelta <= 45*time.Minute
}

func probabilityConfidence(station domain.Station, dataQuality, agreement float64, phase string, trajectory trajectoryScore) float64 {
	history := .35
	if station.CityCalibrationValid && station.CityBaselineMAEC > 0 && station.CityBaselineLogLoss > 0 {
		maeGain := math.Max(0, math.Min(1, (station.CityBaselineMAEC-station.CityHoldoutMAEC)/station.CityBaselineMAEC/.20))
		lossGain := math.Max(0, math.Min(1, (station.CityBaselineLogLoss-station.CityHoldoutLogLoss)/station.CityBaselineLogLoss/.20))
		sampleStrength := math.Max(0, math.Min(1, float64(station.CityCalibrationHoldout)/160))
		history = .55 + .15*sampleStrength + .15*maeGain + .15*lossGain
	}
	quality := math.Max(0, math.Min(1, dataQuality))
	agreement = math.Max(0, math.Min(1, agreement))
	value := .45*history + .25*quality + .30*agreement
	if phase == "MORNING" || phase == "INTRADAY" || phase == "LATE_DAY" {
		// Once same-station observations exist, live trajectory fit is the main
		// evidence. Historical skill remains only a prior.
		if trajectory.N >= 2 {
			value = .65*trajectory.Score + .15*quality + .10*agreement + .10*history
		} else {
			value = .25*history + .35*quality + .15*agreement
			value = math.Min(value, .45)
		}
	}
	if !station.CityCalibrationValid && value > .72 {
		value = .72
	}
	if phase == "AWAITING_SETTLEMENT" || dataQuality <= 0 {
		return 0
	}
	return math.Max(.05, math.Min(.98, value))
}

func phaseForTime(localNow, target, lastHeating time.Time) string {
	if localNow.Format("2006-01-02") > target.Format("2006-01-02") {
		return "AWAITING_SETTLEMENT"
	}
	if sameDate(localNow, target) && !lastHeating.After(localNow) {
		return "LATE_DAY"
	}
	if sameDate(localNow, target) {
		minute := localNow.Hour()*60 + localNow.Minute()
		if minute < 6*60 {
			return "OVERNIGHT"
		}
		if minute < 9*60 {
			return "MORNING"
		}
		return "INTRADAY"
	}
	return "PRE_DAY"
}

func validateForecastDay(forecast domain.Forecast, target, now time.Time) error {
	if forecast.ReceivedAt.IsZero() || forecast.ReceivedAt.After(now) || now.Sub(forecast.ReceivedAt) > 2*time.Hour {
		return fmt.Errorf("forecast is unavailable, future-dated, or stale")
	}
	end := target.AddDate(0, 0, 1)
	expected := int(end.Sub(target).Hours())
	hours := make(map[int64]bool, expected)
	for _, h := range forecast.Hourly {
		if !finite(h.TempC) || h.LocalTime.Before(target) || !h.LocalTime.Before(end) {
			continue
		}
		hours[h.LocalTime.UTC().Truncate(time.Hour).Unix()] = true
	}
	for at := target; at.Before(end); at = at.Add(time.Hour) {
		if !hours[at.UTC().Truncate(time.Hour).Unix()] {
			return fmt.Errorf("hourly temperature coverage is incomplete (%d/%d)", len(hours), expected)
		}
	}
	return nil
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func rangeWidth(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	lo, hi := values[0], values[0]
	for _, value := range values[1:] {
		lo = math.Min(lo, value)
		hi = math.Max(hi, value)
	}
	return hi - lo
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func unboundedDistribution(mean, sigma float64) map[int]float64 {
	p := map[int]float64{}
	total := 0.0
	for n := int(math.Floor(mean - 8*sigma)); n <= int(math.Ceil(mean+8*sigma)); n++ {
		v := normalCDF((float64(n)+.5-mean)/sigma) - normalCDF((float64(n)-.5-mean)/sigma)
		p[n] = v
		total += v
	}
	for n, v := range p {
		p[n] = v / total
	}
	return p
}

func integerDistribution(mean, sigma, observedMax float64) map[int]float64 {
	lo := int(math.Floor(mean - 6*sigma))
	hi := int(math.Ceil(mean + 6*sigma))
	minReached := int(math.Round(observedMax))
	if lo > minReached {
		lo = minReached
	}
	probs := make(map[int]float64)
	var sum float64
	for n := lo; n <= hi; n++ {
		if n < minReached {
			continue
		}
		lower := (float64(n) - 0.5 - mean) / sigma
		upper := (float64(n) + 0.5 - mean) / sigma
		p := normalCDF(upper) - normalCDF(lower)
		if n == minReached {
			p = normalCDF(upper)
		}
		if p > 0 {
			probs[n] = p
			sum += p
		}
	}
	if sum > 0 {
		for n, p := range probs {
			probs[n] = p / sum
		}
	}
	return probs
}

func normalCDF(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }
func cToF(c float64) float64      { return c*9/5 + 32 }
func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
