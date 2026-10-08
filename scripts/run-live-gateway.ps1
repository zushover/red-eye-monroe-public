$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
& (Join-Path $projectRoot 'scripts\run-with-wallet.ps1') -NodeScript 'live-runner.mjs'
