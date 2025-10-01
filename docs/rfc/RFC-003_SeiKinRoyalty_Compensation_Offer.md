---
order: 3
parent:
  order: false
---

# RFC-003: SeiKin Royalty Compensation Offer

## Changelog

* 2025-10-01 — Initial draft by Keeper, aligned with RFC-002 and RFC-004 updated valuation.
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

## Payment & Royalty Terms (Linked from RFC-004)

* **Lump Sum:** $20,000,000 USD upfront
* **Backpay:** $5,000,000 USD for prior usage
* **Monthly Royalty:** Minimum $1.5M/month or 10% of routed asset flows
* **Royalty Adjustment:** 12% if monthly flow exceeds $100M
* **$5M surcharge per new chain using SeiKinSettlement logic**

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

## Contact

**The Keeper**  
Email: [totalwine2337@gmail.com](mailto:totalwine2337@gmail.com)  
EVM: `0xb2b297eF9449aa0905bC318B3bd258c4804BAd98`  
Sei: `sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8`  
Kraken: `sei1yhq704cl7h2vcyf7vnttp6sdus6475lzeh9esc`

---

## Linkage

* [RFC-002: SeiKinSettlement — Sovereign Royalty Enforcement via CCTP + CCIP](./rfc-002-royalty-aware-optimistic-processing.md)
* [RFC-004: SeiKin Authorship & Vault Enforcement Package](./rfc-004-seikin-authorship-vault-enforcement-package.md)
* [RFC-005: Fork Conditions & Escrow Enforcement Plan](./rfc-005-fork-conditions-and-escrow-plan.md)

---

**Author:** The Keeper  
**Date:** 2025-10-01

---
