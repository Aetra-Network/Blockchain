param(
  [string]$OutputDir = "",
  [string]$Binary = "",
  [string]$ChainId = "",
  [string]$GenesisPath = "",
  [string[]]$RpcEndpoints = @(),
  [string]$PreflightEvidenceRoot = "",
  [string]$StateSyncEvidencePath = "",
  [string]$ValidatorOnboardingEvidencePath = "",
  [string[]]$SecurityEvidencePaths = @(),
  [string]$ValidatorDocsPath = "docs\validator-onboarding.md",
  [string]$IncidentDocsPath = "docs\testnet-incident-response.md",
  [string]$PreparationDocsPath = "docs\public-testnet-preparation.md",
  [string]$GatesDocsPath = "docs\public-testnet-production-gates.md",
  [string]$ReadinessReportPath = "scripts\testnet\public-testnet-readiness-report.ps1",
  [switch]$Strict
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version 2.0

$RepoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))

function Resolve-RepoPath {
  param([string]$Path)
  if ([string]::IsNullOrWhiteSpace($Path)) {
    return ""
  }
  if ([System.IO.Path]::IsPathRooted($Path)) {
    return [System.IO.Path]::GetFullPath($Path)
  }
  return [System.IO.Path]::GetFullPath((Join-Path $RepoRoot $Path))
}

