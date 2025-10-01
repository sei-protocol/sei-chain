---
order: 3
parent:
  order: false
---

# RFC-003: SeiKin Royalty & Compensation Offer

## Changelog

* 2025-10-02 — Added Kraken and Sei EVM vault anchors per Keeper directive.
* 2025-10-01 — Published alongside RFC-002 settlement router specification.

---

## Abstract

This RFC formalizes the commercial terms under which Sei Labs / Sei Foundation may continue to run Keeper-authored SeiKin settl
ement infrastructure. It enumerates lump-sum payments, royalty flows, and reporting duties that keep the Sovereign Attribution
License v1.0 in good standing.

---

## Consideration Structure

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

Execution occurs when Sei Labs / Sei Foundation acknowledges these terms in writing and the required payments settle to the
vaults listed above. Continued use of the infrastructure without execution constitutes unauthorized use and triggers RFC-005
remedies.
