param(
  [ValidateSet("Smoke", "Full")]
  [string]$Profile = "Smoke",
  [string]$OutputDir = "",
  [string]$Binary = "",
  [string]$ChainId = "aetra-local-1",
  [int]$ValidatorCount = 3,
  [int]$MinHeight = 4,
  [int]$TimeoutSeconds = 120,
  [int]$BaseP2PPort = 26656,
  [int]$BaseRPCPort = 26657,
  [int]$BaseRESTPort = 1317,
  [int]$BaseGRPCPort = 9090,
  [int]$BasePprofPort = 6060,
  [int]$PortStride = 100,
  [string]$TimeoutCommit = "1s",
  [string]$LogLevel = "info",
  [ValidateSet("base", "execution-os-sim", "zones-prototype", "mesh-prototype")]
  [string]$ProfileName = "base",
  [bool]$EnableAPI = $true,
  [bool]$EnableGRPC = $true,
  [bool]$EnableRPC = $true,
  [string]$Node = "",
  # Valid fee must clear the dynamic base fee (DefaultBaseFeeAmount = 0.4 AET =
  # 400000000naet in x/fees/types/fee_model.go). 0.6 AET leaves headroom under
  # the 5 AET hard cap. The old 300000naet default predated the fee model and
  # is now always rejected as underpaid.
  [string]$Fees = "600000000naet",
  [string]$WrongFees = "1000testtoken",
  [string]$DelegationAmount = "5000000naet",
  [switch]$SkipBuild,
  [switch]$KeepLogsOnFailure
)

$ErrorActionPreference = "Stop"

if ($ValidatorCount -lt 2) {
  throw "ValidatorCount must be at least 2 for prototype acceptance"
}

$RepoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))
. (Join-Path $RepoRoot "scripts\localnet\common.ps1")
. (Join-Path $RepoRoot "tests\e2e\prototype_acceptance_helpers.ps1")

$OutputDir = Resolve-LocalnetPath -Path $OutputDir -DefaultRelativePath ".localnet"
$Binary = Resolve-LocalnetPath -Path $Binary -DefaultRelativePath "build\aetrad.exe"
Assert-LocalnetWorkspacePath -Path $OutputDir -Purpose "acceptance localnet output directory"
if (-not $SkipBuild) {
  Assert-LocalnetWorkspacePath -Path (Split-Path $Binary) -Purpose "acceptance binary output directory"
}

$node0Ports = Get-LocalnetPortProfile `
  -Index 0 `
  -BaseP2PPort $BaseP2PPort `
  -BaseRPCPort $BaseRPCPort `
  -BaseRESTPort $BaseRESTPort `
  -BaseGRPCPort $BaseGRPCPort `
  -BasePprofPort $BasePprofPort `
  -PortStride $PortStride
$rpcNode = if ([string]::IsNullOrWhiteSpace($Node)) { "tcp://127.0.0.1:$($node0Ports.RPC)" } else { $Node }

$ctx = [pscustomobject]@{
  RepoRoot       = $RepoRoot
  OutputDir      = $OutputDir
  Binary         = $Binary
  ChainId        = $ChainId
  ValidatorCount = $ValidatorCount
  MinHeight      = $MinHeight
  TimeoutSeconds = $TimeoutSeconds
  BaseP2PPort    = $BaseP2PPort
  BaseRPCPort    = $BaseRPCPort
  BaseRESTPort   = $BaseRESTPort
  BaseGRPCPort   = $BaseGRPCPort
  BasePprofPort  = $BasePprofPort
  PortStride     = $PortStride
  TimeoutCommit  = $TimeoutCommit
  LogLevel       = $LogLevel
  Profile        = $ProfileName
  EnableAPI      = $EnableAPI
  EnableGRPC     = $EnableGRPC
  EnableRPC      = $EnableRPC
  Node0Ports     = $node0Ports
  RpcNode        = $rpcNode
  GrpcAddr       = "127.0.0.1:$($node0Ports.GRPC)"
  RestBase       = "http://127.0.0.1:$($node0Ports.REST)"
  Fees           = $Fees
}

$failure = $null

function New-AcceptanceUserKey {
  param(
    [string]$BinaryPath,
    [string]$NodeHomePath,
    [string]$KeyName
  )

  Invoke-ExternalChecked `
    -FilePath $BinaryPath `
    -Arguments @("keys", "add", $KeyName, "--home", $NodeHomePath, "--keyring-backend", "test", "--no-backup") `
    -FailureMessage "failed to create acceptance user key $KeyName" | Out-Null

  return Get-LocalnetKeyAddress -Binary $BinaryPath -NodeHome $NodeHomePath -KeyName $KeyName
}

