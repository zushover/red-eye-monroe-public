$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$mutex = New-Object System.Threading.Mutex($false, 'Local\PolymarketWeatherbotSupervisor')
if (-not $mutex.WaitOne(0)) { exit 0 }
try {
    Set-Location -LiteralPath $projectRoot
    $stopFile = Join-Path $projectRoot 'data\STOP'
    $exe = Join-Path $projectRoot 'bin\weatherbot.exe'
    $supervisorLog = Join-Path $projectRoot 'data\supervisor.log'
    $configFile = Join-Path $projectRoot 'config.json'
    if (Test-Path -LiteralPath $configFile) {
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
    }
    Add-Content -LiteralPath $supervisorLog -Value "$(Get-Date -Format o) supervisor started (PID $PID)"
    while (-not (Test-Path -LiteralPath $stopFile)) {
        try {
            if (-not (Test-Path -LiteralPath $exe)) { throw "Collector binary missing: $exe" }
            $stamp = Get-Date -Format 'yyyyMMdd-HHmmss-fff'
            $child = Start-Process -FilePath $exe -ArgumentList 'run' -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput "data\run-$stamp.out.log" -RedirectStandardError "data\run-$stamp.err.log"
            Add-Content -LiteralPath $supervisorLog -Value "$(Get-Date -Format o) collector started (PID $($child.Id))"
            while (-not $child.WaitForExit(2000)) {
                if (Test-Path -LiteralPath $stopFile) {
                    # PID belongs to the process created above, never a broad process kill.
                    $child.Kill()
                    $child.WaitForExit()
                    break
                }
            }
            Add-Content -LiteralPath $supervisorLog -Value "$(Get-Date -Format o) collector exited (code $($child.ExitCode))"
        } catch {
            Add-Content -LiteralPath $supervisorLog -Value "$(Get-Date -Format o) supervisor caught: $($_.Exception.Message)"
        }
        if (-not (Test-Path -LiteralPath $stopFile)) { Start-Sleep -Seconds 15 }
    }
    Add-Content -LiteralPath $supervisorLog -Value "$(Get-Date -Format o) stop marker detected"
} finally {
    try { $mutex.ReleaseMutex() } catch {}
    $mutex.Dispose()
}
