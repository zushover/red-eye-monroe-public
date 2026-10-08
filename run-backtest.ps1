$ErrorActionPreference = 'Stop'
$env:NODE_USE_ENV_PROXY = '1'
Push-Location $PSScriptRoot
try {
    node .\research-backfill.mjs --days=120 --max=500
    if ($LASTEXITCODE -ne 0) { throw 'Historical dataset build failed' }
    Write-Host '历史盘口与收敛窗口已更新：data\backtest\convergence-windows.json'
} finally {
    Pop-Location
}
