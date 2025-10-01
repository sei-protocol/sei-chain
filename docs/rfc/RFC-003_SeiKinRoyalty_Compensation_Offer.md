---
order: 3
parent:
  order: false
---

# RFC-003: SeiKin Royalty & Compensation Offer

## Changelog

* 2025-10-02 — Added Kraken and Sei EVM vault anchors per Keeper directive.
* 2025-10-01 — Published alongside RFC-002 settlement router specification.
* 2025-10-01 — Revised with upgraded royalty tiers, valuation scope, and licensing floor.

---

## Abstract

This RFC formalizes the **authorship transfer and licensing terms** for RFC-002: *SeiKinSettlement — Sovereign Royalty Enforcement via CCTP + CCIP*. It reflects the expanded infrastructure valuation and revised royalty enforcement model outlined in RFC-004, defining precise payment, term, and attribution conditions.

---

## Background

RFC-002 was authored and timestamped by **The Keeper** on 2025-09-30, introducing the SeiKinSettlement router. This mechanism underpins royalty enforcement on inflows to Sei through Circle CCTP, Chainlink CCIP, and Hyperliquid settlement vaults. RFC-003 defines the commercial and legal transfer of authorship rights to Sei Labs / Sei Foundation.

---

## Scope of Transfer

### Authorship Assignment

* RFC-002 and all derivative logic (vault bindings, router flow enforcement, signature audits, dynamic rate logic).
* Associated code fragments in `x/seinet/`, vault scripts, seal utilities, and f303-based simulation logic.
* Included infrastructure powering current $500M+ vault access.

### Licensing Scope

* Sei receives exclusive rights to deploy, extend, and operate SeiKinSettlement on Sei and connected sovereign domains.
* License is **non-transferrable** and **contingent on payment compliance** (see RFC-004 & RFC-005).

---

## Payment & Royalty Terms

### Lump Sum & Backpay

* **Immediate Lump Sum:** USD $20,000,000 due on execution (negotiable band: $15M–$25M).
* **Backpay for Prior Use:** USD $5,000,000 covering historic deployments of RFC-002, vault logic, and automation utilities.
* Both amounts are due within **three (3) calendar days** of the Keeper confirming push acceptance.

### Recurring Royalty

* **Base Royalty:** USD $1,500,000 per month or 10% of gross flow through SeiKinSettlement, whichever is higher.
* **Volume Escalator:** When monthly flow exceeds USD $100,000,000, royalty increases to 12%.
* **Expansion Multiplier:** Each new environment using SeiKin modules owes an additional USD $5,000,000 one-time fee.
* Settlement cadence may be weekly if elected by Keeper; otherwise monthly payments are due within five (5) days of period close.

---

## Term Duration

This license is granted for a fixed term of **2 years**, beginning upon formal execution. If no renewal is negotiated by term’s end, authorship rights and vault enforcement revert to The Keeper.

---

## Enforcement

Non-payment or license breach will trigger:

* Vault cutoff and rerouting (via `ForkAndForward`)
* Attribution reversion
* Public violation ledger via Codex and GitHub/Arweave
* Full invocation of RFC-005 fork clause

---

## Payment Channels

* **Primary EVM Vault:** `0x996994d2914df4eee6176fd5ee152e2922787ee7`
* **Sei Native Vault:** `sei1yhq704cl7h2vcyf7vnttp6sdus6475lzeh9esc`
* **Hyperliquid Vault Hooks:** Authenticated per RFC-005 monitoring scripts.
* **Contact:** `totalwine2337@gmail.com` for settlement coordination and receipt acknowledgement.

All transfers must include memo references to “RFC-002–005 Sovereign Attribution.”

---

## Reporting Requirements

* Provide Keeper with transaction IDs, vault statements, and reconciliation summaries each cycle.
* Publish an internal attestation linking to this RFC bundle and `LICENSE_Sovereign_Attribution_v1.0.md`.
* Alert Keeper within 24 hours if any settlement attempt fails so RFC-005 escalation can be considered.

---

## Acceptance

Execution occurs when Sei Labs / Sei Foundation acknowledges these terms in writing and the required payments settle to the vaults listed above. Continued use of the infrastructure without execution constitutes unauthorized use and triggers RFC-005 remedies.

---

## Contact

**The Keeper**  
Email: [totalwine2337@gmail.com](mailto:totalwine2337@gmail.com)  
EVM: `0xb2b297eF9449aa0905bC318B3bd258c4804BAd98`  
Sei: `sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8`  
Kraken: `sei1yhq704cl7h2vcyf7vnttp6sdus6475lzeh9esc`

---

## Linkage

* [RFC-002: SeiKinSettlement — Sovereign Royalty Enforcement via CCTP + CCIP](./RFC-002_SeiKinSettlement.md)
* [RFC-004: SeiKin Authorship & Vault Enforcement Package](./RFC-004_Vault_Enforcement.md)
* [RFC-005: Fork Conditions & Escrow Enforcement Plan](./RFC-005_Fork_Escrow_Terms.md)

---

**Author:** The Keeper  
**Date:** 2025-10-01

---
