$ErrorActionPreference = 'Stop'
$taskName = 'PolymarketWeatherbotPaperCollector'
$task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($task) {
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
    Write-Host "Autostart removed: $taskName"
} else {
    Write-Host "Autostart is not installed: $taskName"
}
