param(
  [string]$SecurityWorkflow = ".github\workflows\security.yml",
  [string]$ReleaseWorkflow = ".github\workflows\prototype-release.yml"
)

$ErrorActionPreference = "Stop"

$RepoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))

function Resolve-RepoPath {
  param([string]$Path)
  if ([System.IO.Path]::IsPathRooted($Path)) {
    return [System.IO.Path]::GetFullPath($Path)
  }
  return [System.IO.Path]::GetFullPath((Join-Path $RepoRoot $Path))
}

function Assert-Contains {
  param([string]$Text, [string]$Pattern, [string]$Message)
  if ($Text -notmatch $Pattern) { throw $Message }
}

function Assert-NotContains {
  param([string]$Text, [string]$Pattern, [string]$Message)
  if ($Text -match $Pattern) { throw $Message }
}

$securityText = Get-Content -Raw -LiteralPath (Resolve-RepoPath $SecurityWorkflow)
$releaseText = Get-Content -Raw -LiteralPath (Resolve-RepoPath $ReleaseWorkflow)

foreach ($term in @(
    'GITHUB_BASE_REF',
    'triage_file',
    'config_ref',
    '.gitleaks.toml',
    '--log-opts', '--all'
  )) {
  Assert-Contains -Text $securityText -Pattern ([regex]::Escape($term)) -Message "security workflow missing protected triage/config term: $term"
}

foreach ($term in @(
    'fetch-depth: 0',
    'Prototype audit gate',
    '-Strict',
    '-GitleaksConfig'
  )) {
  Assert-Contains -Text $releaseText -Pattern ([regex]::Escape($term)) -Message "release workflow missing guardrail term: $term"
}

Assert-NotContains -Text $releaseText -Pattern ([regex]::Escape('-SkipGitleaksHistory')) -Message "release workflow must not skip gitleaks history"

Write-Host "prototype release workflow guardrails test passed"
