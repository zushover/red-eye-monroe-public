$ErrorActionPreference = 'Stop'
$projectRoot = $PSScriptRoot
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot '.secrets\wallet.dpapi.json'))) {
    throw 'Encrypted wallet is not configured. Run setup-wallet.ps1 first.'
}
$confirmation = Read-Host 'Type ENABLE LIVE UNTIL STOP'
if ($confirmation -cne 'ENABLE LIVE UNTIL STOP') { throw 'Live execution remains disabled' }
New-Item -ItemType Directory -Force -Path (Join-Path $projectRoot 'data') | Out-Null
$control = [ordered]@{
    version = 1
    enabled = $true
    enabled_at = (Get-Date).ToUniversalTime().ToString('o')
    expires_at = $null
    persistent_authorization = $true
    maximum_order_dollars = 1.10
    daily_submission_limit_dollars = $null
    maximum_open_orders = 2
    maximum_simultaneous_positions = 6
    maximum_total_exposure_dollars = 8.00
}
$control | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $projectRoot 'data\live-control.json') -Encoding utf8
Write-Host 'Live gateway enabled until STOP/disable. Six simultaneous positions and $8 total cost remain hard limits.' -ForegroundColor Yellow
