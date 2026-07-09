param(
  [string]$OutputDir = "",
  [string]$Binary = "",
  [string]$ChainId = "aetra-local-onboarding-1",
  [int]$InitialValidatorCount = 3,
  [int]$NewNodeIndex = 3,
  [int]$BaseP2PPort = 33656,
  [int]$BaseRPCPort = 33657,
  [int]$BaseRESTPort = 8317,
  [int]$BaseGRPCPort = 16090,
  [int]$BasePprofPort = 14060,
  [int]$PortStride = 100,
  [string]$NewValidatorKey = "fresh-validator",
  [string]$SelfBond = "100000000naet",
  [string]$FundingAmount = "200000000naet",
  [string]$Fees = "600000000naet",
  [string]$CreateValidatorGas = "500000",
  [int]$TimeoutSeconds = 240,
  [switch]$SkipBuild,
  [switch]$KeepRunning,
  [string]$EvidencePath = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version 2.0
. (Join-Path $PSScriptRoot "common.ps1")

if ($InitialValidatorCount -lt 1) { throw "InitialValidatorCount must be at least 1" }
if ($NewNodeIndex -lt $InitialValidatorCount) { throw "NewNodeIndex must be outside the initial validator set" }

$OutputDir = Resolve-LocalnetPath -Path $OutputDir -DefaultRelativePath ".localnet-validator-onboarding-drill"
$Binary = Resolve-LocalnetPath -Path $Binary -DefaultRelativePath "build\aetrad.exe"
if ([string]::IsNullOrWhiteSpace($EvidencePath)) {
  $EvidencePath = Join-Path $OutputDir "evidence\validator-onboarding-drill.json"
} else {
  $EvidencePath = Resolve-LocalnetPath -Path $EvidencePath -DefaultRelativePath "validator-onboarding-drill.json"
}
Assert-LocalnetWorkspacePath -Path $OutputDir -Purpose "validator onboarding drill output directory"
Assert-LocalnetWorkspacePath -Path (Split-Path -Parent $EvidencePath) -Purpose "validator onboarding drill evidence directory"

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

function Set-OnboardingNodePortsAndPeers {
  param([string]$NodeHome, [int]$Index, [string]$PersistentPeers)

  $ports = Get-DrillPorts -Index $Index
  $configToml = Join-Path $NodeHome "config\config.toml"
  $appToml = Join-Path $NodeHome "config\app.toml"
  $config = Get-Content -Raw -LiteralPath $configToml
  $config = Set-TomlSectionValue -Content $config -Section "p2p" -Key "laddr" -Value "`"tcp://0.0.0.0:$($ports.P2P)`""
  $config = Set-TomlSectionValue -Content $config -Section "p2p" -Key "persistent_peers" -Value "`"$PersistentPeers`""
  $config = Set-TomlSectionValue -Content $config -Section "rpc" -Key "laddr" -Value "`"tcp://0.0.0.0:$($ports.RPC)`""
  $config = Set-TomlSectionValue -Content $config -Section "rpc" -Key "pprof_laddr" -Value "`"localhost:$($ports.Pprof)`""
  $config = Set-TomlSectionValue -Content $config -Section "tx_index" -Key "indexer" -Value "`"kv`""
  Set-Content -LiteralPath $configToml -Value $config

  $app = Get-Content -Raw -LiteralPath $appToml
  $app = Set-TomlSectionValue -Content $app -Section "api" -Key "enable" -Value "true"
  $app = Set-TomlSectionValue -Content $app -Section "api" -Key "address" -Value "`"tcp://0.0.0.0:$($ports.REST)`""
  $app = Set-TomlSectionValue -Content $app -Section "grpc" -Key "enable" -Value "true"
  $app = Set-TomlSectionValue -Content $app -Section "grpc" -Key "address" -Value "`"127.0.0.1:$($ports.GRPC)`""
  $app = $app -replace '(?m)^minimum-gas-prices = ".*"', 'minimum-gas-prices = "0naet"'
  Set-Content -LiteralPath $appToml -Value $app
}

function Start-OnboardingNode {
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

function Stop-OnboardingNode {
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

function Invoke-UnjailCheck {
  param([string]$NodeHome, [int]$RPCPort)

  $node = "tcp://127.0.0.1:$RPCPort"
  $args = @(
    "tx", "slashing", "unjail",
    "--from", $NewValidatorKey,
    "--home", $NodeHome,
    "--chain-id", $ChainId,
    "--keyring-backend", "test",
    "--gas", $CreateValidatorGas,
    "--fees", $Fees,
    "--yes",
    "--broadcast-mode", "sync",
    "--node", $node,
    "--output", "json"
  )
  $previousErrorActionPreference = $ErrorActionPreference
  $ErrorActionPreference = "Continue"
  try {
    $output = & $Binary @args 2>&1
    $exitCode = $LASTEXITCODE
  } finally {
    $ErrorActionPreference = $previousErrorActionPreference
  }
  $text = $output -join "`n"
  if ($exitCode -eq 0) {
    return @{ result = "accepted"; output = $text }
  }
  if ($text -match "not jailed" -or $text -match "validator.*jailed") {
    return @{ result = "reachable_not_jailed"; output = $text }
  }
  throw "unjail flow failed unexpectedly: $text"
}

$primaryPorts = Get-DrillPorts -Index 0
$newPorts = Get-DrillPorts -Index $NewNodeIndex
$primaryRpc = "tcp://127.0.0.1:$($primaryPorts.RPC)"
$summary = [ordered]@{
  chain_id = $ChainId
  output_dir = $OutputDir
  initial_validators = $InitialValidatorCount
  new_node = "node$NewNodeIndex"
  started_at_utc = (Get-Date).ToUniversalTime().ToString("o")
}

try {
  & (Join-Path $PSScriptRoot "stop.ps1") -OutputDir $OutputDir | Out-Null
  & (Join-Path $PSScriptRoot "init.ps1") `
    -OutputDir $OutputDir `
    -Binary $Binary `
    -ValidatorCount $InitialValidatorCount `
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
    -ValidatorCount $InitialValidatorCount `
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
  Wait-LocalnetValidators -ExpectedCount $InitialValidatorCount -RPCPort $primaryPorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  $initialPower = Get-LocalnetTotalVotingPower -RPCPort $primaryPorts.RPC

  $newHome = Get-DrillNodeHome -Index $NewNodeIndex
  New-Item -ItemType Directory -Force -Path $newHome | Out-Null
  Invoke-ExternalChecked -FilePath $Binary -Arguments @("init", "fresh-validator-$NewNodeIndex", "--chain-id", $ChainId, "--home", $newHome) -FailureMessage "fresh validator init failed" | Out-Null
  Copy-Item -LiteralPath (Join-Path (Get-DrillNodeHome -Index 0) "config\genesis.json") -Destination (Join-Path $newHome "config\genesis.json") -Force

  $peerEntries = @()
  for ($i = 0; $i -lt $InitialValidatorCount; $i++) {
    $peerHome = Get-DrillNodeHome -Index $i
    $peerPorts = Get-DrillPorts -Index $i
    $peerEntries += "$(Get-NodeId -NodeHome $peerHome)@127.0.0.1:$($peerPorts.P2P)"
  }
  Set-OnboardingNodePortsAndPeers -NodeHome $newHome -Index $NewNodeIndex -PersistentPeers ($peerEntries -join ",")
  Invoke-ExternalChecked -FilePath $Binary -Arguments @("keys", "add", $NewValidatorKey, "--home", $newHome, "--keyring-backend", "test", "--output", "json") -FailureMessage "fresh validator key creation failed" | Out-Null
  $newAddress = Get-LocalnetKeyAddress -Binary $Binary -NodeHome $newHome -KeyName $NewValidatorKey
  & (Join-Path $PSScriptRoot "fund.ps1") -OutputDir $OutputDir -Binary $Binary -ChainId $ChainId -RPCPort $primaryPorts.RPC -FromHome (Get-DrillNodeHome -Index 0) -FromKey "node0" -Recipients @($newAddress) -Amount $FundingAmount -Fees $Fees -TimeoutSeconds $TimeoutSeconds | Out-Null

  $newPid = Start-OnboardingNode -Index $NewNodeIndex
  Wait-LocalnetRpc -RPCPort $newPorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  Wait-LocalnetPeers -ExpectedMinPeers 1 -RPCPort $newPorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  $newStatus = Invoke-LocalnetRpc -RPCPort $newPorts.RPC -Path "status" -TimeoutSeconds 5
  if ([bool]$newStatus.result.sync_info.catching_up) {
    Wait-LocalnetHeight -TargetHeight ([int64]$newStatus.result.sync_info.latest_block_height + 1) -RPCPort $newPorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  }

  $valPubKey = (Invoke-ExternalChecked -FilePath $Binary -Arguments @("comet", "show-validator", "--home", $newHome) -FailureMessage "show-validator failed") -join "`n"
  $validatorJsonPath = Join-Path $newHome "config\fresh-validator.json"
  $validatorJson = [ordered]@{
    pubkey = ($valPubKey.Trim() | ConvertFrom-Json)
    amount = $SelfBond
    moniker = "fresh-validator-$NewNodeIndex"
    identity = ""
    website = ""
    security = ""
    details = "fresh validator onboarding drill"
    "commission-rate" = "0.05"
    "commission-max-rate" = "0.20"
    "commission-max-change-rate" = "0.01"
    "min-self-delegation" = "1"
  }
  $utf8NoBom = New-Object System.Text.UTF8Encoding $false
  [System.IO.File]::WriteAllText($validatorJsonPath, ($validatorJson | ConvertTo-Json -Depth 20), $utf8NoBom)
  $tx = Send-LocalnetTx -Binary $Binary -Arguments @(
    "tx", "staking", "create-validator",
    $validatorJsonPath,
    "--chain-id", $ChainId,
    "--from", $NewValidatorKey,
    "--home", $newHome,
    "--keyring-backend", "test",
    "--gas", $CreateValidatorGas,
    "--fees", $Fees,
    "--node", $primaryRpc,
    "--broadcast-mode", "sync",
    "--yes",
    "--output", "json"
  ) -RPCPort $primaryPorts.RPC -TimeoutSeconds $TimeoutSeconds

  $newPower = Wait-LocalnetTotalVotingPowerGreater -PreviousPower $initialPower -RPCPort $primaryPorts.RPC -TimeoutSeconds $TimeoutSeconds
  Wait-LocalnetValidators -ExpectedCount ($InitialValidatorCount + 1) -RPCPort $primaryPorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  $stakingValidators = Get-LocalnetStakingValidators -Binary $Binary -RPCPort $primaryPorts.RPC
  $freshValidator = @($stakingValidators | Where-Object { [string]$_.description.moniker -eq "fresh-validator-$NewNodeIndex" }) | Select-Object -First 1
  if (-not $freshValidator) { throw "fresh validator not found in staking validator query" }
  $signingInfos = Get-LocalnetSigningInfos -Binary $Binary -RPCPort $primaryPorts.RPC
  if (@($signingInfos).Count -lt ($InitialValidatorCount + 1)) {
    throw "signing infos did not include the fresh validator set"
  }
  $unjailCheck = Invoke-UnjailCheck -NodeHome $newHome -RPCPort $primaryPorts.RPC

  Stop-OnboardingNode -Index $NewNodeIndex
  $restartPid = Start-OnboardingNode -Index $NewNodeIndex
  Wait-LocalnetRpc -RPCPort $newPorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  Wait-LocalnetHeightIncreasing -RPCPort $newPorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  Wait-LocalnetPeers -ExpectedMinPeers 1 -RPCPort $newPorts.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null

  $summary.operator_address = [string]$freshValidator.operator_address
  $summary.account_address = $newAddress
  $summary.create_validator_txhash = Get-LocalnetTxHash -Tx $tx
  $summary.initial_voting_power = $initialPower
  $summary.final_voting_power = $newPower
  $summary.new_node_rpc = "tcp://127.0.0.1:$($newPorts.RPC)"
  $summary.new_node_pid = $newPid
  $summary.restart_pid = $restartPid
  $summary.peer_count = [int](Invoke-LocalnetRpc -RPCPort $newPorts.RPC -Path "net_info" -TimeoutSeconds 5).result.n_peers
  $summary.signing_info_count = @($signingInfos).Count
  $summary.unjail_flow = $unjailCheck.result
  $summary.completed_at_utc = (Get-Date).ToUniversalTime().ToString("o")
  $summary.result = "passed"

  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $EvidencePath) | Out-Null
  $summary | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $EvidencePath
  Write-Host "Validator onboarding drill passed: operator=$($summary.operator_address) validator_set=$($InitialValidatorCount + 1)"
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
