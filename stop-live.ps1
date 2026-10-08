$ErrorActionPreference = 'Stop'
$projectRoot = $PSScriptRoot
New-Item -ItemType Directory -Force -Path (Join-Path $projectRoot 'data') | Out-Null
New-Item -ItemType File -Force -Path (Join-Path $projectRoot 'data\STOP') | Out-Null
& (Join-Path $projectRoot 'disable-live.ps1')
Write-Host 'STOP is active. The live gateway will stop and no new order may be submitted.' -ForegroundColor Green
