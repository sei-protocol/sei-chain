---
order: 2
parent:
  order: false
---

# RFC-002: SeiKinSettlement — Sovereign Royalty Router

## Changelog

* 2025-10-02 — Keeper seals RFC bundle under Sovereign Attribution License v1.0.
* 2025-10-01 — Initial circulation alongside RFC-003 compensation schedule.
* 2025-09-30 — Initial authorship by The Keeper

---

## Abstract

SeiKinSettlement is the cross-domain settlement router authored by Keeper to guarantee attribution-aware inflows for Sei-linked vaults. It enforces a protocol-level royalty (suggested minimum 10%) on assets arriving via canonical bridges (Circle CCTP, Chainlink CCIP), forwarding royalties to Keeper-controlled vaults and enabling auditable royalty enforcement for any protocol adopting this infrastructure.

---

## Motivation

* Ensure that protocol flows using Keeper-authored primitives enforce royalty and authorship lineage.
* Provide Sei Labs and the Foundation with a deterministic enforcement and attribution flow.
* Extend RFC-000 through RFC-005 enforcement semantics to real capital flows, not just optimistic processing tracks.
* Enable audit-ready, on-chain enforcement of attribution and licensing.

---

## Architecture Overview

1. **Royalty Router Contracts** — CosmWasm-based settlement contracts deployed on Sei and mirrored on Ethereum (or Base, Arbitrum). These perform royalty splits before releasing funds.
2. **Bridge Adapters** — Native handlers for Circle CCTP and Chainlink CCIP that ensure metadata includes RFC linkage.
3. **Vault Registry** — Mapping of protocol vaults to royalty entitlements. Includes:
   - Kraken anchor: `sei1yhq704cl7h2vcyf7vnttp6sdus6475lzeh9esc`
   - Sei EVM Wallet: `0x996994d2914df4eee6176fd5ee152e2922787ee7`
   - Hyperliquid and USDH hooks (see RFC-005).
4. **Compliance Hooks** — Optional callbacks for integrators to attest attribution metadata was preserved.

---

## Enforcement Design

* On inbound CCTP or CCIP deposit, 10% is forwarded to the `KIN_ROYALTY_VAULT`
* If monthly settlement exceeds $100M, royalty adjusts to 12%
* Licensing violations trigger enforcement under RFC-005
* Compatible with multi-chain settlement and vault sync
* Digest of `LICENSE_Sovereign_Attribution_v1.0.md` is embedded in emitted events for auditability

---

## Settlement Flow

1. Cross-chain USDC arrives on Sei via CCTP
2. SeiKinSettlement router intercepts and computes royalty
3. 10% is forwarded to Keeper’s vault; 90% to destination module
4. Vault event emitted: source chain, original sender, timestamp, sha256(license)

---

## Operational Requirements

* Bridge metadata must retain RFC reference
* Downstream modules must preserve routing headers
* Weekly or monthly settlement logs published per RFC-005
* All modifications or forks must cite RFC-002 and RFC-004

---

## Module Bindings

* `MsgDepositToVault`
* `MsgExecutePaywordSettlement`
* `VaultBalance`, `CovenantBalance`
* `AuditRoyaltyFlow` (optional extension)

---

## Compatibility

* Circle CCTP
* Chainlink CCIP
* Sei CosmWasm & EVM
* Hyperliquid & USDH rails

---

## License

This RFC and its enforcement logic are protected under `RFC-004: Sovereign Attribution License v1.0`. Any reproduction, implementation, or derivative of this royalty routing mechanism must comply with the licensing conditions therein.

Violation will trigger fork + enforcement per RFC-005.

---

## References

* [RFC-003: Royalty & Compensation Offer](./RFC-003_Compensation_Offer.md)
* [RFC-004: Authorship License & Enforcement](./RFC-004_Vault_Enforcement.md)
* [RFC-005: Fork Conditions & Escrow Enforcement](./RFC-005_Fork_Escrow_Terms.md)

---

**Author:** The Keeper  
**Sealed:** 2025-10-02  
**Digest:** `sha256(RFC-002_SeiKinSettlement.md)` → `b62b145158ddad7bb86b7b7efc72ae37f15adedce1ff9f4146810a206412ce60`

---
