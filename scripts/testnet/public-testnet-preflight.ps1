param(
  [ValidateSet("3", "5", "10", "All")]
  [string]$ValidatorProfile = "All",
  [string]$Binary = "",
  [string]$ChainId = "aetra-testnet-preflight-1",
  [int]$TimeoutSeconds = 180,
  [string]$EvidenceRoot = "",
  [switch]$ArchiveEvidence,
  [switch]$SkipBuild,
  [switch]$SkipCosmWasmDisabledCheck
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version 2.0

$RepoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))
. (Join-Path $RepoRoot "scripts\localnet\common.ps1")

function Resolve-PreflightPath {
  param([string]$Path, [string]$DefaultRelativePath)
  if ([System.IO.Path]::IsPathRooted($Path)) {
    return [System.IO.Path]::GetFullPath($Path)
  }
  return Resolve-LocalnetPath -Path $Path -DefaultRelativePath $DefaultRelativePath
}

function Write-PreflightTextFile {
  param([string]$Path, [string]$Text)
  New-Item -ItemType Directory -Force -Path (Split-Path $Path) | Out-Null
  Set-Content -LiteralPath $Path -Value $Text -Encoding utf8
}

function Copy-PreflightDirectory {
  param([string]$Source, [string]$Destination)
  if (-not (Test-Path -LiteralPath $Source)) {
    return
  }
  if (Test-Path -LiteralPath $Destination) {
    Remove-Item -LiteralPath $Destination -Recurse -Force
  }
  New-Item -ItemType Directory -Force -Path $Destination | Out-Null
  Copy-Item -Path (Join-Path $Source "*") -Destination $Destination -Recurse -Force
}

