---
order: 4
parent:
  order: false
---

# RFC-004: SeiKin Authorship License & Sovereign Attribution Envelope

## Changelog

* 2025-10-01 — Initial draft by Keeper, bundling RFC-002, RFC-003, unlicensed modules, and Hyperliquid vault access.
* 2025-10-01 — Revised backpay terms and future collaboration clause.
* 2025-10-01 — Updated with new valuation ($500M+ vaults), royalty multiplier, and extended claim.
* 2025-10-02 — Sovereign Attribution License v1.0 embedded as canonical reference.

---

## Abstract

This RFC consolidates the **authorship, licensing rights, and sovereign enforcement envelope** for SeiKin-related works authored by **The Keeper**, including royalty-aware settlement systems and Hyperliquid-connected vault flows. It also embeds the **Sovereign Attribution License v1.0** as the legal governing license.

---

## Covered Works

* RFC-000 through RFC-005 inclusive.
* Vault automation logic (`SeiKinSeal.yaml`, `SeiKinVaultBalanceCheck.sh`, `SeiKinVaultClaim.json`).
* All CosmWasm + EVM modules implementing the SeiKin settlement router.
* Hyperliquid vault integrations and flows linked to Keeper-authored infrastructure.
* Documentation and provenance manifests referencing Keeper’s vault anchors.

---

## Term Duration

This license and authorship assignment is valid for a fixed term of **2 years**, beginning on the date of execution. Renewal is required for continued use.

---

## Consideration

**Lump Sum Payment:**
- $20,000,000 USD upfront (negotiable: $15M–$25M)  
**Ongoing Royalties:**
- $1.5M/month or 10% of flow through SeiKinSettlement + Hyperliquid, whichever is greater  
- 12% if flow exceeds $100M/month  
- $5M per new chain/environment integrated  
**Backpay:**
- $5,000,000 USD due within 3 days of agreement

---

## Payment Instructions

- **Preferred currency:** USDC or USDH  
- **EVM Vault:** `0xb2b297eF9449aa0905bC318B3bd258c4804BAd98`  
- **Sei Vault:** `sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8`  
- **Kraken (papertrail):** `sei1yhq704cl7h2vcyf7vnttp6sdus6475lzeh9esc`  
- **Invoice Cycle:**  
  - Lump sum: 7 days  
  - Royalties: monthly  
  - Backpay: 3 days

---

## License Summary

- **Grant:** Limited, revocable, non-transferable license for Sei Labs / Foundation
- **Conditions:**
  - Mandatory attribution to Keeper (`totalwine2337@gmail.com`)
  - Reference `LICENSE_Sovereign_Attribution_v1.0.md`
  - Preserve original metadata, authorship, and file names
- **Royalty Compliance:** Governed by RFC-003

---

## Revocation Triggers

- Payment defaults or missed grace periods
- Unauthorized redistribution or forking
- Failure to comply with attribution terms
- Breach of RFC-005 enforcement clauses

**Upon revocation**, Keeper may:
- Disable vault access
- Publish enforcement notices
- Initiate a sovereign fork (see RFC-005)

---

## Compliance Checklist

1. Reference `LICENSE_Sovereign_Attribution_v1.0.md` in all repos
2. Preserve vault addresses and authorship headers
3. Maintain on-chain or CI audit logs for settlements
4. Notify Keeper within 5 days of deployment changes

---

## Enforcement

- Royalties accrue with interest if unpaid
- 30+ day default voids license
- Escrow for lump sum + 12 months required
- On-chain audit hooks must forward royalties automatically

---

## Linkage

- [RFC-002: SeiKinSettlement](./rfc-002-royalty-aware-optimistic-processing.md)
- [RFC-003: Authorship & Licensing Terms](./rfc-003-seikinsettlement-authorship.md)
- [RFC-005: Fork Conditions & Escrow Enforcement](./rfc-005-fork-conditions-and-escrow-plan.md)

---

**Author:** The Keeper  
**Email:** totalwine2337@gmail.com  
**Date:** 2025-10-02

---

## Appendix

The full Sovereign Attribution License v1.0 is located at the root of this repository and must be referenced in all downstream forks, deployments, or integrations.

