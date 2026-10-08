# Red Eye Monroe

A local weather-market research platform combining station observations, forecast models, Polymarket order books, probability diagnostics, and portfolio monitoring.

Built with Go, JavaScript, and PowerShell. The dashboard brings together a global city view, market matrices, research opportunities, simulated portfolios, and an optional live-execution interface.

## Features

- Global city explorer with daylight shading and city-level weather and market details.
- ECMWF, GFS, Open-Meteo, and METAR comparisons, temperature curves, and station diagnostics.
- City-specific forecast calibration, empirical error distributions, probability decomposition, and chronological validation.
- Market search, ranking, local-date selection, original Fahrenheit bucket mapping, and Celsius display.
- Simulated portfolio research, execution-state reconciliation, dynamic exit rules, and portfolio accounting.
- Optional wallet integration with local Windows DPAPI credential storage and explicit live-session controls.

## Quick start

Use Windows and PowerShell. The bootstrap script downloads a checksum-verified portable Go toolchain when needed. Node.js is needed for the JavaScript tests, research scripts, and optional execution SDK.

```powershell
git clone https://github.com/zushover/red-eye-monroe-public.git
cd red-eye-monroe-public
Copy-Item config.example.json config.json
./start.ps1
```

Open [localhost:8787](http://localhost:8787). The example configuration starts in paper mode. Public weather and market requests do not require wallet credentials.

For background collection, run `./background.ps1`. Stop it with `./stop.ps1`. Runtime state is created locally under `data/`; a stop marker must be cleared locally before a stopped collector can resume.

## Tests

```powershell
./scripts/bootstrap.ps1
node --test dashboard-selection.test.mjs research-evaluate.test.mjs
cd execution-sdk
npm ci
node --test *.test.mjs
```

The bootstrap runs the Go tests and builds the collector. No account credentials are needed for the offline tests.

## Probability research

The research code evaluates forecast errors by station and local date, compares city models against a baseline, and tests probability calibration separately from trading returns. Reports include forecast-versus-observation comparisons, Brier scores, log loss, forward validation, and market/weather probability decomposition.

- [Research methods](docs/weather-probability-research.md)
- [City model audit](docs/model-audit.html)
- [Forward weather validation](docs/forward-weather-audit.html)
- [Probability decomposition](docs/probability-decomposition.html)
- [Weather probability V2 audit](docs/weather-v2-audit.html)
- [Station research workflow](WEATHER_RESEARCH.md)

HTML reports are static research snapshots. Open them locally after cloning. Raw historical inputs, runtime archives, account data, and order histories are excluded from this public repository. Research scripts require you to collect or supply their inputs locally before rerunning them.

Some city models improve temperature prediction, but the current research does not establish reliable trading profitability. Historical forecast lead times and METAR-derived daily maxima also have limitations when compared with exact market settlement rules.

## Optional execution integration

The source includes wallet checks, order guards, buy reconciliation, sell retries, and accounting. To install its dependencies:

```powershell
cd execution-sdk
npm ci
```

Copy `config.live.example.json` to a local `config.live.json` before configuring an execution session. Configure credentials only on your own machine using `setup-wallet.ps1`; they are encrypted for the current Windows user. `check-wallet.ps1` and `check-live.ps1` perform account checks. `start-live.ps1` requires a separately configured local `config.live.json`, a successful preflight, and explicit session activation. The public package includes no wallet, credentials, account permissions, or active session.

## Repository layout

```text
cmd/             Collector, calibration, and research entry points
internal/        Weather clients, models, pricing, dashboard, and execution
execution-sdk/   Wallet adapter, reconciliation, accounting, and tests
configs/         Public station metadata and calibration parameters
scripts/         Build and supervision helpers
docs/            Static research reports and method notes
research-*.mjs   Offline analysis and report generation
```

## Privacy

This is a fresh source-only export. It contains no original Git history, local wallet storage, API credentials, personal network configuration, trading ledgers, or account snapshots. Local configuration and generated data are excluded by `.gitignore`.

Public provider URLs and credential field names remain in the implementation because they are part of the integration interface. Supply your own credentials locally and never commit them.
