$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$toolRoot = Join-Path $projectRoot '.tools'
$goRoot = Join-Path $toolRoot 'go'
$goExe = Join-Path $goRoot 'bin\go.exe'
$archive = Join-Path $toolRoot 'go1.27.1.windows-amd64.zip'
$expectedHash = 'a3911b5e0e1b1053f25ed0675f4c1c6aad1e2bfcf253df2b9be4caabd2edd95d'

New-Item -ItemType Directory -Force -Path $toolRoot | Out-Null
if (-not (Test-Path -LiteralPath $goExe)) {
    Write-Host '正在下载官方便携版 Go 工具链...'
    Invoke-WebRequest -Uri 'https://go.dev/dl/go1.27.1.windows-amd64.zip' -OutFile $archive
    $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash.ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
        throw "Go 工具链校验失败。实际 SHA256: $actualHash"
    }
    Expand-Archive -LiteralPath $archive -DestinationPath $toolRoot -Force
    Remove-Item -LiteralPath $archive -Force
}

Push-Location $projectRoot
try {
    & $goExe test ./...
    if ($LASTEXITCODE -ne 0) { throw "测试失败，退出码：$LASTEXITCODE" }
    New-Item -ItemType Directory -Force -Path (Join-Path $projectRoot 'bin') | Out-Null
    & $goExe build -trimpath -o (Join-Path $projectRoot 'bin\weatherbot.exe') ./cmd/weatherbot
    if ($LASTEXITCODE -ne 0) { throw "构建失败，退出码：$LASTEXITCODE" }
    Write-Host '构建完成：bin\weatherbot.exe'
} finally {
    Pop-Location
}
