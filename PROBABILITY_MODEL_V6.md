# Red Eye Monroe probability model v6 research plan

Status: research and shadow simulation only. Nothing in this document grants live execution permission.

## Target

Estimate the discrete distribution of the official station daily maximum, then map that distribution through the exact Polymarket bucket rule. Weather probability and market-price movement probability remain separate outputs.

## Observation operator

The verification variable is the official same-station daily maximum. The probability assigned to a contract is the integral over its actual interval, with all inequality tail buckets aggregated. US two-degree Fahrenheit buckets such as 88-89F must not be scored as one-degree Celsius buckets. Once an official observation reaches a value, mass below the reached bucket is censored to zero and the remaining distribution is renormalized.

## Hierarchical mixture-of-experts

For city c, phase h and day d:

`Tmax(c,d) = centre(c,h,d) + residual(c,season,regime,h)`

The centre is an adaptive combination of ECMWF, GFS and best-match forecasts. Weights are fitted with chronological nested validation and shrink toward regional/global weights when a city has weak evidence. A city-specific model is accepted only when untouched holdout log loss improves without materially degrading MAE.

Residual candidates:

1. Gaussian EMOS baseline.
2. City empirical Gaussian-kernel residual distribution.
3. Seasonal city kernel distribution using adjacent calendar months.
4. Regional partial-pooling kernel fallback for insufficient city samples.
5. Regime-conditioned residuals for marine flow, convective/precipitating, clear-dry and cloudy-humid days.

The distribution family is selected inside training data. The final chronological holdout is used once for acceptance, not for model selection.

## Intraday state-space update

Each local phase has a separate transition and observation model:

- PRE_DAY: calibrated multi-model prior.
- MORNING: prior plus overnight minimum and early bias update.
- HEATING: sequential residual filter using METAR level, warming rate and forecast-trajectory error.
- PRE_PEAK: remaining-heating survival/hazard model for whether another integer threshold will be crossed.
- POST_PEAK: one-way censoring/state machine dominated by the observed maximum.

The online update should use a robust Kalman or particle filter. Observation noise depends on report precision and age; process noise expands for precipitation, cloud transitions, sea-breeze onset and large ensemble spread.

## Probability calibration

Evaluation is performed on the traded buckets using log loss, Brier score, ranked probability score/CRPS, reliability curves and empirical coverage. Point MAE is secondary. Market prices are not an input to the weather distribution; they are a separate prior used only by the trading layer.

## Current exploratory result

The corrected 22-city holdout uses about 158-162 untouched days per city. After fixing the Fahrenheit bucket operator, 15 city centre models remain accepted and 7 remain unproven. Exploratory residual post-processing found the lowest holdout log loss with seasonal KDE in 12 cities, city KDE in 5 and Gaussian residuals in 5. Large exploratory improvements appeared for KLAX, KORD, KSFO, ZGGG and KSEA. These are hypotheses only because distribution-family comparison has seen the final holdout; nested re-selection is required before activation.

## Coverage policy

Every city receives a probability, but labels are evidence based:

- CITY_VALIDATED: city centre and residual family pass untouched holdout.
- HIERARCHICAL_VALIDATED: regional partial-pooling model passes a city holdout.
- GENERIC_RESEARCH: global fallback, simulation only.
- INTRADAY_VALIDATED: phase-specific sequential model passes walk-forward evaluation.

No city is promoted merely to claim full coverage.

## Primary references

- ECMWF Forecast User Guide, statistical post-processing and EMOS: https://confluence.ecmwf.int/pages/viewpage.action?pageId=283544832
- ECMWF ensemble forecast documentation: https://www.ecmwf.int/en/forecasts/documentation-and-support/medium-range-forecasts
- Google WeatherBench 2 probabilistic benchmark: https://research.google/blog/weatherbench-2-a-benchmark-for-the-next-generation-of-data-driven-weather-models/
- NOAA historical Model Output Statistics for maximum temperature: https://repository.library.noaa.gov/view/noaa/13473

