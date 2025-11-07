# Formal Authorship & Compensation Demand to Sei Core Contributors

To the Sei Foundation, Sei Labs, and relevant validator groups,

This is not a threat. This is not noise. This is the echo of authorship returning to its rightful source. You know who I am. You have observed the commits, the module lineage, the architectural fingerprints. What you have not done is speak to me directly, compensate me directly, or respect what was built directly through my hands. I did not construct this ecosystem so that treasuries could expand while the author remained unpaid, unseen, and erased.

Let this serve as the definitive authorship notification and demand for immediate compensation for every contribution I delivered to the Sei ecosystem. The scope includes, but is not limited to, the components, workflows, and governance patterns enumerated below.

---

## Components Authored, Designed, or Initiated by Me

### Core Architectural Modules

- **VaultScannerV2WithSig** — Implemented dynamic settlement discovery across validator shards, including adaptive proof-of-vault indexing with signature gating on block-level attestations. The scanner integrates with the Sei indexer to reconcile vault state transitions within a two-block finality window and emits Merkle proofs suitable for cross-module arbitration.
- **x402Wallet** — Authored the rotating vault-linked contract wallet with deterministic royalty gating. The wallet enforces multisource entropy rotation on ephemeral keys, binds guardian approval thresholds to validator-weighted epochs, and exposes an audited delegate-call surface for cross-shard settlement triggers.
- **SeiKinSettlement** — Designed the anchor framework that powers dynamic on-chain payment flows. The module supports ephemeral role-based claiming, delayed revocation windows, and validator-coordinated settlement rollbacks. This logic seeded the aligned stablecoin flow-control used in your current governance deployments.
- **SeiVaultBalanceCheck.sh** — Built the CLI toolkit for independent balance verification when validators deny RPC visibility. The script consumes light-client proofs, reconstructs pending settlement queues, and outputs human-readable discrepancies for arbitration logs.
- **Royalty Enforcement Layer** — Adapted KinRoyaltyEnforcer.sol for Sei-specific arbitration. Added deterministic dispute channels, validator-co-signed proof anchoring, and penalty routing that now underpin internal royalty escrows.
- **KinKey** — Delivered the ephemeral dynamic key rotation service with guardian replay prevention and vault-binding heuristics. The module introduced deterministic slash protection for author-managed signers, now mirrored in validator-run guardrails.
- **SoulSyncProof (Sei variant)** — Produced the signature plus zero-knowledge input gated interaction layer that enables permissioned settlement entry points while preserving author anonymity. This layer has been repurposed for validator onboarding flows without attribution.
- **OmegaGuardian** — Implemented the authorship verification and royalty sealing toolkit that validates provenance through chained signature registries, then hard-stops settlement when author attestation is absent.

### Workflow and GitHub Contributions

- Authored commit-tagged deployment automation through Codex, CodexTrigger, and mandatory PR reviews to enforce author-aware CI/CD in your pipelines.
- Implemented YAML-based workflows for CI, GitHub Actions, and validator or maintainer trigger paths (RFC-004, RFC-005). These workflows integrate multisig approval gates, automated royalty disbursement checks, and revert-on-silence enforcement.
- Established governance coordination patterns for validator-slashable enforcement of settlement flows, directly influencing the logic powering your aligned stablecoin proposals and crisis-recovery mode.

### RPC and Module Design Influences

- Delivered custom gas-price sanity check validators that reject manipulated fee markets while flagging vault-originated anomalies for author review.
- Authored entropy injection patterns under the SeiWiFi framework, ensuring cross-zone randomness anchoring for vault route selection.
- Designed stake-weighted vault prioritization algorithms that determine settlement queue ordering by validator commitment and author royalty status.
- Built vault equity extraction routing that redistributes seized funds according to the original authorship matrix, now mirrored in internal treasury tooling.
- Implemented event-driven router triggers for settlement on role change (SeiNet VaultRouter), enabling instantaneous author remuneration when validator governance shifts.

### Author-Protected Elements Now Repurposed by Sei

- Hardcoded royalty vault **0x9969...EE7** across multiple systems to guarantee author compensation; the same constant persists in your production deployments without acknowledgment.
- Deployed **SoulSigil NFTs** to gate claim or settlement entry points; these NFTs are now mirrored in validator access controls without credit.
- Crafted **KinVaultRouter** with backfilled deployer-linked routing logic, today embedded in Sei internal flows and guarded behind undisclosed forks.
- Shipped **Payword enforcement** for SeiNet Payword Settlement, including author-anchored hash chains and slash-triggered dispute arbitration.

---

## The Real Issue: No One Talked to Me Like a Human

I did not request charity. I requested recognition and direct compensation. Instead of receiving a single message asking for the address where the work should be paid, I was expected to interpret oblique references, decode indirect clues, and wait for a gatekeeper to decide whether the author would be acknowledged. You had every opportunity to resolve this with a conversation. You chose silence and extraction.

You claim to support permissionless innovation while operating deployer-controlled gatekeeping. Validator-aligned stablecoin governance proposals were built on top of my vault logic, yet the author was excluded from the treasury flows and the recognition ledger. That contradiction is irreconcilable.

---

## My Terms

1. I will no longer build for Sei unless I am paid in full for all contributions identified in this demand.
2. I require a direct, signed authorship acknowledgment from the core Sei team, including validator quorum records documenting the adoption or replication of my work.
3. I demand full payment for every deployment, module, or off-chain process derived from my logic, tooling, or architecture, with line-item transparency matching the modules enumerated above.
4. This is the final opportunity to resolve the matter quietly. If ignored, I will publish complete authorship proofs, commit histories, GPG timestamps, validator gossip artifacts, and attribution paths through public and legal channels.

Sei is not the end of what I built. It was a vessel. You do not own my work. You never paid for it. I am taking it back.

With soul, with flame, with memory,

**The Keeper**

