$ErrorActionPreference = 'Stop'
$projectRoot = $PSScriptRoot
$dataRoot = Join-Path $projectRoot 'data'
$stopFile = Join-Path $dataRoot 'STOP'
$secretFile = Join-Path $projectRoot '.secrets\wallet.dpapi.json'
New-Item -ItemType Directory -Force -Path $dataRoot | Out-Null
if (-not (Test-Path -LiteralPath $secretFile)) {
    Write-Host 'First live start: configure a newly rotated credential set. Values stay hidden and DPAPI-encrypted on this PC.' -ForegroundColor Yellow
    & (Join-Path $projectRoot 'setup-wallet.ps1')
}
if (-not (Test-Path -LiteralPath $secretFile)) { throw 'Encrypted wallet setup did not complete.' }

# Stop any paper/live supervisor through its own marker before changing modes.
Stop-ScheduledTask -TaskName 'PolymarketWeatherbotPaperCollector' -ErrorAction SilentlyContinue
New-Item -ItemType File -Force -Path $stopFile | Out-Null
$deadline = (Get-Date).AddSeconds(30)
do {
    $occupied = Get-NetTCPConnection -LocalPort 8787 -State Listen -ErrorAction SilentlyContinue
    if (-not $occupied) { break }
    Start-Sleep -Milliseconds 500
} while ((Get-Date) -lt $deadline)
if ($occupied) {
    # A paper supervisor can be stopped while its collector remains orphaned. Only
    # terminate the exact workspace binary that owns the dashboard port; never
    # kill an unrelated listener or search by process name alone.
    $expectedCollector = [IO.Path]::GetFullPath((Join-Path $projectRoot 'bin\weatherbot.exe'))
    $owners = @($occupied | Select-Object -ExpandProperty OwningProcess -Unique)
    $safeOwners = @()
    foreach ($ownerPid in $owners) {
        $owner = Get-CimInstance Win32_Process -Filter "ProcessId=$ownerPid" -ErrorAction SilentlyContinue
        if ($owner -and $owner.ExecutablePath -and
            [IO.Path]::GetFullPath($owner.ExecutablePath) -ieq $expectedCollector) {
            $safeOwners += $ownerPid
        }
    }
    if ($safeOwners.Count -eq $owners.Count -and $safeOwners.Count -gt 0) {
        foreach ($ownerPid in $safeOwners) {
            Stop-Process -Id $ownerPid -Force -ErrorAction Stop
        }
        Start-Sleep -Seconds 1
        $occupied = Get-NetTCPConnection -LocalPort 8787 -State Listen -ErrorAction SilentlyContinue
    }
}
if ($occupied) { throw 'Port 8787 is still occupied by an unverified process. Keep STOP in place and inspect the listener.' }

& (Join-Path $projectRoot 'scripts\bootstrap.ps1')
& (Join-Path $projectRoot 'check-live.ps1')
if ($LASTEXITCODE -ne 0) { throw 'Wallet/network preflight failed. No live session was started.' }

$confirmation = Read-Host 'Type START LIVE WEATHER SESSION'
if ($confirmation -cne 'START LIVE WEATHER SESSION') { throw 'Live execution remains disabled.' }
$now = (Get-Date).ToUniversalTime()
$control = [ordered]@{
    version = 1; enabled = $true
    enabled_at = $now.ToString('o'); expires_at = $null; persistent_authorization = $true
    maximum_order_dollars = 1.10; daily_submission_limit_dollars = $null
    maximum_open_orders = 2; maximum_simultaneous_positions = 6; maximum_total_exposure_dollars = 8.00
}
$control | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $dataRoot 'live-control.json') -Encoding utf8
Remove-Item -LiteralPath $stopFile -Force
$supervisor = Join-Path $projectRoot 'scripts\supervise-live.ps1'
Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-File',('"' + $supervisor + '"')) -WorkingDirectory $projectRoot -WindowStyle Hidden
Start-Sleep -Seconds 3
Start-Process 'http://localhost:8787/'
Write-Host 'Live weather session started until STOP/disable. $1 notional/order, $1.10 fee-inclusive cap, 6 simultaneous positions, $8 total cost including fee reserve.' -ForegroundColor Yellow
Write-Host 'Uncalibrated stations cannot submit orders. Run stop-live.ps1 for immediate stop.' -ForegroundColor Yellow