Push-Location $RepoRoot
try {
  Write-AcceptanceStep "profile=$Profile validators=$ValidatorCount chain-id=$ChainId"

  & .\scripts\localnet\stop.ps1 -OutputDir $OutputDir

  if (-not $SkipBuild) {
    Write-AcceptanceStep "build aetrad"
    Invoke-AcceptanceBuild -Context $ctx
  } elseif (!(Test-Path -LiteralPath $Binary)) {
    throw "Binary not found at $Binary and -SkipBuild was specified"
  }

  Write-AcceptanceStep "reset/init localnet"
  Invoke-AcceptanceLocalnetScript -Context $ctx -ScriptName "init.ps1" -Extra @{ SkipBuild = $true }
  Invoke-AcceptanceLocalnetScript -Context $ctx -ScriptName "validate-genesis.ps1"

  Write-AcceptanceStep "start validators"
  Invoke-AcceptanceLocalnetScript -Context $ctx -ScriptName "start.ps1" -Extra @{ NoInit = $true }
  Wait-LocalnetRpc -RPCPort $node0Ports.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  $height = Wait-LocalnetHeight -TargetHeight $MinHeight -RPCPort $node0Ports.RPC -TimeoutSeconds $TimeoutSeconds
  Wait-LocalnetValidators -ExpectedCount $ValidatorCount -RPCPort $node0Ports.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  if ($ValidatorCount -gt 1) {
    Wait-LocalnetPeers -ExpectedMinPeers 1 -RPCPort $node0Ports.RPC -TimeoutSeconds $TimeoutSeconds | Out-Null
  }
  & .\scripts\localnet\health.ps1 `
    -OutputDir $OutputDir `
    -ValidatorCount $ValidatorCount `
    -TimeoutSeconds $TimeoutSeconds `
    -BaseP2PPort $BaseP2PPort `
    -BaseRPCPort $BaseRPCPort `
    -BaseRESTPort $BaseRESTPort `
    -BaseGRPCPort $BaseGRPCPort `
    -BasePprofPort $BasePprofPort `
    -PortStride $PortStride `
    -EnableAPI $EnableAPI `
    -EnableGRPC $EnableGRPC `
    -EnableRPC $EnableRPC | Out-Null
  Write-Host "localnet healthy at height $height"

  $node0Home = Join-Path $OutputDir "node0\aetrad"
  $node1Home = Join-Path $OutputDir "node1\aetrad"
  $node0 = Get-LocalnetKeyAddress -Binary $Binary -NodeHome $node0Home -KeyName "node0"
  $node1 = Get-LocalnetKeyAddress -Binary $Binary -NodeHome $node1Home -KeyName "node1"

  Write-AcceptanceStep "query base state"
  $status = Invoke-LocalnetRpc -RPCPort $node0Ports.RPC -Path "/status"
  if ($status.result.node_info.network -ne $ChainId) {
    throw "RPC status network mismatch: $($status.result.node_info.network)"
  }
  $latestBlock = Invoke-AcceptanceQueryCliJson -Context $ctx -Arguments @("query", "block")
  if (-not ($latestBlock.header.height -or $latestBlock.block.header.height)) {
    throw "query block did not return a block height"
  }
  $metadata = Get-LocalnetBankMetadata -Binary $Binary -Denom "naet" -RPCPort $node0Ports.RPC
  Assert-AcceptanceNativeMetadata -Metadata $metadata
  $feesParams = Invoke-AcceptanceQueryGrpcJson -Context $ctx -Arguments @("query", "fees", "params")
  Assert-AcceptanceFeesParams -Params $feesParams.params
  $stakingParams = Get-LocalnetStakingParams -Binary $Binary -RPCPort $node0Ports.RPC
  if ($stakingParams.bond_denom -ne "naet") {
    throw "staking bond denom must be naet, got $($stakingParams.bond_denom)"
  }
  $validators = @(Get-LocalnetStakingValidators -Binary $Binary -RPCPort $node0Ports.RPC)
  if ($validators.Count -ne $ValidatorCount) {
    throw "staking validators query returned $($validators.Count), expected $ValidatorCount"
  }
  foreach ($validator in $validators) {
    Assert-AcceptanceBondedValidator -Validator $validator
  }
  $selectedValidator = @($validators | Sort-Object -Property operator_address | Select-Object -First 1)[0]
  if ($EnableAPI) {
    $restNode = Invoke-AcceptanceRestJson -Context $ctx -Path "/cosmos/base/tendermint/v1beta1/node_info"
    if ($restNode.default_node_info.network -ne $ChainId) {
      throw "REST node_info network mismatch: $($restNode.default_node_info.network)"
    }
  }
  Write-Host "base CLI/gRPC/REST queries passed"

  # Capture native supply before any fee-paying tx. Emission fires only at a
  # ~1-day reward-epoch boundary (not crossed in this short run), so within the
  # acceptance the only supply change is fee burn. Total naet supply must
  # therefore strictly decrease after fee-paying txs -- a live proof that the
  # burn process executes at runtime (full emission cycle is covered by the Go
  # test TestEndBlockerFinalizesEmissionInflationAndBurnAtEpochBoundary).
  $naetSupplyBefore = [decimal]((Get-LocalnetBankSupplyOf -Binary $Binary -Denom "naet" -RPCPort $node0Ports.RPC).amount)

  Write-AcceptanceStep "bank send"
  $node1Before = Get-AcceptanceBalanceAmount -Context $ctx -Address $node1 -Denom "naet"
  Send-LocalnetBankTx `
    -Binary $Binary `
    -FromHome $node0Home `
    -FromKey "node0" `
    -ToAddress $node1 `
    -Amount "1000naet" `
    -Fees $Fees `
    -ChainId $ChainId `
    -RPCPort $node0Ports.RPC `
    -TimeoutSeconds $TimeoutSeconds | Out-Null
  $node1After = Get-AcceptanceBalanceAmount -Context $ctx -Address $node1 -Denom "naet"
  if ($node1After -ne ($node1Before + 1000)) {
    throw "bank send did not increase node1 balance by 1000naet: before=$node1Before after=$node1After"
  }
  Write-Host "bank send updated node1 balance to $($node1After)naet"

  Write-AcceptanceStep "fees policy"
  Send-LocalnetBankTx `
    -Binary $Binary `
    -FromHome $node0Home `
    -FromKey "node0" `
    -ToAddress $node1 `
    -Amount "1naet" `
    -Fees $WrongFees `
    -ChainId $ChainId `
    -RPCPort $node0Ports.RPC `
    -TimeoutSeconds $TimeoutSeconds `
    -ExpectFailure `
    -ExpectedLog "fee denom testtoken not accepted; use naet" | Out-Null
  Write-Host "wrong fee denom rejected"

  Write-AcceptanceStep "PoS query and policy checks"
  $slashingParams = Get-LocalnetSlashingParams -Binary $Binary -RPCPort $node0Ports.RPC
  if ([int64]$slashingParams.signed_blocks_window -le 0) {
    throw "slashing signed_blocks_window must be positive"
  }

  $acceptanceUserKey = "acceptance-user"
  $acceptanceUser = New-AcceptanceUserKey -BinaryPath $Binary -NodeHomePath $node0Home -KeyName $acceptanceUserKey
  # Fund the acceptance user enough to cover a valid dynamic fee (>= 0.4 AET) so
  # the delegate tx clears the ante fee check and reaches the pool-only policy
  # rejection in app/stakingpolicy (a staking msg-server wrapper, i.e. it runs
  # after fee deduction). The old 7000000naet predated the 0.4 AET base fee.
  Send-LocalnetBankTx `
    -Binary $Binary `
    -FromHome $node0Home `
    -FromKey "node0" `
    -ToAddress $acceptanceUser `
    -Amount "2000000000naet" `
    -Fees $Fees `
    -ChainId $ChainId `
    -RPCPort $node0Ports.RPC `
    -TimeoutSeconds $TimeoutSeconds | Out-Null
  Send-AcceptanceTx `
    -Context $ctx `
    -ActionArgs @("tx", "staking", "delegate", $selectedValidator.operator_address, $DelegationAmount) `
    -FromHome $node0Home `
    -FromKey $acceptanceUserKey `
    -Fees $Fees `
    -ExpectFailure `
    -ExpectedLog "direct user delegation to validators is disabled; use official liquid staking pool deposit" | Out-Null
  Write-Host "direct user staking delegation rejected by pool-only policy"
  Write-Host "staking/slashing query surfaces and direct-delegation policy checks passed"

  Write-AcceptanceStep "fee burn reduces supply"
  $naetSupplyAfter = [decimal]((Get-LocalnetBankSupplyOf -Binary $Binary -Denom "naet" -RPCPort $node0Ports.RPC).amount)
  if ($naetSupplyAfter -ge $naetSupplyBefore) {
    throw "fee burn did not reduce total naet supply: before=$naetSupplyBefore after=$naetSupplyAfter"
  }
  Write-Host "fee burn reduced total naet supply: $naetSupplyBefore -> $naetSupplyAfter"

  if ($Profile -eq "Full") {
    Write-AcceptanceStep "full profile restart/health"
    $heightBeforeRestart = Get-LocalnetHeight -RPCPort $node0Ports.RPC
    & .\scripts\localnet\stop.ps1 -OutputDir $OutputDir
    Invoke-AcceptanceLocalnetScript -Context $ctx -ScriptName "start.ps1" -Extra @{ NoInit = $true }
    $restartHeight = Wait-LocalnetHeight -TargetHeight ($heightBeforeRestart + 1) -RPCPort $node0Ports.RPC -TimeoutSeconds $TimeoutSeconds
    & .\scripts\localnet\health.ps1 `
      -OutputDir $OutputDir `
      -ValidatorCount $ValidatorCount `
      -TimeoutSeconds $TimeoutSeconds `
      -BaseP2PPort $BaseP2PPort `
      -BaseRPCPort $BaseRPCPort `
      -BaseRESTPort $BaseRESTPort `
      -BaseGRPCPort $BaseGRPCPort `
      -BasePprofPort $BasePprofPort `
      -PortStride $PortStride `
      -EnableAPI $EnableAPI `
      -EnableGRPC $EnableGRPC `
      -EnableRPC $EnableRPC | Out-Null
    Write-Host "restart preserved chain progress: $heightBeforeRestart->$restartHeight"
  }

  $height = Get-LocalnetHeight -RPCPort $node0Ports.RPC
  Write-Host "prototype acceptance $Profile passed at height $height"
} catch {
  $failure = $_
  Write-Host "prototype acceptance failed: $($failure.Exception.Message)"
  Invoke-AcceptanceDiagnostics -Context $ctx -Reason $Profile
  if (-not $KeepLogsOnFailure) {
    Write-Host "diagnostic bundle collected; pass -KeepLogsOnFailure to preserve localnet output after failure"
  }
  throw
} finally {
  try {
    & .\scripts\localnet\stop.ps1 -OutputDir $OutputDir
    if ($failure -and -not $KeepLogsOnFailure) {
      & .\scripts\localnet\reset.ps1 -OutputDir $OutputDir
    }
  } finally {
    Pop-Location
  }
}
