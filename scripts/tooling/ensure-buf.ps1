param(
  [string]$Version = "",
  [string]$ToolDir = "",
  [switch]$ForceInstall
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version 2.0

function Get-RepoRoot {
  return [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))
}

function Resolve-ToolPath {
  param([string]$Path, [string]$DefaultRelativePath)
  $repoRoot = Get-RepoRoot
  if ([string]::IsNullOrWhiteSpace($Path)) {
    $Path = Join-Path $repoRoot (Normalize-ToolRelativePath $DefaultRelativePath)
  } elseif (-not [System.IO.Path]::IsPathRooted($Path)) {
    $Path = Join-Path $repoRoot (Normalize-ToolRelativePath $Path)
  }
  return [System.IO.Path]::GetFullPath($Path)
}

function Normalize-ToolRelativePath {
  param([string]$Path)
  $sep = [string][System.IO.Path]::DirectorySeparatorChar
  return $Path.Replace('\', $sep).Replace('/', $sep)
}

function Get-GoTool {
  $repoRoot = Get-RepoRoot
  $localGo = Join-Path $repoRoot ".work\tools\go1.25.11\go\bin\go.exe"
  if (Test-Path -LiteralPath $localGo) {
    $env:PATH = "$(Split-Path $localGo);$env:PATH"
    return $localGo
  }
  return "go"
}

function Get-BufVersion {
  param([string]$BufPath)

  $previousErrorActionPreference = $ErrorActionPreference
  $ErrorActionPreference = "Continue"
  try {
    $output = & $BufPath --version 2>&1
    $exitCode = $LASTEXITCODE
  } finally {
    $ErrorActionPreference = $previousErrorActionPreference
  }

  if ($exitCode -ne 0) {
    return ""
  }
  return (($output | Out-String).Trim())
}

function Install-Buf {
  param([string]$TargetDir, [string]$Version)

  $repoRoot = Get-RepoRoot
  $go = Get-GoTool
  New-Item -ItemType Directory -Force -Path $TargetDir | Out-Null

  $oldGobin = $env:GOBIN
  try {
    $env:GOBIN = $TargetDir
    & $go install "github.com/bufbuild/buf/cmd/buf@v$Version"
    if ($LASTEXITCODE -ne 0) {
      throw "buf install failed for version $Version"
    }
  } finally {
    $env:GOBIN = $oldGobin
  }

  $bufPath = Join-Path $TargetDir "buf.exe"
  if (-not (Test-Path -LiteralPath $bufPath)) {
    throw "buf install did not create $bufPath"
  }
  return $bufPath
}

$RepoRoot = Get-RepoRoot
if ([string]::IsNullOrWhiteSpace($Version)) {
  $Version = if ($env:BUF_VERSION) { $env:BUF_VERSION } else { "1.70.0" }
}
if ($Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9._-]+)?$') {
  throw "Invalid buf version: $Version"
}

$ToolDir = Resolve-ToolPath -Path $ToolDir -DefaultRelativePath ".work\tools\bin"
$BufPath = Join-Path $ToolDir "buf.exe"

if (-not $ForceInstall -and (Test-Path -LiteralPath $BufPath)) {
  $installedVersion = Get-BufVersion -BufPath $BufPath
  if ($installedVersion -match [regex]::Escape($Version)) {
    Write-Host "Using buf $installedVersion at $BufPath"
    return $BufPath
  }
  Write-Host "Replacing buf $installedVersion at $BufPath with version $Version"
}

$BufPath = Install-Buf -TargetDir $ToolDir -Version $Version
$installedVersion = Get-BufVersion -BufPath $BufPath
if ($installedVersion -notmatch [regex]::Escape($Version)) {
  throw "Installed buf version mismatch: expected $Version, got $installedVersion"
}

Write-Host "Installed buf $installedVersion at $BufPath"
return $BufPath
