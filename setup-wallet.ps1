$ErrorActionPreference = 'Stop'
$projectRoot = $PSScriptRoot
$confirmation = Read-Host 'Type I HAVE ROTATED to confirm these credentials were never posted in chat'
if ($confirmation -cne 'I HAVE ROTATED') { throw 'Credential setup cancelled' }

$relayerAddress = (Read-Host 'Relayer signer address (public 0x address)').Trim()
$profileWallet = (Read-Host 'Profile wallet address (public 0x address)').Trim()
if ($relayerAddress -notmatch '^0x[0-9a-fA-F]{40}$' -or $profileWallet -notmatch '^0x[0-9a-fA-F]{40}$') {
    throw 'Invalid public wallet address'
}
$relayerKey = Read-Host 'Relayer API key (hidden)' -AsSecureString
$privateKey = Read-Host 'Private key (64 hex characters, optional 0x; hidden)' -AsSecureString

$secretDirectory = Join-Path $projectRoot '.secrets'
$secretPath = Join-Path $secretDirectory 'wallet.dpapi.json'
New-Item -ItemType Directory -Force -Path $secretDirectory | Out-Null
$record = [ordered]@{
    version = 1
    profile_wallet = $profileWallet
    relayer_address = $relayerAddress
    relayer_api_key_dpapi = ConvertFrom-SecureString $relayerKey
    private_key_dpapi = ConvertFrom-SecureString $privateKey
    created_at = (Get-Date).ToUniversalTime().ToString('o')
}
$record | ConvertTo-Json | Set-Content -LiteralPath $secretPath -Encoding utf8
$relayerKey.Dispose()
$privateKey.Dispose()
Write-Host 'Encrypted wallet credentials saved for this Windows user only.' -ForegroundColor Green
& (Join-Path $projectRoot 'scripts\run-with-wallet.ps1') -NodeScript 'doctor.mjs'

