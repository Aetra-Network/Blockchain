# Third-Party Security Audit Status

No independent third-party security audit has been performed on this codebase.

## What Exists Instead

| Area | Coverage |
|------|----------|
| Internal audit checklists | [Manual Audit Checklist](manual-audit-checklist.md), [Cosmos Security Checklist](cosmos-security-checklist.md) |
| Automated scanners | `govulncheck`, `gosec`, `CodeQL`, `gitleaks`, dependency review (`.github/workflows/security.yml`) |
| Determinism gate | `scripts/security/determinism-gate.ps1` |
| Prototype audit gate | `scripts/security/prototype-audit.ps1` (Fast, Full, Nightly profiles) |
| Module invariants | Registered in `app.AppInvariantRegistry()` and executed in `TestAppRuntimeInvariantsPassDefaultState` |
| Fuzzing | Seed corpus in `x/aetravm/avm/` |
| Adversarial tests | Planned per `docs/security/security-audit-pack.md` |

## Required Before Mainnet

Per `app/params/mainnet_readiness.go`:

- `MainnetReadinessSecurityAudit` ("security_audit_completed") must be `true`.
- `MainnetReadinessCriticalFindingsFixed` ("critical_findings_fixed") must be `true`.

## Tracking

This file will be updated when an independent audit is commissioned, in progress, or completed. Until then, all security evidence is limited to internal and automated checks.

## Local Verification Snapshot

Verified on 2026-06-29 against commit `7afe0d3`:

- `gosec` reported `0` issues at high severity with generated protobuf excluded.
- `gitleaks` passed with the repo allowlist in [`.gitleaks.toml`](</C:/Users/Ryzen/Desktop/L1/.gitleaks.toml>).
- `govulncheck` reported only the triaged GO IDs already listed in `.github/security/govulncheck-triage.txt`.
- `CodeQL` and dependency review remain enforced by [`.github/workflows/security.yml`](</C:/Users/Ryzen/Desktop/L1/.github/workflows/security.yml>) for GitHub-hosted PR and push runs.
