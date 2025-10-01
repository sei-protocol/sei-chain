# RFC-002: SeiKinSettlement — Sovereign Royalty Enforcement via CCTP + CCIP

## Summary
This RFC outlines the SeiKinSettlement router that enforces a protocol-level royalty when assets arrive on Sei via canonical cross-chain messaging channels. The mechanism forwards a configurable royalty share to the `KIN_ROYALTY_VAULT` while remaining permissionless for integrators.

## Goals
- Guarantee deterministic routing of royalty flows alongside settlement transactions.
- Support CCTP and CCIP bridges without sacrificing latency.
- Provide simple adapter paths for existing settlement contracts.

## Key Requirements
1. The settlement router must collect royalties before forwarding funds to destination contracts.
2. Integrators either target `SeiKinSettlement` directly or use wrappers that call into it.
3. Deployment starts with a limited trusted sender set before progressive decentralization.

## References
- See `docs/rfc/rfc-002-royalty-aware-optimistic-processing.md` for the full technical design and background material.
