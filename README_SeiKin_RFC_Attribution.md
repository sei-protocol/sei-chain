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

To validate the signature that protects `docs/signatures/integrity-checksums.txt.asc`, first fetch the SeiKin RFC signing key from the project's keys.openpgp.org entry and confirm the fingerprint (`9464 BC09 65B7 2963 0789 764A AA61 DE3B F64D 5D19`) before importing:

```bash
curl -L https://keys.openpgp.org/vks/v1/by-fingerprint/9464BC0965B729630789764AAA61DE3BF64D5D19 \
  -o docs/signatures/keeper-pubkey.asc

gpg --show-keys docs/signatures/keeper-pubkey.asc
gpg --import docs/signatures/keeper-pubkey.asc
gpg --verify docs/signatures/integrity-checksums.txt.asc
```

A `Good signature` message tied to the fingerprint above confirms that the integrity manifest was produced by the expected signer.
