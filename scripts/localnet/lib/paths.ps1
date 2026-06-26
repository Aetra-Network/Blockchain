$ErrorActionPreference = "Stop"

function Get-LocalnetRepoRoot {
  $dir = [System.IO.DirectoryInfo]::new($PSScriptRoot)
  while ($null -ne $dir) {
    if (Test-Path -LiteralPath (Join-Path $dir.FullName "go.mod")) {
      return $dir.FullName
    }
    $dir = $dir.Parent
  }
  throw "Could not locate repository root from $PSScriptRoot"
}

function Resolve-LocalnetPath {
  param(
    [string]$Path,
    [string]$DefaultRelativePath
  )

  $repoRoot = Get-LocalnetRepoRoot
  if ([string]::IsNullOrWhiteSpace($Path)) {
    $Path = Join-Path $repoRoot $DefaultRelativePath
  } elseif (-not [System.IO.Path]::IsPathRooted($Path)) {
    $Path = Join-Path $repoRoot $Path
  }

  return [System.IO.Path]::GetFullPath($Path)
}

function ConvertTo-AbsolutePath {
  param([string]$Path)

  if ([System.IO.Path]::IsPathRooted($Path)) {
    return [System.IO.Path]::GetFullPath($Path)
  }
  return [System.IO.Path]::GetFullPath((Join-Path (Get-Location) $Path))
}

function Assert-LocalnetWorkspacePath {
  param(
    [string]$Path,
    [string]$Purpose = "localnet path"
  )

  $repoRoot = (Get-LocalnetRepoRoot).TrimEnd('\', '/')
  $fullPath = [System.IO.Path]::GetFullPath($Path).TrimEnd('\', '/')
  $prefix = $repoRoot + [System.IO.Path]::DirectorySeparatorChar

  if ($fullPath.Equals($repoRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to use repository root as $Purpose`: $fullPath"
  }
  if (-not $fullPath.StartsWith($prefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to use path outside repository as $Purpose`: $fullPath"
  }
}

function Remove-LocalnetDirectory {
  param([string]$OutputDir)

  $resolved = Resolve-LocalnetPath -Path $OutputDir -DefaultRelativePath ".localnet"
  Assert-LocalnetWorkspacePath -Path $resolved -Purpose "delete target"
  if (Test-Path -LiteralPath $resolved) {
    Remove-Item -LiteralPath $resolved -Recurse -Force
  }
}

function Read-LocalnetManifest {
  param([string]$OutputDir)

  $resolved = Resolve-LocalnetPath -Path $OutputDir -DefaultRelativePath ".localnet"
  foreach ($manifestName in @("manifest.json", "profile.json")) {
    $manifestPath = Join-Path $resolved $manifestName
    if (Test-Path -LiteralPath $manifestPath) {
      return Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
    }
  }
  return $null
}

function Get-NodeHome {
  param(
    [string]$OutputDir,
    [int]$Index
  )

  $resolved = Resolve-LocalnetPath -Path $OutputDir -DefaultRelativePath ".localnet"
  return Join-Path $resolved "node$Index\aetrad"
}
