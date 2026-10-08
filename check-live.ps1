$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'scripts\run-with-wallet.ps1') -NodeScript 'executor.mjs' -ScriptArguments @('status')

