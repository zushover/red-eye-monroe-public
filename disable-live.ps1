$ErrorActionPreference = 'Stop'
$controlPath = Join-Path $PSScriptRoot 'data\live-control.json'
$control = [ordered]@{ version = 1; enabled = $false; disabled_at = (Get-Date).ToUniversalTime().ToString('o') }
$control | ConvertTo-Json | Set-Content -LiteralPath $controlPath -Encoding utf8
Write-Host 'Live execution disabled.' -ForegroundColor Green

