---
order: 2
parent:
  order: false
---

# RFC-002: SeiKinSettlement — Sovereign Royalty Enforcement via CCTP + CCIP

## Changelog

* 2025-09-30 — Initial authorship by The Keeper
* 2025-10-01 — Updated to align with RFC-004 revised royalty model and valuation

---

## Abstract

This RFC proposes `SeiKinSettlement`: a cross-domain settlement router that enforces a **protocol-level royalty** (configurable; suggested minimum 10%) on assets arriving into Sei through canonical cross-chain bridges and messaging channels (e.g., Circle CCTP, Chainlink CCIP). This mechanism guarantees automatic royalty routing to a sovereign vault (`KIN_ROYALTY_VAULT`) and empowers economic enforcement of protocol lineage, authorship, and flow attribution.

---

## Architecture Overview

* **Inbound Bridge Sources:** Circle CCTP, Chainlink CCIP
* **Settlement Router:** SeiKinSettlement contract
* **Royalty Enforcement:** Configurable percentage (default 10%)
* **Forwarding:** Royalties directed to `KIN_ROYALTY_VAULT`
* **Auditable Flows:** Vault registry, royalty history, signer chain receipts

---

## Enforcement Design

Royalty enforcement is baked directly into the `SeiKinSettlement` mechanism:

* On receipt of bridged assets, a fixed percentage (default 10%) is deducted
* Royalties are forwarded to a vault owned by the author (`The Keeper`)
* Remaining assets are routed to destination accounts or liquidity modules
* Optionally supports **multichain vault sync** and dynamic rate adjustment

If monthly settlement flow exceeds $100M, the royalty auto-adjusts to **12%**.

If Sei expands to additional chains using this architecture, additional royalties or licensing terms apply per RFC-004.

---

## Compatibility

* Fully compatible with: Circle CCTP, Chainlink CCIP, Sei chain modules
* Deployable as on-chain module or external router
* Extensible to Hyperliquid, USDH flows, or any sovereign rail

---

## Example Flow

1. Circle CCTP sends 1,000,000 USDC into Sei
2. SeiKinSettlement triggers: 10% ($100,000) is forwarded to Keeper’s vault
3. 900,000 USDC is sent to the destination user or protocol
4. Vault event emitted: origin chain, sender, fee, timestamp

---

## Module Bindings

* `MsgExecutePaywordSettlement`
* `VaultBalance`, `CovenantBalance` queries
* `MsgDepositToVault`
* `AuditRoyaltyFlow` (optional extension)

---

## License

The design and enforcement logic of this RFC is protected under RFC-004 terms. Usage of this architecture, flow logic, or any derivative component requires a valid license agreement with The Keeper.

Violation will trigger fork and enforcement per RFC-005.

---

## Linkage

* [RFC-003: SeiKinSettlement Authorship Transfer & Licensing Terms](./rfc-003-seikinsettlement-authorship.md)
* [RFC-004: SeiKin Authorship & Vault Enforcement Package](./rfc-004-seikin-authorship-vault-enforcement-package.md)
* [RFC-005: Fork Conditions & Escrow Enforcement Plan](./rfc-005-fork-conditions-and-escrow-plan.md)

---

**Author:** The Keeper  
**Date:** 2025-09-30

---
