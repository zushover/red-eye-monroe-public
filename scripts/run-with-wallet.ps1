param(
    [Parameter(Mandatory = $true)][string]$NodeScript,
    [string[]]$ScriptArguments = @()
)
$ErrorActionPreference = 'Stop'
$securityModule = Join-Path $PSHOME 'Modules\Microsoft.PowerShell.Security\Microsoft.PowerShell.Security.psd1'
if (-not (Test-Path -LiteralPath $securityModule)) { throw 'Windows PowerShell security module is unavailable' }
Import-Module -Name $securityModule -ErrorAction Stop
$projectRoot = Split-Path -Parent $PSScriptRoot
$sdkRoot = [IO.Path]::GetFullPath((Join-Path $projectRoot 'execution-sdk'))
$resolvedScript = [IO.Path]::GetFullPath((Join-Path $sdkRoot $NodeScript))
if (-not $resolvedScript.StartsWith($sdkRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) -or -not (Test-Path -LiteralPath $resolvedScript)) {
    throw 'Invalid execution script path'
}
$secretPath = Join-Path $projectRoot '.secrets\wallet.dpapi.json'
if (-not (Test-Path -LiteralPath $secretPath)) { throw 'Encrypted wallet is not configured. Run setup-wallet.ps1.' }
$record = Get-Content -LiteralPath $secretPath -Raw | ConvertFrom-Json
if ($record.version -ne 1) { throw 'Unsupported encrypted wallet record' }

$relayerSecure = ConvertTo-SecureString $record.relayer_api_key_dpapi
$privateSecure = ConvertTo-SecureString $record.private_key_dpapi
$relayerPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($relayerSecure)
$privatePointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($privateSecure)
$previousTaskHttpProxy = $env:HTTP_PROXY
$previousTaskHttpsProxy = $env:HTTPS_PROXY
try {
    $polymarketEndpoint = [Uri]'https://relayer-v2.polymarket.com'
    $detectedProxy = [System.Net.WebRequest]::DefaultWebProxy.GetProxy($polymarketEndpoint)
    if ($detectedProxy.AbsoluteUri -ne $polymarketEndpoint.AbsoluteUri) {
        $taskProxyUrl = $detectedProxy.GetLeftPart([UriPartial]::Authority)
        $env:HTTP_PROXY = $taskProxyUrl
        $env:HTTPS_PROXY = $taskProxyUrl
    }
    $env:POLYMARKET_WALLET_ADDRESS = [string]$record.profile_wallet
    $env:RELAYER_API_KEY_ADDRESS = [string]$record.relayer_address
    $env:RELAYER_API_KEY = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($relayerPointer)
    $env:SIGNER_PRIVATE_KEY = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($privatePointer)
    & node --use-env-proxy $resolvedScript @ScriptArguments
    $taskExitCode = $LASTEXITCODE
} finally {
    $env:SIGNER_PRIVATE_KEY = $null
    $env:POLYMARKET_WALLET_ADDRESS = $null
    $env:RELAYER_API_KEY = $null
    $env:RELAYER_API_KEY_ADDRESS = $null
    $env:HTTP_PROXY = $previousTaskHttpProxy
    $env:HTTPS_PROXY = $previousTaskHttpsProxy
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($relayerPointer)
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($privatePointer)
    $relayerSecure.Dispose()
    $privateSecure.Dispose()
}
if ($taskExitCode -ne 0) { throw "Wallet task failed with exit code $taskExitCode" }
