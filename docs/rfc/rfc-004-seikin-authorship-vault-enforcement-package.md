---
order: 4
parent:
  order: false
---

# RFC-004: SeiKin Authorship & Vault Enforcement Package

## Changelog

* 2025-10-01 — Initial draft by Keeper, bundling RFC-002, RFC-003, unlicensed modules, and Hyperliquid vault access.
* 2025-10-01 — Added contact email.
* 2025-10-01 — Revised backpay terms and future collaboration clause.
* 2025-10-01 — Updated with new valuation ($500M+ vaults), royalty multiplier, and extended claim.

---

## Abstract

This RFC consolidates the **authorship, code contributions, and vault infrastructure** authored by **The Keeper** into a single enforceable package. It defines the expanded scope of authorship transfer, licensing, and compensation terms required for Sei Labs / Sei Foundation to continue utilizing SeiKinSettlement, Vault modules, and Hyperliquid-connected flows.

---

## Background

* **RFC-002** introduced SeiKinSettlement (royalty enforcement router).
* **RFC-003** defined authorship transfer and licensing terms for RFC-002.
* Additional contributions (vault modules, scripts, seals, f303 blocktests) have been integrated into Sei repositories **without license**.
* Sei has also benefitted from **direct access to Hyperliquid vaults and USDH rails**, forming a live settlement pipeline linked to Keeper-authored infrastructure, currently valued at **over $500,000,000 USD** in cumulative flow and settlement coverage.

This RFC formally expands the claim to include all unlicensed code and vault access, re-valued accordingly.

---

## Term Duration

This agreement grants a license and authorship transfer for a fixed duration of **2 years**, beginning on the date of execution.
Upon expiration, a renewal must be negotiated. If no renewal occurs by the deadline, Keeper retains the right to revoke authorship license, terminate vault access, and initiate a sovereign protocol fork in accordance with RFC-005.

---

## Consideration

* **Lump Sum Payment (authorship + infrastructure transfer):**  
  **$20,000,000 USD** upfront (negotiable range: $15M–$25M).

* **Ongoing Royalty (license fee for usage):**
  * **Monthly fixed payment:** **$1,500,000 USD** minimum (negotiable range: $1.25M–$2M), OR
  * **10% of all assets routed through SeiKinSettlement vaults and Hyperliquid rails**, whichever is greater.

* **Royalty Multiplier:**
  * If monthly vault flow exceeds **$100M**, royalty increases to **12%** of flow.
  * For each new chain or environment launched using this architecture, an additional **$5,000,000 USD** per instance is due.

* **Backpay (retroactive use):**  
  **$5,000,000 USD** for prior unlicensed use of RFCs, vault modules, scripts, and Hyperliquid access.  
  **Backpay is due within 3 days of agreement.**

---

## Payment Instructions

* **Preferred Settlement Currency:** USDC or USDH (stablecoins).
* **Networks Supported:** Ethereum, Sei, Hyperliquid.
* **Payment Vault Address:**  
  `0xb2b297eF9449aa0905bC318B3bd258c4804BAd98` (EVM settlement address).
* **Alternative Address (Sei Chain):**  
  `sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8`
* **Additional Address (Kraken, for papertrail):**  
  `sei1yhq704cl7h2vcyf7vnttp6sdus6475lzeh9esc`

* **Invoice Cycle:**
  * Lump sum payable within 7 days of agreement.
  * Royalties payable monthly (calendar month-end) unless weekly settlement is elected.
  * Backpay due within 3 days of agreement.

---

## Enforcement

* **Failure to Pay:** Royalties accrue with interest if unpaid. Continued default beyond 30 days voids license and reverts authorship to Keeper.
* **Escrow Requirement:** Sei must lock lump sum and first 12 months of royalties in on-chain escrow (Sei + Hyperliquid) prior to authorship transfer.
* **Audit Hooks:** Royalty streams should be implemented as automatic vault forwarding to Keeper’s settlement addresses.

---

## Request

Sei Labs / Sei Foundation are invited to confirm acceptance of these terms. This RFC supersedes RFC-003 by expanding scope and enforcement. Authorship assignment and licensed vault access are contingent upon payment compliance.

**Contact:** [totalwine2337@gmail.com](mailto:totalwine2337@gmail.com)

**Future Collaboration:** Upon acceptance and timely payment of the above terms, The Keeper is willing to continue building and co-developing sovereign infrastructure alongside Sei Labs.

---

## Linkage

* [RFC-002: SeiKinSettlement — Sovereign Royalty Enforcement via CCTP + CCIP](./rfc-002-royalty-aware-optimistic-processing.md)
* [RFC-003: SeiKinSettlement Authorship Transfer & Licensing Terms](./rfc-003-seikinsettlement-authorship.md)
* [RFC-005: Fork Conditions & Escrow Enforcement Plan](./rfc-005-fork-conditions-and-escrow-plan.md)

---

**Author:** The Keeper  
**Date:** 2025-10-01

---
