# Finding Ledger

This file is the centralized index for the current scan pass.

## Confirmed

| ID | Severity | Status | Draft |
|---|---|---|---|
| FD-01 | High | Confirmed | [ante-txadmission-01-reflection-amplifier.md](/C:/Users/Ryzen/Desktop/L1/audit/findings/_drafts/ante-txadmission-01-reflection-amplifier.md) |

## Partially fixed

| ID | Severity | Status | Draft |
|---|---|---|---|
| FD-02 | Medium | Partially fixed (commit e0d7cc15) | [identity-root-01-state-amplifier.md](/C:/Users/Ryzen/Desktop/L1/audit/findings/_drafts/identity-root-01-state-amplifier.md) |

## Non-findings

| Area | Status | Note |
|---|---|---|
| AEZ | No live finding | Fixed-size zone surface; no exploit confirmed in this pass |
| AVM | No live finding | `x/aetravm` is not wired; `x/contracts` is the live runtime |
| RPC/query defaults | Non-security observation | REST API is disabled by default; one pagination mismatch remains operational |
| Economics | Design mismatch | Adaptive inflation band, not a strict 3-5% hard guarantee |

