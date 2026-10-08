$ErrorActionPreference = 'Stop'

$projectRoot = $PSScriptRoot
$supervisor = Join-Path $projectRoot 'scripts\supervise.ps1'
$taskName = 'PolymarketWeatherbotPaperCollector'
$currentUser = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name

if (-not (Test-Path -LiteralPath (Join-Path $projectRoot 'bin\weatherbot.exe'))) {
    throw 'bin\weatherbot.exe is missing. Run scripts\bootstrap.ps1 first.'
}

$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument ('-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "' + $supervisor + '"') -WorkingDirectory $projectRoot
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $currentUser
$principal = New-ScheduledTaskPrincipal -UserId $currentUser -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries

Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description 'Keeps the Polymarket weatherbot paper collector running after Windows login.' -Force | Out-Null
Start-ScheduledTask -TaskName $taskName
Write-Host "Autostart installed and started: $taskName"
Write-Host 'Dashboard: http://localhost:8787/'
