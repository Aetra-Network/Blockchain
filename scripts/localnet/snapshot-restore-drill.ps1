param(
  [string]$OutputDir = "",
  [string]$Binary = "",
  [string]$ChainId = "aetra-local-snapshot-restore-1",
  [int]$ValidatorCount = 3,
  [int]$RestoreNodeIndex = 3,
  [int]$BaseP2PPort = 34656,
  [int]$BaseRPCPort = 34657,
  [int]$BaseRESTPort = 9317,
  [int]$BaseGRPCPort = 17090,
  [int]$BasePprofPort = 15060,
  [int]$PortStride = 100,
  [int]$SnapshotBackoffBlocks = 2,
  [int]$MinStableBlocks = 3,
  [int]$SnapshotFormat = 3,
  [int]$TimeoutSeconds = 240,
  [switch]$SkipBuild,
  [switch]$KeepRunning,
  [string]$EvidencePath = ""
)

# Offline snapshot-restore drill. Proves the full operator flow that previously
# panicked with "state.AppHash does not match AppHash after replay":
#   export snapshot -> fresh node home -> snapshots load -> snapshots restore
#   -> comet bootstrap-state -> start -> node syncs past the restore height.
# The missing step in the failed 2026-07-01 drill was `comet bootstrap-state`:
# without it CometBFT state.db stays at genesis (empty AppHash, empty
# validator set) while application.db holds the restored state, so replay
# from genesis panics on the first hash comparison.

$ErrorActionPreference = "Stop"
Set-StrictMode -Version 2.0
. (Join-Path $PSScriptRoot "common.ps1")

if ($ValidatorCount -lt 3) { throw "snapshot-restore drill requires at least 3 validators so two trusted RPCs stay online" }
if ($RestoreNodeIndex -lt $ValidatorCount) { throw "RestoreNodeIndex must be outside the validator set" }
if ($SnapshotBackoffBlocks -lt 1) { throw "SnapshotBackoffBlocks must be at least 1" }
if ($MinStableBlocks -lt 1) { throw "MinStableBlocks must be at least 1" }

$OutputDir = Resolve-LocalnetPath -Path $OutputDir -DefaultRelativePath ".localnet-snapshot-restore-drill"
$Binary = Resolve-LocalnetPath -Path $Binary -DefaultRelativePath "build\aetrad.exe"
if ([string]::IsNullOrWhiteSpace($EvidencePath)) {
  $EvidencePath = Join-Path $OutputDir "evidence\snapshot-restore-drill.json"
} else {
  $EvidencePath = Resolve-LocalnetPath -Path $EvidencePath -DefaultRelativePath "snapshot-restore-drill.json"
}
Assert-LocalnetWorkspacePath -Path $OutputDir -Purpose "snapshot-restore drill output directory"
Assert-LocalnetWorkspacePath -Path (Split-Path -Parent $EvidencePath) -Purpose "snapshot-restore drill evidence directory"

function Get-DrillPorts {
  param([int]$Index)
  return Get-LocalnetPortProfile -Index $Index -BaseP2PPort $BaseP2PPort -BaseRPCPort $BaseRPCPort -BaseRESTPort $BaseRESTPort -BaseGRPCPort $BaseGRPCPort -BasePprofPort $BasePprofPort -PortStride $PortStride
}

function Get-DrillNodeHome {
  param([int]$Index)
  return Join-Path $OutputDir "node$Index\aetrad"
}

function Get-NodeId {
  param([string]$NodeHome)
  $out = Invoke-ExternalChecked -FilePath $Binary -Arguments @("comet", "show-node-id", "--home", $NodeHome) -FailureMessage "show-node-id failed"
  return (($out | Select-Object -Last 1).ToString().Trim())
}

