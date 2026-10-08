$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$mutex = New-Object System.Threading.Mutex($false, 'Local\PolymarketWeatherbotSupervisor')
if (-not $mutex.WaitOne(0)) { throw 'Another weatherbot supervisor is still running.' }
function Stop-LiveGatewayTree([System.Diagnostics.Process]$Gateway) {
    $children = @(Get-CimInstance Win32_Process -Filter "ParentProcessId=$($Gateway.Id)" -ErrorAction SilentlyContinue)
    foreach ($child in $children) {
        if ($child.CommandLine -and $child.CommandLine -match 'execution-sdk\\live-runner\.mjs') {
            Stop-Process -Id $child.ProcessId -Force -ErrorAction SilentlyContinue
        }
    }
    if (-not $Gateway.HasExited) { $Gateway.Kill(); $Gateway.WaitForExit() }
}
try {
    Set-Location -LiteralPath $projectRoot
    $stopFile = Join-Path $projectRoot 'data\STOP'
    $exe = Join-Path $projectRoot 'bin\weatherbot.exe'
    $gatewayScript = Join-Path $projectRoot 'scripts\run-live-gateway.ps1'
    $configFile = Join-Path $projectRoot 'config.live.json'
    $config = Get-Content -LiteralPath $configFile -Raw | ConvertFrom-Json
    if ($config.proxy_url) {
        $proxy = [Uri]$config.proxy_url
        if ($proxy.Scheme -notin @('http', 'https') -or -not $proxy.IsLoopback) {
            throw 'proxy_url must be an HTTP(S) loopback URL'
        }
        $env:HTTP_PROXY = $proxy.AbsoluteUri
        $env:HTTPS_PROXY = $proxy.AbsoluteUri
        $env:NO_PROXY = 'localhost'
    }
    while (-not (Test-Path -LiteralPath $stopFile)) {
        $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
        $bot = Start-Process -FilePath $exe -ArgumentList @('-config', 'config.live.json', 'run') -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput "data\live-bot-$stamp.out.log" -RedirectStandardError "data\live-bot-$stamp.err.log"
        $gateway = Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-File',('"' + $gatewayScript + '"')) -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput "data\live-gateway-$stamp.out.log" -RedirectStandardError "data\live-gateway-$stamp.err.log"
        [ordered]@{ started_at=(Get-Date).ToUniversalTime().ToString('o'); supervisor_pid=$PID; strategy_pid=$bot.Id; gateway_pid=$gateway.Id } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $projectRoot 'data\live-session.json') -Encoding utf8
        while (-not (Test-Path -LiteralPath $stopFile) -and -not $bot.HasExited -and -not $gateway.HasExited) {
            Start-Sleep -Seconds 2
            $bot.Refresh(); $gateway.Refresh()
        }
        if (Test-Path -LiteralPath $stopFile) {
            if (-not $gateway.WaitForExit(12000)) { Stop-LiveGatewayTree $gateway }
            if (-not $bot.HasExited) { $bot.Kill(); $bot.WaitForExit() }
            break
        }
        if (-not $bot.HasExited) { $bot.Kill(); $bot.WaitForExit() }
        if (-not $gateway.HasExited) { Stop-LiveGatewayTree $gateway }
        Start-Sleep -Seconds 15
    }
} finally {
    $mutex.ReleaseMutex(); $mutex.Dispose()
}
