param(
  [string]$OutputDir = "",
  [string]$Binary = "",
  [string]$ChainId = "aetra-local-1",
  [int]$RPCPort = 26657,
  [string]$Fees = "1000000naet",
  [int]$TimeoutSeconds = 60,
  [string]$FromKey = "node0",
  [string]$Action = "list",
  [string]$Name = "",
  [string]$Recipient = "",
  [string]$Amount = "1000naet",
  [string]$Denom = "naet",
  [switch]$Json,
  [switch]$Check
)

$ErrorActionPreference = "Stop"

$RepoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))
. (Join-Path $RepoRoot "scripts\localnet\common.ps1")

$OutputDir = Resolve-LocalnetPath -Path $OutputDir -DefaultRelativePath ".localnet"
$Binary = Resolve-LocalnetPath -Path $Binary -DefaultRelativePath "build\aetrad.exe"
$nodeHome = Join-Path $OutputDir "node0\aetrad"
$rpcNode = "tcp://127.0.0.1:$RPCPort"

function Invoke-Cli {
  param([string[]]$Arguments)
  return Invoke-LocalnetCliJson -Binary $Binary -Arguments $Arguments
}

function Invoke-CliRaw {
  param([string[]]$Arguments)
  $prev = $ErrorActionPreference; $ErrorActionPreference = "Continue"
  $output = & $Binary @Arguments 2>&1; $ErrorActionPreference = $prev
  return ($output -join "`n")
}

function Send-Tx {
  param([string]$Label, [string[]]$ActionArgs)
  Write-Host "  $Label"
  $tx = Send-LocalnetTx -Binary $Binary -Arguments ($ActionArgs + @("--from", $FromKey, "--home", $nodeHome, "--chain-id", $ChainId, "--keyring-backend", "test", "--fees", $Fees, "--yes", "--broadcast-mode", "sync", "--node", $rpcNode, "--output", "json")) -RPCPort $RPCPort -TimeoutSeconds $TimeoutSeconds
  Write-Host "  txhash=$(Get-LocalnetTxHash -Tx $tx)"
  return $tx
}

function Get-Balance {
  param([string]$Address, [string]$Denom = "naet")
  $bal = Get-LocalnetBankBalance -Binary $Binary -Address $Address -Denom $Denom -RPCPort $RPCPort
  if (-not $bal.amount) { return [int64]0 }
  return [int64]$bal.amount
}

function Assert-Running {
  try {
    Wait-LocalnetRpc -RPCPort $RPCPort -TimeoutSeconds 5 | Out-Null
  } catch { throw "Localnet not running on RPC $RPCPort" }
}

function List-Keys {
  $keyOutput = Invoke-CliRaw -Arguments @("keys", "list", "--home", $nodeHome, "--keyring-backend", "test")
  $lines = $keyOutput -split "`n"
  $results = @()
  $current = $null
  foreach ($line in $lines) {
    if ($line -match '^-+$') { if ($current) { $results += $current }; $current = $null; continue }
    if ($line -match '^\s*$') { continue }
    if ($line -match '^\S') { if ($current) { $results += $current }; $current = @{Raw = $line }; continue }
    if ($current) { $current.Raw += "`n" + $line }
  }
  if ($current) { $results += $current }

  foreach ($entry in $results) {
    $addr = Invoke-CliRaw -Arguments @("keys", "show", ($entry.Raw -split '\s')[0], "--home", $nodeHome, "--keyring-backend", "test", "-a")
    $addr = $addr.Trim().Split("`n")[-1].Trim()
    $name = ($entry.Raw -split '\s')[0]
    $bal = Get-Balance -Address $addr
    Write-Host ("  ${name}: $addr  balance=$bal naet")
  }
  return $results
}

function Show-Key {
  param([string]$KeyName)
  $addr = Invoke-CliRaw -Arguments @("keys", "show", $KeyName, "--home", $nodeHome, "--keyring-backend", "test", "-a")
  $addr = $addr.Trim().Split("`n")[-1].Trim()
  $valAddr = Invoke-CliRaw -Arguments @("keys", "show", $KeyName, "--home", $nodeHome, "--keyring-backend", "test", "--bech", "val", "-a")
  $valAddr = $valAddr.Trim().Split("`n")[-1].Trim()
  $bal = Get-Balance -Address $addr
  $pubKey = Invoke-CliRaw -Arguments @("keys", "show", $KeyName, "--home", $nodeHome, "--keyring-backend", "test", "-p")
  $pubKey = $pubKey.Trim().Split("`n")[-1].Trim()
  Write-Host "  name:        $KeyName"
  Write-Host "  address:     $addr"
  Write-Host "  valoper:     $valAddr"
  Write-Host "  pubkey:      $pubKey"
  Write-Host "  balance:     $bal naet"
}

if ($Check) {
  Write-Output "Wallet management helper"
  Write-Output "actions: list, show, balance, send, all"
  Write-Output "rpc: $rpcNode home: $nodeHome"
  return
}

Assert-Running

switch ($Action.ToLower()) {
  "list" {
    Write-Host "==> Wallet keys"
    List-Keys
  }
  "show" {
    if ([string]::IsNullOrWhiteSpace($Name)) { throw "provide -Name for show action" }
    Write-Host "==> Key: $Name"
    Show-Key -KeyName $Name
  }
  "balance" {
    if ([string]::IsNullOrWhiteSpace($Name)) { throw "provide -Name or -Recipient for balance action" }
    $addr = if ($Recipient) { $Recipient } else {
      (Invoke-CliRaw -Arguments @("keys", "show", $Name, "--home", $nodeHome, "--keyring-backend", "test", "-a")).Trim().Split("`n")[-1].Trim()
    }
    $bal = Get-Balance -Address $addr -Denom $Denom
    Write-Host ("  ${addr}: $bal $Denom")
  }
  "send" {
    if ([string]::IsNullOrWhiteSpace($Recipient)) { throw "provide -Recipient for send action" }
    $before = Get-Balance -Address $Recipient -Denom ($Amount -replace '^[0-9]+', '')
    $tx = Send-Tx -Label "send $Amount $FromKey -> $Recipient" -ActionArgs @("tx", "bank", "send", $FromKey, $Recipient, $Amount)
    $after = Get-Balance -Address $Recipient -Denom ($Amount -replace '^[0-9]+', '')
    $parts = $Amount -split '(?<=\d)(?=[a-zA-Z])'
    Write-Host "  recipient balance: $before -> $after $($parts[1])"
  }
  "all" {
    Write-Host "==> All wallet info"
    List-Keys
    Write-Host ""
    foreach ($k in @("node0", "node1", "node2")) {
      Show-Key -KeyName $k
      Write-Host ""
    }
  }
  default { throw "Unknown action: $Action. Use: list, show, balance, send, all" }
}
