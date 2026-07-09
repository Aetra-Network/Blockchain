param(
  [string]$OutputDir = "",
  [string]$Binary = "",
  [string]$ChainId = "aetra-local-drill-1",
  [int]$ValidatorCount = 3,
  [int]$TargetNodeIndex = 2,
  [int]$BaseP2PPort = 32656,
  [int]$BaseRPCPort = 32657,
  [int]$BaseRESTPort = 7317,
  [int]$BaseGRPCPort = 15090,
  [int]$BasePprofPort = 13060,
  [int]$PortStride = 100,
  [int]$TrustBackoffBlocks = 2,
  [int]$MinStableBlocks = 3,
  [int]$TimeoutSeconds = 180,
  [switch]$SkipBuild,
  [switch]$KeepRunning,
  [string]$EvidencePath = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version 2.0
. (Join-Path $PSScriptRoot "common.ps1")

if ($ValidatorCount -lt 3) { throw "state-sync drill requires at least 3 validators so two trusted RPCs remain online" }
if ($TargetNodeIndex -lt 0 -or $TargetNodeIndex -ge $ValidatorCount) { throw "target node index out of range" }
if ($TrustBackoffBlocks -lt 1) { throw "TrustBackoffBlocks must be at least 1" }
if ($MinStableBlocks -lt 1) { throw "MinStableBlocks must be at least 1" }

$OutputDir = Resolve-LocalnetPath -Path $OutputDir -DefaultRelativePath ".localnet-state-sync-drill"
$Binary = Resolve-LocalnetPath -Path $Binary -DefaultRelativePath "build\aetrad.exe"
if ([string]::IsNullOrWhiteSpace($EvidencePath)) {
  $EvidencePath = Join-Path $OutputDir "evidence\state-sync-drill.json"
} else {
  $EvidencePath = Resolve-LocalnetPath -Path $EvidencePath -DefaultRelativePath "state-sync-drill.json"
}
Assert-LocalnetWorkspacePath -Path $OutputDir -Purpose "state-sync drill output directory"
Assert-LocalnetWorkspacePath -Path (Split-Path -Parent $EvidencePath) -Purpose "state-sync drill evidence directory"

function Get-DrillRpcPort {
  param([int]$Index)
  return (Get-LocalnetPortProfile -Index $Index -BaseP2PPort $BaseP2PPort -BaseRPCPort $BaseRPCPort -BaseRESTPort $BaseRESTPort -BaseGRPCPort $BaseGRPCPort -BasePprofPort $BasePprofPort -PortStride $PortStride).RPC
}

function Get-DrillRpcUrl {
  param([int]$Index)
  return "tcp://127.0.0.1:$(Get-DrillRpcPort -Index $Index)"
}

function Start-DrillNode {
  param([int]$Index)

  $pidDir = Join-Path $OutputDir "pids"
  $logDir = Join-Path $OutputDir "logs"
  New-Item -ItemType Directory -Force -Path $pidDir, $logDir | Out-Null
  $nodeHome = Get-NodeHome -OutputDir $OutputDir -Index $Index
  $stdout = Join-Path $logDir "node$Index.out.log"
  $stderr = Join-Path $logDir "node$Index.err.log"
  $proc = Start-Process -FilePath $Binary `
    -ArgumentList @("start", "--home", $nodeHome, "--log_level", "info") `
    -RedirectStandardOutput $stdout `
    -RedirectStandardError $stderr `
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

$trustedRpcIndexes = @(0..($ValidatorCount - 1) | Where-Object { $_ -ne $TargetNodeIndex } | Select-Object -First 2)
$trustedRpcPorts = @($trustedRpcIndexes | ForEach-Object { Get-DrillRpcPort -Index $_ })
$trustedRpcUrls = @($trustedRpcIndexes | ForEach-Object { Get-DrillRpcUrl -Index $_ })
$targetRpcPort = Get-DrillRpcPort -Index $TargetNodeIndex
$summary = [ordered]@{
  chain_id = $ChainId
  output_dir = $OutputDir
  target_node = "node$TargetNodeIndex"
  trusted_rpcs = $trustedRpcUrls
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

  foreach ($port in $trustedRpcPorts) {
    Wait-LocalnetRpc -RPCPort $port -TimeoutSeconds $TimeoutSeconds | Out-Null
    Wait-LocalnetPeers -ExpectedMinPeers 1 -RPCPort $port -TimeoutSeconds $TimeoutSeconds | Out-Null
  }
  Wait-LocalnetValidators -ExpectedCount $ValidatorCount -RPCPort $trustedRpcPorts[0] -TimeoutSeconds $TimeoutSeconds | Out-Null
  $latestHeight = Wait-LocalnetHeight -TargetHeight ($TrustBackoffBlocks + 3) -RPCPort $trustedRpcPorts[0] -TimeoutSeconds $TimeoutSeconds
  $trustHeight = [int64]$latestHeight - $TrustBackoffBlocks
  $trustHash = Get-BlockHashAtHeight -RPCPort $trustedRpcPorts[0] -Height $trustHeight
  $snapshotPath = Join-Path $OutputDir "evidence\aetra-state-sync-drill-$trustHeight.tar"
  Stop-DrillNode -Index $trustedRpcIndexes[0]
  & (Join-Path $PSScriptRoot "snapshot.ps1") -OutputDir $OutputDir -Binary $Binary -NodeIndex $trustedRpcIndexes[0] -Height $trustHeight -ArchivePath $snapshotPath | Out-Null
  Start-DrillNode -Index $trustedRpcIndexes[0] | Out-Null
  Wait-LocalnetRpc -RPCPort $trustedRpcPorts[0] -TimeoutSeconds $TimeoutSeconds | Out-Null
  Wait-LocalnetPeers -ExpectedMinPeers 1 -RPCPort $trustedRpcPorts[0] -TimeoutSeconds $TimeoutSeconds | Out-Null
  $snapshotHash = (Get-FileHash -LiteralPath $snapshotPath -Algorithm SHA256).Hash.ToLowerInvariant()

  $summary.trust_height = $trustHeight
  $summary.trust_hash = $trustHash
  $summary.snapshot_archive = $snapshotPath
  $summary.snapshot_sha256 = $snapshotHash

  Stop-DrillNode -Index $TargetNodeIndex
  & (Join-Path $PSScriptRoot "statesync.ps1") `
    -OutputDir $OutputDir `
    -Binary $Binary `
    -TargetNodeIndex $TargetNodeIndex `
    -TrustHeight $trustHeight `
    -TrustHash $trustHash `
    -ResetData | Out-Null
  $joinPid = Start-DrillNode -Index $TargetNodeIndex
  $summary.join_pid = $joinPid

  Wait-LocalnetRpc -RPCPort $targetRpcPort -TimeoutSeconds $TimeoutSeconds | Out-Null
  $joinHeight = Wait-LocalnetHeight -TargetHeight ($trustHeight + 1) -RPCPort $targetRpcPort -TimeoutSeconds $TimeoutSeconds
  Wait-LocalnetHeight -TargetHeight ([int64]$joinHeight + $MinStableBlocks) -RPCPort $targetRpcPort -TimeoutSeconds $TimeoutSeconds | Out-Null
  Wait-LocalnetPeers -ExpectedMinPeers 1 -RPCPort $targetRpcPort -TimeoutSeconds $TimeoutSeconds | Out-Null
  $targetStatus = Invoke-LocalnetRpc -RPCPort $targetRpcPort -Path "status" -TimeoutSeconds 5
  if ([bool]$targetStatus.result.sync_info.catching_up) {
    throw "target node is still catching up after state-sync join"
  }
  $summary.join_height = [int64]$targetStatus.result.sync_info.latest_block_height
  $summary.join_app_hash = [string]$targetStatus.result.sync_info.latest_app_hash
  $summary.catching_up = [bool]$targetStatus.result.sync_info.catching_up
  $summary.peer_count = [int](Invoke-LocalnetRpc -RPCPort $targetRpcPort -Path "net_info" -TimeoutSeconds 5).result.n_peers
  $summary.completed_at_utc = (Get-Date).ToUniversalTime().ToString("o")
  $summary.result = "passed"

  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $EvidencePath) | Out-Null
  $summary | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $EvidencePath
  Write-Host "State-sync drill passed: trust_height=$trustHeight trust_hash=$trustHash target_height=$($summary.join_height)"
  Write-Host "Trusted RPCs: $($trustedRpcUrls -join ',')"
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
