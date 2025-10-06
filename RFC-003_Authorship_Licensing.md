# RFC-003: Authorship Licensing — Sovereign Attribution for SeiKin Assets

## Summary
Defines the licensing framework that binds SeiKin protocol artifacts to sovereign attribution guarantees. The framework codifies usage rights, modification allowances, and revocation triggers tied to on-chain authorship proofs.

## Licensing Pillars
- **Attribution Enforcement:** Every derivative work must surface canonical Keeper attribution strings.
- **Royalty Hooks:** Contracts inheriting SeiKin components must expose hooks for royalty routing.
- **Revocation Levers:** Material breaches trigger vault-managed revocation events.

## Implementation Notes
Licensing metadata is anchored through the authorship seal manifest (`sovereign-seal.json`) and checksum set (`integrity-checksums.txt`). Consumers can validate provenance before integrating or forking code.