function Assert-WorkspacePath {
  param([string]$Path, [string]$Purpose)
  $full = [System.IO.Path]::GetFullPath($Path)
  $root = $RepoRoot.TrimEnd('\', '/')
  if ($full -ne $root -and -not $full.StartsWith($root + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing $Purpose outside repo workspace: $full"
  }
}

function Copy-IfExists {
  param([string]$Source, [string]$Destination)
  if ([string]::IsNullOrWhiteSpace($Source)) {
    return $false
  }
  if (-not (Test-Path -LiteralPath $Source)) {
    return $false
  }
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Destination) | Out-Null
  Copy-Item -LiteralPath $Source -Destination $Destination -Force
  return $true
}

function Copy-DirectoryIfExists {
  param([string]$Source, [string]$Destination)
  if ([string]::IsNullOrWhiteSpace($Source)) {
    return $false
  }
  if (-not (Test-Path -LiteralPath $Source)) {
    return $false
  }
  if (Test-Path -LiteralPath $Destination) {
    Remove-Item -LiteralPath $Destination -Recurse -Force
  }
  New-Item -ItemType Directory -Force -Path $Destination | Out-Null
  Copy-Item -LiteralPath (Join-Path $Source "*") -Destination $Destination -Recurse -Force
  return $true
}

function Read-JsonIfExists {
  param([string]$Path)
  if ([string]::IsNullOrWhiteSpace($Path)) {
    return $null
  }
  if (-not (Test-Path -LiteralPath $Path)) {
    return $null
  }
  return Get-Content -Raw -LiteralPath $Path | ConvertFrom-Json
}

function Write-TextFile {
  param([string]$Path, [string]$Text)
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null
  Set-Content -LiteralPath $Path -Value $Text -Encoding utf8
}

function Get-FileSha256 {
  param([string]$Path)
  if ([string]::IsNullOrWhiteSpace($Path) -or -not (Test-Path -LiteralPath $Path)) {
    return ""
  }
  return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Add-ManifestFile {
  param(
    [System.Collections.Generic.List[object]]$Files,
    [string]$Role,
    [string]$Source,
    [string]$Destination
  )
  $Files.Add([ordered]@{
      role = $Role
      source = $Source
      destination = $Destination
    }) | Out-Null
}

if ([string]::IsNullOrWhiteSpace($OutputDir)) {
  $stamp = (Get-Date).ToUniversalTime().ToString("yyyyMMdd-HHmmss")
  $OutputDir = Join-Path $RepoRoot ".work\launch-evidence\$stamp"
} else {
  $OutputDir = Resolve-RepoPath $OutputDir
}
Assert-WorkspacePath -Path $OutputDir -Purpose "launch evidence bundle"
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

$bundleRoot = Join-Path $OutputDir "bundle"
if (Test-Path -LiteralPath $bundleRoot) {
  Remove-Item -LiteralPath $bundleRoot -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $bundleRoot | Out-Null

$preflightRoot = Resolve-RepoPath $PreflightEvidenceRoot
$stateSyncPath = Resolve-RepoPath $StateSyncEvidencePath
$onboardingPath = Resolve-RepoPath $ValidatorOnboardingEvidencePath
$validatorDocs = Resolve-RepoPath $ValidatorDocsPath
$incidentDocs = Resolve-RepoPath $IncidentDocsPath
$preparationDocs = Resolve-RepoPath $PreparationDocsPath
$gatesDocs = Resolve-RepoPath $GatesDocsPath
$readinessReport = Resolve-RepoPath $ReadinessReportPath

$manifestFiles = [System.Collections.Generic.List[object]]::new()

$preflightManifest = $null
if (-not [string]::IsNullOrWhiteSpace($preflightRoot) -and (Test-Path -LiteralPath $preflightRoot)) {
  Copy-DirectoryIfExists -Source $preflightRoot -Destination (Join-Path $bundleRoot "preflight")
  $preflightManifest = Read-JsonIfExists (Join-Path $preflightRoot "run-summary.json")
  if (-not $preflightManifest) {
    $preflightProfile = Get-ChildItem -LiteralPath $preflightRoot -Directory -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($preflightProfile) {
      $preflightManifest = Read-JsonIfExists (Join-Path $preflightProfile.FullName "profile-manifest.json")
    }
  }
  Add-ManifestFile -Files $manifestFiles -Role "preflight-root" -Source $preflightRoot -Destination "preflight/"
}

$stateSync = $null
if (-not [string]::IsNullOrWhiteSpace($stateSyncPath) -and (Test-Path -LiteralPath $stateSyncPath)) {
  Copy-IfExists -Source $stateSyncPath -Destination (Join-Path $bundleRoot "state-sync\state-sync-drill.json") | Out-Null
  $stateSync = Read-JsonIfExists $stateSyncPath
  Add-ManifestFile -Files $manifestFiles -Role "state-sync-evidence" -Source $stateSyncPath -Destination "state-sync\state-sync-drill.json"
}

$onboarding = $null
if (-not [string]::IsNullOrWhiteSpace($onboardingPath) -and (Test-Path -LiteralPath $onboardingPath)) {
  Copy-IfExists -Source $onboardingPath -Destination (Join-Path $bundleRoot "validator-onboarding\validator-onboarding-drill.json") | Out-Null
  $onboarding = Read-JsonIfExists $onboardingPath
  Add-ManifestFile -Files $manifestFiles -Role "validator-onboarding-evidence" -Source $onboardingPath -Destination "validator-onboarding\validator-onboarding-drill.json"
}

$securityRoots = [System.Collections.Generic.List[string]]::new()
foreach ($path in $SecurityEvidencePaths) {
  $resolved = Resolve-RepoPath $path
  if ([string]::IsNullOrWhiteSpace($resolved) -or -not (Test-Path -LiteralPath $resolved)) {
    continue
  }
  $securityRoots.Add($resolved) | Out-Null
  $name = Split-Path $resolved -Leaf
  $target = Join-Path $bundleRoot ("security\" + $name)
  if (Test-Path -LiteralPath $resolved -PathType Container) {
    Copy-DirectoryIfExists -Source $resolved -Destination $target | Out-Null
  } else {
    Copy-IfExists -Source $resolved -Destination $target | Out-Null
  }
  Add-ManifestFile -Files $manifestFiles -Role "security-evidence" -Source $resolved -Destination ("security\" + $name)
}

Copy-IfExists -Source $validatorDocs -Destination (Join-Path $bundleRoot "docs\validator-onboarding.md") | Out-Null
Copy-IfExists -Source $incidentDocs -Destination (Join-Path $bundleRoot "docs\testnet-incident-response.md") | Out-Null
Copy-IfExists -Source $preparationDocs -Destination (Join-Path $bundleRoot "docs\public-testnet-preparation.md") | Out-Null
Copy-IfExists -Source $gatesDocs -Destination (Join-Path $bundleRoot "docs\public-testnet-production-gates.md") | Out-Null
Copy-IfExists -Source $readinessReport -Destination (Join-Path $bundleRoot "scripts\testnet\public-testnet-readiness-report.ps1") | Out-Null

Add-ManifestFile -Files $manifestFiles -Role "validator-docs" -Source $validatorDocs -Destination "docs\validator-onboarding.md"
Add-ManifestFile -Files $manifestFiles -Role "incident-docs" -Source $incidentDocs -Destination "docs\testnet-incident-response.md"
Add-ManifestFile -Files $manifestFiles -Role "preparation-docs" -Source $preparationDocs -Destination "docs\public-testnet-preparation.md"
Add-ManifestFile -Files $manifestFiles -Role "gates-docs" -Source $gatesDocs -Destination "docs\public-testnet-production-gates.md"
Add-ManifestFile -Files $manifestFiles -Role "readiness-report" -Source $readinessReport -Destination "scripts\testnet\public-testnet-readiness-report.ps1"

$binarySha256 = ""
if (-not [string]::IsNullOrWhiteSpace($Binary) -and (Test-Path -LiteralPath (Resolve-RepoPath $Binary))) {
  $resolvedBinary = Resolve-RepoPath $Binary
  $binarySha256 = Get-FileSha256 $resolvedBinary
  Copy-IfExists -Source $resolvedBinary -Destination (Join-Path $bundleRoot "binary\aetrad.exe") | Out-Null
  Add-ManifestFile -Files $manifestFiles -Role "binary" -Source $resolvedBinary -Destination "binary\aetrad.exe"
}
if ([string]::IsNullOrWhiteSpace($ChainId) -and $preflightManifest) {
  $ChainId = [string]$preflightManifest.chain_id
}
if ([string]::IsNullOrWhiteSpace($ChainId) -and $stateSync) {
  $ChainId = [string]$stateSync.chain_id
}

$genesisPathResolved = Resolve-RepoPath $GenesisPath
$genesisSha256 = ""
if (-not [string]::IsNullOrWhiteSpace($genesisPathResolved) -and (Test-Path -LiteralPath $genesisPathResolved)) {
  $genesisSha256 = Get-FileSha256 $genesisPathResolved
  Copy-IfExists -Source $genesisPathResolved -Destination (Join-Path $bundleRoot "genesis\genesis.json") | Out-Null
  Add-ManifestFile -Files $manifestFiles -Role "genesis" -Source $genesisPathResolved -Destination "genesis\genesis.json"
} elseif ($preflightManifest -and $preflightRoot) {
  $preflightGenesis = Join-Path $preflightRoot "genesis.json"
  if (Test-Path -LiteralPath $preflightGenesis) {
    $genesisSha256 = Get-FileSha256 $preflightGenesis
    Copy-IfExists -Source $preflightGenesis -Destination (Join-Path $bundleRoot "genesis\genesis.json") | Out-Null
    Add-ManifestFile -Files $manifestFiles -Role "genesis" -Source $preflightGenesis -Destination "genesis\genesis.json"
  }
}

$rpcList = @($RpcEndpoints | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
if ($stateSync -and $stateSync.trusted_rpcs) {
  $rpcList = @($stateSync.trusted_rpcs)
}
if ($rpcList.Count -gt 0) {
  Write-TextFile -Path (Join-Path $bundleRoot "rpc\endpoints.txt") -Text (($rpcList -join [System.Environment]::NewLine))
}

$bundleManifest = [ordered]@{
  created_at_utc = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
  output_dir = $OutputDir
  chain_id = $ChainId
  binary_sha256 = $binarySha256
  genesis_sha256 = $genesisSha256
  rpc_endpoints = @($rpcList)
  evidence = [ordered]@{
    preflight = if ($preflightManifest) { $preflightManifest } else { $null }
    state_sync = if ($stateSync) { $stateSync } else { $null }
    validator_onboarding = if ($onboarding) { $onboarding } else { $null }
  }
  docs = [ordered]@{
    validator = $validatorDocs
    incident = $incidentDocs
    preparation = $preparationDocs
    production_gates = $gatesDocs
    readiness_report = $readinessReport
  }
  security_evidence = @($securityRoots)
  files = @($manifestFiles)
}

$bundleManifest | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath (Join-Path $bundleRoot "manifest.json") -Encoding utf8

$readme = @(
  "# Launch Evidence Bundle",
  "",
  "This bundle is an operator-facing evidence pack, not a production-readiness claim.",
  "",
  "Contents:",
  "",
  "- binary checksum",
  "- genesis hash",
  "- chain-id",
  "- RPC endpoints",
  "- snapshot trust data",
  "- validator docs",
  "- incident docs",
  "- security scan evidence",
  "- preflight outputs",
  "",
  "Use `-Strict` to require all mandatory inputs before packaging."
) -join [System.Environment]::NewLine
Write-TextFile -Path (Join-Path $bundleRoot "README.md") -Text $readme

$missing = [System.Collections.Generic.List[string]]::new()
if ([string]::IsNullOrWhiteSpace($ChainId)) { $missing.Add("chain-id") | Out-Null }
if ([string]::IsNullOrWhiteSpace($binarySha256)) { $missing.Add("binary checksum") | Out-Null }
if ([string]::IsNullOrWhiteSpace($genesisSha256)) { $missing.Add("genesis hash") | Out-Null }
if ($rpcList.Count -eq 0) { $missing.Add("RPC endpoints") | Out-Null }
if (-not $preflightManifest) { $missing.Add("preflight outputs") | Out-Null }
if (-not $stateSync) { $missing.Add("snapshot trust data") | Out-Null }
if (-not $onboarding) { $missing.Add("validator docs evidence") | Out-Null }
if ($securityRoots.Count -eq 0) { $missing.Add("security scan evidence") | Out-Null }
if (-not (Test-Path -LiteralPath $validatorDocs)) { $missing.Add("validator docs") | Out-Null }
if (-not (Test-Path -LiteralPath $incidentDocs)) { $missing.Add("incident docs") | Out-Null }

if ($Strict -and $missing.Count -gt 0) {
  throw ("launch evidence bundle is incomplete: " + ($missing -join ", "))
}

Write-Host "Launch evidence bundle written to $bundleRoot"
if ($missing.Count -gt 0) {
  Write-Host ("Incomplete fields: " + ($missing -join ", "))
}
