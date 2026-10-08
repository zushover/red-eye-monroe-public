$ErrorActionPreference = 'Stop'
$projectRoot = $PSScriptRoot
$exe = Join-Path $projectRoot 'bin\weatherbot.exe'

if (-not (Test-Path -LiteralPath $exe)) {
    & (Join-Path $projectRoot 'scripts\bootstrap.ps1')
}

Push-Location $projectRoot
try {
    & $exe run
} finally {
    Pop-Location
}