function Start-DrillNode {
  param([int]$Index)

  $pidDir = Join-Path $OutputDir "pids"
  $logDir = Join-Path $OutputDir "logs"
  New-Item -ItemType Directory -Force -Path $pidDir, $logDir | Out-Null
  $nodeHome = Get-DrillNodeHome -Index $Index
  $proc = Start-Process -FilePath $Binary `
    -ArgumentList @("start", "--home", $nodeHome, "--log_level", "info") `
    -RedirectStandardOutput (Join-Path $logDir "node$Index.out.log") `
    -RedirectStandardError (Join-Path $logDir "node$Index.err.log") `
    -WindowStyle Hidden `
    -PassThru
  Set-Content -LiteralPath (Join-Path $pidDir "node$Index.pid") -Value $proc.Id
  return $proc.Id
}

function Stop-DrillNode {
  param([int]$Index)

  $pidPath = Join-Path $OutputDir "pids\node$Index.pid"
  if (-not (Test-Path -LiteralPath $pidPath)) { return }
  $pidValue = [int](Get-Content -Raw -LiteralPath $pidPath)
  $proc = Get-Process -Id $pidValue -ErrorAction SilentlyContinue
  if ($proc) {
    Stop-Process -Id $pidValue -Force -ErrorAction SilentlyContinue
    Wait-Process -Id $pidValue -Timeout 10 -ErrorAction SilentlyContinue
  }
  Remove-Item -LiteralPath $pidPath -Force
}

function Get-BlockHashAtHeight {
  param([int]$RPCPort, [int64]$Height)

  $block = Invoke-LocalnetRpc -RPCPort $RPCPort -Path "block?height=$Height" -TimeoutSeconds 5
  $hash = [string]$block.result.block_id.hash
  if ([string]::IsNullOrWhiteSpace($hash)) {
    throw "could not read block hash for height $Height on RPC $RPCPort"
  }
  return $hash
}

$restorePorts = Get-DrillPorts -Index $RestoreNodeIndex
$summary = [ordered]@{
  chain_id = $ChainId
  output_dir = $OutputDir
  restore_node = "node$RestoreNodeIndex"
  snapshot_format = $SnapshotFormat
  started_at_utc = (Get-Date).ToUniversalTime().ToString("o")
}

try {
  & (Join-Path $PSScriptRoot "stop.ps1") -OutputDir $OutputDir | Out-Null
  & (Join-Path $PSScriptRoot "init.ps1") `
    -OutputDir $OutputDir `
    -Binary $Binary `
    -ValidatorCount $ValidatorCount `
    -ChainId $ChainId `
    -BaseP2PPort $BaseP2PPort `
    -BaseRPCPort $BaseRPCPort `
    -BaseRESTPort $BaseRESTPort `
    -BaseGRPCPort $BaseGRPCPort `
    -BasePprofPort $BasePprofPort `
    -PortStride $PortStride `
    -SkipBuild:$SkipBuild | Out-Null
  & (Join-Path $PSScriptRoot "start.ps1") `
    -OutputDir $OutputDir `
    -Binary $Binary `
    -ValidatorCount $ValidatorCount `
    -ChainId $ChainId `
    -BaseP2PPort $BaseP2PPort `
    -BaseRPCPort $BaseRPCPort `
    -BaseRESTPort $BaseRESTPort `
    -BaseGRPCPort $BaseGRPCPort `
    -BasePprofPort $BasePprofPort `
    -PortStride $PortStride `
    -NoInit `
    -Wait `
    -TimeoutSeconds $TimeoutSeconds | Out-Null

  $primaryPorts = Get-DrillPorts -Index 0
  $secondaryPorts = Get-DrillPorts -Index 1
  Wait-LocalnetValidators -ExpectedCount $ValidatorCount -RPCPort $primaryPorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  $latestHeight = Wait-LocalnetHeight -TargetHeight ($SnapshotBackoffBlocks + 4) -RPCPort $primaryPorts.RPC -TimeoutSeconds $TimeoutSeconds
  $restoreHeight = [int64]$latestHeight - $SnapshotBackoffBlocks
  $trustHash = Get-BlockHashAtHeight -RPCPort $primaryPorts.RPC -Height $restoreHeight

  # Export + dump the snapshot offline from node0, then bring node0 back.
  $archivePath = Join-Path $OutputDir "evidence\aetra-snapshot-$restoreHeight.tar"
  Stop-DrillNode -Index 0
  & (Join-Path $PSScriptRoot "snapshot.ps1") -OutputDir $OutputDir -Binary $Binary -NodeIndex 0 -Height $restoreHeight -ArchivePath $archivePath | Out-Null
  Start-DrillNode -Index 0 | Out-Null
  Wait-LocalnetRpc -RPCPort $primaryPorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  Wait-LocalnetPeers -ExpectedMinPeers 1 -RPCPort $primaryPorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  if (-not (Test-Path -LiteralPath $archivePath)) {
    throw "snapshot archive was not produced: $archivePath"
  }
  $summary.snapshot_height = $restoreHeight
  $summary.snapshot_archive = $archivePath
  $summary.snapshot_sha256 = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()

  # Fresh restore home: init, adopt genesis, wire p2p peers and the
  # [statesync] light-client trust anchors that `comet bootstrap-state` uses.
  $restoreHome = Get-DrillNodeHome -Index $RestoreNodeIndex
  if (Test-Path -LiteralPath $restoreHome) {
    Remove-Item -LiteralPath $restoreHome -Recurse -Force
  }
  New-Item -ItemType Directory -Force -Path $restoreHome | Out-Null
  Invoke-ExternalChecked -FilePath $Binary -Arguments @("init", "snapshot-restore-$RestoreNodeIndex", "--chain-id", $ChainId, "--home", $restoreHome) -FailureMessage "restore node init failed" | Out-Null
  Copy-Item -LiteralPath (Join-Path (Get-DrillNodeHome -Index 0) "config\genesis.json") -Destination (Join-Path $restoreHome "config\genesis.json") -Force

  $peerEntries = @()
  for ($i = 0; $i -lt $ValidatorCount; $i++) {
    $peerPorts = Get-DrillPorts -Index $i
    $peerEntries += "$(Get-NodeId -NodeHome (Get-DrillNodeHome -Index $i))@127.0.0.1:$($peerPorts.P2P)"
  }
  $rpcServers = @("tcp://127.0.0.1:$($primaryPorts.RPC)", "tcp://127.0.0.1:$($secondaryPorts.RPC)")

  $configToml = Join-Path $restoreHome "config\config.toml"
  $config = Get-Content -Raw -LiteralPath $configToml
  $config = Set-TomlSectionValue -Content $config -Section "p2p" -Key "laddr" -Value "`"tcp://0.0.0.0:$($restorePorts.P2P)`""
  $config = Set-TomlSectionValue -Content $config -Section "p2p" -Key "persistent_peers" -Value "`"$($peerEntries -join ',')`""
  $config = Set-TomlSectionValue -Content $config -Section "rpc" -Key "laddr" -Value "`"tcp://0.0.0.0:$($restorePorts.RPC)`""
  $config = Set-TomlSectionValue -Content $config -Section "rpc" -Key "pprof_laddr" -Value "`"localhost:$($restorePorts.Pprof)`""
  # bootstrap-state reads these trust anchors; statesync itself stays disabled
  # because the application state arrives from the offline archive instead.
  $config = Set-TomlSectionValue -Content $config -Section "statesync" -Key "enable" -Value "false"
  $config = Set-TomlSectionValue -Content $config -Section "statesync" -Key "rpc_servers" -Value "`"$($rpcServers -join ',')`""
  $config = Set-TomlSectionValue -Content $config -Section "statesync" -Key "trust_height" -Value "$restoreHeight"
  $config = Set-TomlSectionValue -Content $config -Section "statesync" -Key "trust_hash" -Value "`"$trustHash`""
  $config = Set-TomlSectionValue -Content $config -Section "statesync" -Key "trust_period" -Value "`"168h0m0s`""
  Set-Content -LiteralPath $configToml -Value $config

  $appToml = Join-Path $restoreHome "config\app.toml"
  $app = Get-Content -Raw -LiteralPath $appToml
  $app = Set-TomlSectionValue -Content $app -Section "api" -Key "enable" -Value "false"
  $app = Set-TomlSectionValue -Content $app -Section "grpc" -Key "enable" -Value "true"
  $app = Set-TomlSectionValue -Content $app -Section "grpc" -Key "address" -Value "`"127.0.0.1:$($restorePorts.GRPC)`""
  $app = $app -replace '(?m)^minimum-gas-prices = ".*"', 'minimum-gas-prices = "0naet"'
  Set-Content -LiteralPath $appToml -Value $app

  # The offline restore sequence under test.
  Invoke-ExternalChecked -FilePath $Binary -Arguments @("snapshots", "load", $archivePath, "--home", $restoreHome) -FailureMessage "snapshots load failed" | Out-Null
  Invoke-ExternalChecked -FilePath $Binary -Arguments @("snapshots", "restore", "$restoreHeight", "$SnapshotFormat", "--home", $restoreHome) -FailureMessage "snapshots restore failed" | Out-Null
  Invoke-ExternalChecked -FilePath $Binary -Arguments @("comet", "bootstrap-state", "--height", "$restoreHeight", "--home", $restoreHome) -FailureMessage "comet bootstrap-state failed" | Out-Null
  $summary.bootstrap_state = "completed"

  $restorePid = Start-DrillNode -Index $RestoreNodeIndex
  $summary.restore_pid = $restorePid
  Wait-LocalnetRpc -RPCPort $restorePorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  $syncedHeight = Wait-LocalnetHeight -TargetHeight ($restoreHeight + $MinStableBlocks) -RPCPort $restorePorts.RPC -TimeoutSeconds $TimeoutSeconds
  Wait-LocalnetPeers -ExpectedMinPeers 1 -RPCPort $restorePorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null

  # catching_up flips to false once block sync closes the last gap to the
  # peers; poll for it instead of sampling a single racy status snapshot.
  $settleDeadline = (Get-Date).AddSeconds($TimeoutSeconds)
  $restoreStatus = $null
  while ($true) {
    $restoreStatus = Invoke-LocalnetRpc -RPCPort $restorePorts.RPC -Path "status" -TimeoutSeconds 5
    if (-not [bool]$restoreStatus.result.sync_info.catching_up) {
      break
    }
    if ((Get-Date) -gt $settleDeadline) {
      throw "restore node is still catching up after snapshot restore"
    }
    Start-Sleep -Seconds 2
  }

  # The failure signatures of the broken flow must be absent from the logs.
  # (A benign "Completed ABCI Handshake ... appHash=..." success line is
  # expected — only match the actual replay-failure wording.)
  foreach ($logName in @("node$RestoreNodeIndex.err.log", "node$RestoreNodeIndex.out.log")) {
    $logPath = Join-Path $OutputDir "logs\$logName"
    if (-not (Test-Path -LiteralPath $logPath)) { continue }
    $logText = Get-Content -Raw -LiteralPath $logPath
    if ($logText -match "does not match AppHash" -or $logText -match "AppHash mismatch" -or $logText -match "panic:") {
      throw "restore node log $logName contains an AppHash/panic failure signature"
    }
  }
  # The restored handshake success line is the positive proof we keep.
  $outLog = Get-Content -Raw -LiteralPath (Join-Path $OutputDir "logs\node$RestoreNodeIndex.out.log")
  $handshake = [regex]::Match($outLog, "Completed ABCI Handshake.*appHash=([0-9A-Fa-f]+).*appHeight=(\d+)")
  if ($handshake.Success) {
    $summary.handshake_app_hash = $handshake.Groups[1].Value
    $summary.handshake_app_height = [int64]$handshake.Groups[2].Value
  }

  # Cross-check: restored node must agree with the validators on the app hash.
  $restoreAppHash = [string]$restoreStatus.result.sync_info.latest_app_hash
  $restoreHeightNow = [int64]$restoreStatus.result.sync_info.latest_block_height
  $referenceBlock = Invoke-LocalnetRpc -RPCPort $primaryPorts.RPC -Path "block?height=$restoreHeightNow" -TimeoutSeconds 5
  $referenceAppHash = [string]$referenceBlock.result.block.header.app_hash
  if ([string]::IsNullOrWhiteSpace($restoreAppHash) -or ($restoreAppHash -ne $referenceAppHash)) {
    throw "restored node app hash $restoreAppHash does not match validator app hash $referenceAppHash at height $restoreHeightNow"
  }

  $summary.restored_height = $syncedHeight
  $summary.verified_height = $restoreHeightNow
  $summary.verified_app_hash = $restoreAppHash
  $summary.peer_count = [int](Invoke-LocalnetRpc -RPCPort $restorePorts.RPC -Path "net_info" -TimeoutSeconds 5).result.n_peers
  $summary.completed_at_utc = (Get-Date).ToUniversalTime().ToString("o")
  $summary.result = "passed"

  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $EvidencePath) | Out-Null
  $summary | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $EvidencePath
  Write-Host "Snapshot-restore drill passed: snapshot_height=$restoreHeight verified_height=$restoreHeightNow app_hash=$restoreAppHash"
  Write-Host "Evidence: $EvidencePath"
} catch {
  $summary.result = "failed"
  $summary.error = $_.Exception.Message
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $EvidencePath) | Out-Null
  $summary | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $EvidencePath
  throw
} finally {
  if (-not $KeepRunning) {
    & (Join-Path $PSScriptRoot "stop.ps1") -OutputDir $OutputDir | Out-Null
  }
}
