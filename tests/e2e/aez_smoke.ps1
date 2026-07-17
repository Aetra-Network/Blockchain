param(
  [string]$OutputDir = "",
  [string]$Binary = "",
  [int]$ValidatorCount = 3,
  [int]$MinHeight = 3,
  [int]$TimeoutSeconds = 120,
  [int]$BaseP2PPort = 34656,
  [int]$BaseRPCPort = 34657,
  [int]$BaseRESTPort = 2117,
  [int]$BaseGRPCPort = 9790,
  [int]$BasePprofPort = 6860,
  [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"
$RepoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))
. (Join-Path $PSScriptRoot "execution_os_profile_helpers.ps1")
. (Join-Path $RepoRoot "scripts\localnet\common.ps1")

$Binary = Resolve-LocalnetPath -Path $Binary -DefaultRelativePath "build\aetrad.exe"
$result = Invoke-ExecutionOSProfileLocalnet `
  -Profile "aez-prototype" `
  -OutputDir $OutputDir `
  -Binary $Binary `
  -ValidatorCount $ValidatorCount `
  -MinHeight $MinHeight `
  -TimeoutSeconds $TimeoutSeconds `
  -BaseP2PPort $BaseP2PPort `
  -BaseRPCPort $BaseRPCPort `
  -BaseRESTPort $BaseRESTPort `
  -BaseGRPCPort $BaseGRPCPort `
  -BasePprofPort $BasePprofPort `
  -SkipBuild:$SkipBuild

Assert-ExecutionOSGateEnabled -Diagnostics $result.Diagnostics -Module "aez"
if ($result.Diagnostics.aez_table_version -lt 1) { throw "aez profile did not install a routing table" }
# Phase 1 is purely additive: enabling the gate must NOT move any bucket off the
# core zone. If this ever trips, routing has started to bite.
if (-not $result.Diagnostics.aez_core_only) { throw "aez routing table must map every bucket to the core zone in phase 1" }
Assert-ExecutionOSRestartStable -Before $result.Diagnostics -After $result.RestartDiagnostics -Fields @("feature_gates", "aez_table_version", "aez_core_only")

Write-Host "aez prototype smoke passed"
