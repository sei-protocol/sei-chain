# SeiKin RFC Sovereign Attribution Bundle

This bundle contains a curated subset of SeiKin governance artifacts that support sovereign attribution and royalty enforcement. The included RFCs, license, and sealing script provide the materials needed to validate authorship before integrating or forking SeiKin components.

## Contents
- `RFC-002_SeiKinSettlement.md`
- `RFC-003_Authorship_Licensing.md`
- `RFC-004_Vault_Enforcement.md`
- `RFC-005_Fork_Escrow_Terms.md`
- `LICENSE_Sovereign_Attribution`
- `sovereign-seal.sh`

## Usage
1. Review the RFCs to understand the expected operational and legal commitments.
2. Inspect `LICENSE_Sovereign_Attribution` for attribution terms.
3. Run `./sovereign-seal.sh` to generate checksums, optional GPG signatures, and a JSON manifest anchoring authorship metadata.

## Validation
The checksum file (`integrity-checksums.txt`) and manifest (`sovereign-seal.json`) can be distributed with downstream forks. Consumers can verify file hashes to confirm integrity and authorship provenance.
