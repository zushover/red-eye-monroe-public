# Phase-aware execution V7

## Scope

- `SOURCE_ALIGNED_EARLY` is paper-only: preday or before local 10:00, weather and market top bucket agree, at least three forecast peaks support the exact contract range, data quality >=65%, market concentration <70%, Ask 10–70c, usable spread/liquidity. A marginal model-minus-Ask down to -3pp is permitted solely as a research hypothesis, not a claim of positive value. Stake remains $1 subject to ledger and exchange-size gates.
- Existing real entries retain calibrated positive-value gates. The research branch cannot publish a live intent.
- Early held positions receive 30 minutes grace (preday 45 minutes); persistent nonpositive exit edge needs three distinct strategy snapshots. Later sessions retain the stricter two-scan/negative-edge exit. Model and 25% loss exits wait for early grace; a 40% emergency loss, day-end, and profitable dynamic trail do not.
- Live confirmation counts now advance once per new strategy snapshot, not once per account polling cycle.
- Current-day display rolls to the published next-day event at 95% market concentration or the learned local entry deadline. Existing old-day positions remain monitored. No next-day event is invented.

## Replay

Run `.\.tools\go\bin\go.exe run ./cmd/stage-backtest` to generate `data/backtest/stage-price-path.json` from actual local snapshots. It separates preday, overnight, 06–09, 09–12, prepeak, and postpeak, then measures 15/30/60-minute Bid moves and approximate net exit edge.

This is a diagnostic price-path replay, not a complete strategy PnL backtest: observations overlap, sample counts are not independent trades, depth/fills and full exit fees are not reconstructed. Independent city-day counts are included. Current evidence does not establish profitability; the new source-alignment branch stays in simulation until adequate independent holdout data supports it.
