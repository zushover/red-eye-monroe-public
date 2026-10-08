$ErrorActionPreference = 'Stop'
$relayerAddress = Read-Host 'Relayer signer address (public 0x address)'
if ($relayerAddress -notmatch '^0x[0-9a-fA-F]{40}$') { throw 'Invalid Relayer signer address' }
$profileWallet = Read-Host 'Profile wallet address (press Enter to auto-discover)'
if ($profileWallet -and $profileWallet -notmatch '^0x[0-9a-fA-F]{40}$') { throw 'Invalid profile wallet address' }
$relayerKey = Read-Host 'Relayer API key (hidden; local only; not saved)' -AsSecureString
$signerSecret = Read-Host 'Exported private key (64 hex characters, optional 0x; hidden; not saved)' -AsSecureString
$keyPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($relayerKey)
$secretPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($signerSecret)
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
    $env:POLYMARKET_WALLET_ADDRESS = $profileWallet
    $env:RELAYER_API_KEY_ADDRESS = $relayerAddress
    $env:RELAYER_API_KEY = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($keyPointer)
    $env:SIGNER_PRIVATE_KEY = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($secretPointer)
    # Node does not consume HTTP(S)_PROXY by default; this host requires it.
    & node --use-env-proxy (Join-Path $PSScriptRoot 'execution-sdk\doctor.mjs')
    if ($LASTEXITCODE -ne 0) {
        Write-Host 'Wallet check did not complete. Follow the specific message above.' -ForegroundColor Yellow
        return
    }
} finally {
    $env:SIGNER_PRIVATE_KEY = $null
    $env:POLYMARKET_WALLET_ADDRESS = $null
    $env:RELAYER_API_KEY = $null
    $env:RELAYER_API_KEY_ADDRESS = $null
    $env:HTTP_PROXY = $previousTaskHttpProxy
    $env:HTTPS_PROXY = $previousTaskHttpsProxy
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($keyPointer)
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($secretPointer)
    $relayerKey.Dispose()
    $signerSecret.Dispose()
}
