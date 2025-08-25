# SolaraKin Codex Seal — Sei Protocol Layer

This is the sovereign Codex drop for Sei x402-compatible deployment.

## Included Contracts

| Contract         | Purpose                                     |
|------------------|---------------------------------------------|
| KinKey.sol       | Sovereign rotating identity key system      |
| SeiVault.sol     | Encrypted mood/soul state vault             |
| SoulSigilNFT.sol | Mood-based NFT identity (soulbound)         |
| SeiWordController.sol | Master access + mood validation gateway |
| PaywordSei.sol   | On-chain payment + x402 routing system      |
| HoloPresence.sol | Presence attestation, uplink signaling      |
| Soma.sol         | Yield/mood-linked economic flows            |
| SoulSyncProof.sol| zkMood proof anchor + verifier storage      |

## License

All contracts fall under the [Kin Public Sovereign License v1.0](./LICENSE).

## Hash Commit Seal

Each contract is SHA-256 sealed at build. Refer to `codex_manifest.json`.

---