function Get-PreflightBinaryVersionText {
  param([string]$BinaryPath)

  $versionOutput = Invoke-ExternalChecked `
    -FilePath $BinaryPath `
    -Arguments @("version", "--long", "--output", "json") `
    -FailureMessage "failed to query release binary version"
  return (($versionOutput | Out-String).Trim())
}

function Capture-PreflightEvidence {
  param(
    [int]$ProfileValidators,
    [string]$OutputDir,
    [string]$EvidenceDir,
    [string]$BinaryPath,
    [string]$BinaryVersionText,
    [string]$BinarySha256
  )

  New-Item -ItemType Directory -Force -Path $EvidenceDir | Out-Null
  $node0Home = Join-Path $OutputDir "node0\aetrad"
  $genesisPath = Join-Path $node0Home "config\genesis.json"
  if (-not (Test-Path -LiteralPath $genesisPath)) {
    throw "missing genesis evidence file: $genesisPath"
  }

  $genesisText = Get-Content -Raw -LiteralPath $genesisPath
  $genesisDoc = $genesisText | ConvertFrom-Json
  if ($genesisDoc.chain_id -ne $ChainId) {
    throw ("genesis chain-id mismatch for validators={0}: expected {1}, got {2}" -f $ProfileValidators, $ChainId, $genesisDoc.chain_id)
  }

  $genesisHash = (Get-FileHash -LiteralPath $genesisPath -Algorithm SHA256).Hash.ToLowerInvariant()
  Copy-Item -LiteralPath $genesisPath -Destination (Join-Path $EvidenceDir "genesis.json") -Force
  Copy-PreflightDirectory -Source (Join-Path $OutputDir "logs") -Destination (Join-Path $EvidenceDir "logs")

  $binaryInfo = [ordered]@{
    path         = $BinaryPath
    sha256       = $BinarySha256
    version_json = $BinaryVersionText
  }

  $profileManifest = [ordered]@{
    validator_profile = $ProfileValidators
    chain_id          = $ChainId
    genesis_chain_id   = [string]$genesisDoc.chain_id
    genesis_sha256     = $genesisHash
    binary             = $binaryInfo
    output_dir         = $OutputDir
    logs_dir           = (Join-Path $EvidenceDir "logs")
    captured_at_utc    = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
  }

  $profileManifest | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $EvidenceDir "profile-manifest.json") -Encoding utf8
  Write-PreflightTextFile -Path (Join-Path $EvidenceDir "chain-id.txt") -Text $ChainId
  Write-PreflightTextFile -Path (Join-Path $EvidenceDir "binary-sha256.txt") -Text $BinarySha256
  Write-PreflightTextFile -Path (Join-Path $EvidenceDir "genesis-sha256.txt") -Text $genesisHash
  Write-PreflightTextFile -Path (Join-Path $EvidenceDir "binary-version.json") -Text $BinaryVersionText
  $readme = @(
    "# Public Testnet Preflight Evidence",
    "",
    "- validator profile: $ProfileValidators",
    "- chain-id: $ChainId",
    "- binary: $BinaryPath",
    "- binary sha256: $BinarySha256",
    "- genesis sha256: $genesisHash",
    "- output dir: $OutputDir",
    "",
    "Artifacts:",
    "",
    "- binary-version.json",
    "- binary-sha256.txt",
    "- chain-id.txt",
    "- genesis.json",
    "- genesis-sha256.txt",
    "- profile-manifest.json",
    "- logs/"
  ) -join [System.Environment]::NewLine
  Write-PreflightTextFile -Path (Join-Path $EvidenceDir "README.md") -Text $readme
}

$Binary = Resolve-LocalnetPath -Path $Binary -DefaultRelativePath "build\aetrad.exe"
if ([string]::IsNullOrWhiteSpace($EvidenceRoot)) {
  $runStamp = (Get-Date).ToUniversalTime().ToString("yyyyMMdd-HHmmss")
  $EvidenceRoot = ".work\public-testnet-preflight-evidence\$runStamp"
}
$EvidenceRoot = Resolve-PreflightPath -Path $EvidenceRoot -DefaultRelativePath $EvidenceRoot
Assert-LocalnetWorkspacePath -Path $EvidenceRoot -Purpose "public testnet preflight evidence root"
New-Item -ItemType Directory -Force -Path $EvidenceRoot | Out-Null

if (-not $SkipBuild) {
  & (Join-Path $RepoRoot "scripts\build-aetrad.ps1") -Binary $Binary
} elseif (-not (Test-Path -LiteralPath $Binary)) {
  throw "Binary not found at $Binary and -SkipBuild was specified"
}

$binarySha256 = (Get-FileHash -LiteralPath $Binary -Algorithm SHA256).Hash.ToLowerInvariant()
$binaryVersionText = Get-PreflightBinaryVersionText -BinaryPath $Binary
$profiles = if ($ValidatorProfile -eq "All") { @(3, 5, 10) } else { @([int]$ValidatorProfile) }

$profilePorts = @{
  3  = @{ P2P = 29656; RPC = 29657; REST = 4317; GRPC = 12090; Pprof = 9060 }
  5  = @{ P2P = 30656; RPC = 30657; REST = 5317; GRPC = 13090; Pprof = 10060 }
  10 = @{ P2P = 31656; RPC = 31657; REST = 6317; GRPC = 14090; Pprof = 11060 }
}

$runSummaryPath = Join-Path $EvidenceRoot "run-summary.json"
$runManifest = [ordered]@{
  validator_profile = $ValidatorProfile
  chain_id          = $ChainId
  binary            = $Binary
  binary_sha256     = $binarySha256
  binary_version    = $binaryVersionText
  archive_evidence  = [bool]$ArchiveEvidence
  started_at_utc    = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
  status            = "running"
}
$runManifest | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $runSummaryPath -Encoding utf8

$transcriptPath = Join-Path $EvidenceRoot "preflight.log"
$transcriptStarted = $false
try {
  Start-Transcript -LiteralPath $transcriptPath -Force | Out-Null
  $transcriptStarted = $true
} catch {
  Write-Warning ("Unable to start transcript at {0}: {1}" -f $transcriptPath, $_.Exception.Message)
}

if ($transcriptStarted) {
  Write-Host "Evidence root: $EvidenceRoot"
  Write-Host "Transcript: $transcriptPath"
  Write-Host "Binary version: $binaryVersionText"
  Write-Host "Binary sha256: $binarySha256"
}

if (-not $profilePorts.ContainsKey($profiles[0])) {
  throw "Unsupported validator profile: $ValidatorProfile"
}

Push-Location $RepoRoot
try {
  $profileSummaries = @()
  foreach ($validators in $profiles) {
    $outputDir = Resolve-LocalnetPath -Path ".localnet-public-preflight-$validators" -DefaultRelativePath ".localnet-public-preflight-$validators"
    $ports = $profilePorts[$validators]
    $profileEvidenceDir = Join-Path $EvidenceRoot "$validators"
    Write-Host "Running public testnet preflight: validators=$validators output=$outputDir ports=@{$($ports.RPC),$($ports.P2P),$($ports.REST),$($ports.GRPC)}"

    & .\tests\e2e\prototype_acceptance.ps1 `
      -Profile Full `
      -OutputDir $outputDir `
      -Binary $Binary `
      -ChainId $ChainId `
      -ValidatorCount $validators `
      -TimeoutSeconds $TimeoutSeconds `
      -BaseP2PPort $ports.P2P `
      -BaseRPCPort $ports.RPC `
      -BaseRESTPort $ports.REST `
      -BaseGRPCPort $ports.GRPC `
      -BasePprofPort $ports.Pprof `
      -SkipBuild `
      -KeepLogsOnFailure

    if (-not $SkipCosmWasmDisabledCheck) {
      & .\tests\e2e\cosmwasm_smoke.ps1 `
        -Binary $Binary `
        -Node "tcp://127.0.0.1:$($ports.RPC)"
    }

    Capture-PreflightEvidence `
      -ProfileValidators $validators `
      -OutputDir $outputDir `
      -EvidenceDir $profileEvidenceDir `
      -BinaryPath $Binary `
      -BinaryVersionText $binaryVersionText `
      -BinarySha256 $binarySha256

    $profileSummaries += [ordered]@{
      validator_profile = $validators
      output_dir        = $outputDir
      evidence_dir      = $profileEvidenceDir
      status            = "pass"
    }
  }

  $runManifest.status = "pass"
  $runManifest.completed_at_utc = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
  $runManifest.profiles = @($profileSummaries)
  $runManifest | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $runSummaryPath -Encoding utf8
} finally {
  foreach ($validators in $profiles) {
    $outputDir = Resolve-LocalnetPath -Path ".localnet-public-preflight-$validators" -DefaultRelativePath ".localnet-public-preflight-$validators"
    & .\scripts\localnet\stop.ps1 -OutputDir $outputDir
  }
  if ($transcriptStarted) {
    try {
      Stop-Transcript | Out-Null
    } catch {
      Write-Warning "Unable to stop transcript: $($_.Exception.Message)"
    }
  }
  Pop-Location
}

if ($ArchiveEvidence) {
  $archivePath = "$EvidenceRoot.zip"
  if (Test-Path -LiteralPath $archivePath) {
    Remove-Item -LiteralPath $archivePath -Force
  }
  Compress-Archive -Path (Join-Path $EvidenceRoot "*") -DestinationPath $archivePath -Force
  Write-Host "Archived evidence: $archivePath"
}

Write-Host "Public testnet preflight passed for validator profile(s): $($profiles -join ',')"
