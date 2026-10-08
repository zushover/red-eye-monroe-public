$ErrorActionPreference = 'Stop'
$researchRoot = $PSScriptRoot
$researchExe = Join-Path $researchRoot 'bin\weather-research-v2.exe'
if (-not (Test-Path -LiteralPath $researchExe)) { throw 'Build bin/weather-research-v2.exe first.' }
if (Get-NetTCPConnection -LocalPort 8789 -State Listen -ErrorAction SilentlyContinue) { Write-Host 'Port 8789 already in use. Open http://localhost:8789'; return }
New-Item -ItemType Directory -Force -Path (Join-Path $researchRoot 'data\weather-research') | Out-Null
$researchStamp = Get-Date -Format 'yyyyMMdd-HHmmss'
Start-Process -FilePath $researchExe -WorkingDirectory $researchRoot -WindowStyle Hidden -RedirectStandardOutput (Join-Path $researchRoot "data\weather-research\$researchStamp.out.log") -RedirectStandardError (Join-Path $researchRoot "data\weather-research\$researchStamp.err.log")
Write-Host 'Weather research started: http://localhost:8789'
