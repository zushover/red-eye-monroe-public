$ErrorActionPreference = 'Stop'
$projectRoot = $PSScriptRoot
New-Item -ItemType Directory -Force -Path (Join-Path $projectRoot 'data') | Out-Null
if (Test-Path -LiteralPath (Join-Path $projectRoot 'data\STOP')) {
    throw 'STOP marker exists. Rename data\STOP to resume collection.'
}
Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', ('"' + (Join-Path $projectRoot 'scripts\supervise.ps1') + '"')) -WorkingDirectory $projectRoot -WindowStyle Hidden
Write-Host 'Background paper collector started. Dashboard: http://localhost:8787'
