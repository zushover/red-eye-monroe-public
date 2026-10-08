param([Parameter(Mandatory = $true)][string]$IntentPath)
$ErrorActionPreference = 'Stop'
$resolvedIntent = [IO.Path]::GetFullPath($IntentPath)
$dataRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot 'data'))
if (-not $resolvedIntent.StartsWith($dataRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Intent must be stored inside the project data directory'
}
& (Join-Path $PSScriptRoot 'scripts\run-with-wallet.ps1') -NodeScript 'executor.mjs' -ScriptArguments @('execute', $resolvedIntent)

