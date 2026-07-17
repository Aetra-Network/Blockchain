$script:LocalnetProfiles = @(
  "base",
  "localnet-3",
  "localnet-4",
  "localnet-5",
  "execution-os-sim",
  "aez-prototype",
  "mesh-prototype"
)

function Assert-LocalnetProfile {
  param([string]$Profile)

  if ($script:LocalnetProfiles -notcontains $Profile) {
    throw "Unknown localnet profile '$Profile'. Allowed profiles: $($script:LocalnetProfiles -join ', ')"
  }
}

function Get-LocalnetProfiles {
  return $script:LocalnetProfiles
}

function Get-LocalnetProfileNodeCount {
  param([string]$Profile)

  switch ($Profile) {
    "base" { return 3 }
    "localnet-3" { return 3 }
    "localnet-4" { return 4 }
    "localnet-5" { return 5 }
    default { return 3 }
  }
}

function New-LocalnetSha256Hex {
  param([string]$Text)

  $sha = [System.Security.Cryptography.SHA256]::Create()
  try {
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($Text)
    $hash = $sha.ComputeHash($bytes)
    return (($hash | ForEach-Object { $_.ToString("x2") }) -join "")
  } finally {
    $sha.Dispose()
  }
}

function Set-PrototypeParamsEnabled {
  param([object]$Params)

  $Params.Enabled = $true
  $Params.TestnetProfile = $true
  $Params.ProductionVersionGate = ""
}

function New-LocalnetAEZProfileParams {
  param($Params)

  # x/aez nests the standard prototype gate under .Prototype (its Params also
  # carry RoutingEpochLength), unlike the flat prototype modules above.
  Set-PrototypeParamsEnabled -Params $Params.Prototype
}

function New-LocalnetRoutingShardProfile {
  return @(
    [pscustomobject]@{ ZoneID = "APPLICATION_ZONE"; ActiveShards = 2 },
    [pscustomobject]@{ ZoneID = "CONTRACT_ZONE"; ActiveShards = 1 },
    [pscustomobject]@{ ZoneID = "FINANCIAL_ZONE"; ActiveShards = 1 },
    [pscustomobject]@{ ZoneID = "IDENTITY_ZONE"; ActiveShards = 1 }
  )
}

function New-LocalnetMeshProfileState {
  return [pscustomobject]@{
    CurrentHeight        = 0
    Params               = [pscustomobject]@{ MaxFinalityAge = 256 }
    Destinations         = @(
      [pscustomobject]@{ ZoneID = "CONTRACT_ZONE"; ShardID = "0:1"; Active = $true },
      [pscustomobject]@{ ZoneID = "FINANCIAL_ZONE"; ShardID = "0:0"; Active = $true }
    )
    FinalizedCommitments = @()
    ReplayMarkers        = @()
    Receipts             = @()
    BounceReceipts       = @()
    RefundReceipts       = @()
  }
}

function Set-LocalnetProfileGenesis {
  param(
    [string]$OutputDir,
    [string]$Profile
  )

  Assert-LocalnetProfile -Profile $Profile
  $resolved = Resolve-LocalnetPath -Path $OutputDir -DefaultRelativePath ".localnet"
  $nodes = Get-LocalnetNodes -OutputDir $resolved

  foreach ($node in $nodes) {
    $genesisPath = Join-Path $node.FullName "aetrad\config\genesis.json"
    $doc = Get-Content -Raw -LiteralPath $genesisPath | ConvertFrom-Json
    $appState = $doc.app_state

    if ($Profile -in @("execution-os-sim", "aez-prototype", "mesh-prototype")) {
      Set-PrototypeParamsEnabled -Params $appState.load.Params
      Set-PrototypeParamsEnabled -Params $appState.routing.Params
      $appState.routing.RoutingEpoch = 1
      $appState.routing.Shards = New-LocalnetRoutingShardProfile
    }

    if ($Profile -in @("aez-prototype", "mesh-prototype")) {
      # The AEZ routing table is NOT rewritten here: genesis already ships all
      # 256 buckets on the core zone, and Phase 1 keeps it that way. Only the
      # feature gate moves.
      New-LocalnetAEZProfileParams -Params $appState.aez.Params
    }

    if ($Profile -eq "mesh-prototype") {
      Set-PrototypeParamsEnabled -Params $appState.mesh.Params
      $appState.mesh.State = New-LocalnetMeshProfileState
    }

    $utf8NoBom = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText($genesisPath, ($doc | ConvertTo-Json -Depth 100), $utf8NoBom)
  }
}

function Write-LocalnetProfileManifest {
  param(
    [string]$OutputDir,
    [string]$Profile,
    [int]$ValidatorCount,
    [string]$ChainId
  )

  Assert-LocalnetProfile -Profile $Profile
  $resolved = Resolve-LocalnetPath -Path $OutputDir -DefaultRelativePath ".localnet"
  $enabled = switch ($Profile) {
    "base" { @() }
    "localnet-3" { @() }
    "localnet-4" { @() }
    "localnet-5" { @() }
    "execution-os-sim" { @("load", "routing") }
    "aez-prototype" { @("load", "routing", "aez") }
    "mesh-prototype" { @("load", "routing", "aez", "mesh") }
  }
  $manifest = [ordered]@{
    profile          = $Profile
    chain_id         = $ChainId
    validator_count  = $ValidatorCount
    enabled_modules  = $enabled
    production_live  = $false
    note             = "Feature-gated prototype profile. No mnemonics, private validator keys, node keys, or keyring material are stored in this manifest."
    created_at_utc   = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
  }
  $manifest | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $resolved "profile.json")
}
