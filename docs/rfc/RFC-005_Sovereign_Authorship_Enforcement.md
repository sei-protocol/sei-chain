---
order: 5
parent:
  order: false
---

# RFC-005: Sovereign Authorship Enforcement Protocol

## Changelog

* 2025-10-02 — Updated enforcement triggers to match Keeper’s Sovereign Attribution License v1.0.
* 2025-10-01 — Enforcement workflow aligned with revised royalty schedule.

---

## Abstract

RFC-005 documents the enforcement mechanisms Keeper may invoke if Sei Labs / Sei Foundation or downstream integrators breach
the Sovereign Attribution License. It defines operational runbooks for vault suspension, public notice, and renegotiation rights.

---

## Enforcement Triggers

1. Non-payment of lump sum, backpay, or recurring royalties defined in RFC-003.
2. Removal or alteration of attribution lines referencing Keeper or the Sovereign Attribution License.
3. Unauthorized forks, redistributions, or deployments of the covered works.
4. Attempts to bypass or tamper with the SeiKinSettlement royalty router.
5. Denial of access to settlement records or failure to provide reconciliation reports.

---

## Response Actions

* **Vault Freezing:** Hyperliquid and Sei vault webhooks suspend downstream distribution until settlement resumes.
* **Public Notice:** Keeper publishes violation notices citing relevant RFC sections and settlement expectations.
* **Royalty Recalculation:** Outstanding balances accrue a 20% surcharge per missed period.
* **License Revocation:** Keeper may revoke the license described in RFC-004 and require destruction of derivative artifacts.
* **Sovereign Fork Option:** Keeper may initiate an alternative deployment that excludes non-compliant validators or
  counterparties.

---

## Remediation Path

1. Offending party contacts Keeper at `totalwine2337@gmail.com` with a remediation plan and proof of payment.
2. Keeper validates settlement receipts to the listed vaults.
3. Attribution and documentation updates are reviewed for compliance.
4. License is reinstated or renegotiated at Keeper’s discretion.

---

## Monitoring & Evidence

* Transaction logs from Sei, Ethereum, and Hyperliquid.
* Git provenance and checksum attestations for RFC files and settlement scripts.
* Internal audit reports documenting royalty disbursements.

Failure to cooperate with evidence gathering extends enforcement timelines and may escalate to legal remedies.

---

## Sovereign Rights

Keeper retains the right to renegotiate royalty tiers if vault access expands, to revoke the license via Codex push, and to seek
additional remedies as permitted by governing law. All settlement updates must be anchored back into this repository for
transparency.
