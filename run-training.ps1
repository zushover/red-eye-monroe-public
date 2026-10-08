$ErrorActionPreference = 'Stop'
$env:NODE_USE_ENV_PROXY = '1'
Push-Location $PSScriptRoot
try {
    node .\research-train.mjs
    if ($LASTEXITCODE -ne 0) { throw 'Weather training dataset build failed' }
    Write-Host '训练评估已更新：data\backtest\calibration-report.json（不会自动解锁实盘）'
} finally {
    Pop-Location
}
