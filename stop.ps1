$ErrorActionPreference = 'Stop'
New-Item -ItemType File -Force -Path (Join-Path $PSScriptRoot 'data\STOP') | Out-Null
Write-Host 'Stop requested. Background collector will stop within a few seconds.'
