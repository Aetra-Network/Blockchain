# Deployment Pipeline Spec

This document defines the optional deployment attestation pipeline for
Aetralis contracts. It is not consensus behavior and MUST NOT be implemented
as hidden runtime logic.

## 1. Scope

The deployment pipeline MAY run during build, test, release, or pre-deploy
staging. It is versioned independently from the language and ABI, and every
published pipeline result MUST be hash-committed.

Optional pipeline components may include:

- mock wallets;
- static checks;
- safety profile evaluation;
- deployment test execution;
- test attestation generation.

The pipeline MUST be treated as explicit tooling, not as chain consensus.

## 2. Canonical Outputs

If a deployment attestation is produced, the canonical record MUST include:

- code hash;
- ABI hash;
- storage schema hash;
- safety profile hash;
- attestation hash.

The attestation hash MUST commit to the canonical serialized deployment record.
The deployment record MAY also include toolchain version, pipeline version,
environment labels, and test summary hashes, but those fields are informational
only and MUST be versioned explicitly.

## 3. Hashing Rules

- optional pipeline steps MUST be versioned;
- changing the mock wallet set, static checks, safety policy, or attestation
  inputs MUST change the corresponding pipeline hash;
- chain-visible commitments MUST remain separate from local pipeline policy;
- the pipeline MUST never substitute for ABI validation or consensus checks.

## 4. Chain Commitment Boundary

The chain MUST store only the following deployment commitments for a contract:

- code hash;
- ABI hash;
- storage schema hash;
- safety profile hash;
- attestation hash.

Anything else is off-chain deployment metadata or tooling output and MUST NOT
be treated as consensus state.

