---
order: 2
parent:
  order: false
---

# RFC-002: SeiKinSettlement — Sovereign Royalty Router

## Changelog

* 2025-10-02 — Keeper seals RFC bundle under Sovereign Attribution License v1.0.
* 2025-10-01 — Initial circulation alongside RFC-003 compensation schedule.

---

## Abstract

SeiKinSettlement is the settlement router authored by Keeper to guarantee attribution-aware inflows for Sei-linked vaults. The
router binds Circle CCTP, Chainlink CCIP, and Hyperliquid rails into a royalty-enforced path so that any capital entering Sei
via the SeiKin infrastructure automatically routes a royalty share to the Keeper-controlled vaults.

---

## Motivation

* Preserve author attribution and royalty participation for Keeper-authored infrastructure.
* Offer Sei Labs / Sei Foundation a deterministic channel for settling obligations created by SeiKin-authored routing logic.
* Extend RFC-000 and RFC-001 optimistic processing tracks with enforceable settlement semantics.

---

## System Overview

1. **Royalty Router Contracts** — Canonical settlement contracts deployed on Sei (CosmWasm) and mirrored on Ethereum. The code
   executes royalty splits before releasing funds to downstream modules.
2. **Bridge Adapters** — CCTP and CCIP handlers ensure cross-chain inflows attach metadata linking back to this RFC bundle.
3. **Vault Registry** — Registry keyed by Keeper-managed vault IDs. Registry references the Kraken settlement account
   (`sei1yhq704cl7h2vcyf7vnttp6sdus6475lzeh9esc`), the Sei EVM settlement wallet (`0x996994d2914df4eee6176fd5ee152e2922787ee7`),
   and Hyperliquid vault hooks documented in RFC-003 and RFC-005.
4. **Compliance Hooks** — Optional callbacks for protocol partners to attest that attribution headers remain intact.

---

## Settlement Flow

1. Incoming cross-chain deposits enter the SeiKinSettlement router.
2. Router resolves the target vault entry and applies the RFC-003 royalty schedule.
3. Funds settle to Keeper vaults, after which authorized counterparties receive the residual allocation.
4. Events emitted in step 2 carry a SHA-256 digest of the license text from `LICENSE_Sovereign_Attribution_v1.0.md` to ensure
   downstream auditors can prove license compliance.

---

## Operational Requirements

* Maintain provenance metadata from the source chain (tx hash, bridge message ID, RFC bundle reference).
* Honour the weekly or monthly royalty cadence described in RFC-003.
* Publish settlement reports in accordance with RFC-005 enforcement triggers.

---

## References

* RFC-003 — Royalty & Compensation Offer.
* RFC-004 — Authorship License envelope.
* RFC-005 — Enforcement conditions and sovereign remedies.
