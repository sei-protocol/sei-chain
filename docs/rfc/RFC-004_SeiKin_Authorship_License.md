---
order: 4
parent:
  order: false
---

# RFC-004: SeiKin Authorship License Envelope

## Changelog

* 2025-10-02 — Sovereign Attribution License v1.0 embedded as canonical reference.
* 2025-10-01 — License envelope drafted to accompany RFC-002 and RFC-003.

---

## Abstract

This RFC documents the authorship license governing Keeper’s SeiKin research assets. The Sovereign Attribution License v1.0 is
attached and any exercise of rights under RFC-002 or RFC-003 must comply with this instrument.

---

## Covered Works

* RFC-000 through RFC-005 inclusive.
* Vault automation logic (`SeiKinSeal.yaml`, `SeiKinVaultBalanceCheck.sh`, `SeiKinVaultClaim.json`).
* Settlement scripts, CosmWasm modules, and EVM shims implementing the SeiKin settlement router.
* Documentation and provenance manifests that reference Keeper’s vault anchors.

---

## License Summary

* **Grant:** Limited, revocable, non-transferable license for Sei Labs / Sei Foundation to operate the covered works solely for
  sovereign deployment on Sei and affiliated environments.
* **Conditions:** Mandatory attribution referencing Keeper, email `totalwine2337@gmail.com`, and the Sovereign Attribution
  License v1.0 file added at repository root.
* **Royalty Compliance:** Payment cadence and reporting obligations are governed by RFC-003.
* **Attribution Integrity:** Filenames, signatures, and metadata must not be altered or removed.

---

## Revocation Triggers

* Missed payments beyond the grace periods defined in RFC-003.
* Attempts to fork, white-label, or redistribute the covered works without written approval.
* Failure to provide attribution or settlement reports.
* Breach of RFC-005 enforcement clauses.

Upon revocation, Keeper may disable vault access, publish enforcement notices, and initiate sovereign forks as described in
RFC-005.

---

## Compliance Checklist

1. Reference `LICENSE_Sovereign_Attribution_v1.0.md` in all derivative documentation.
2. Preserve Keeper’s contact and vault anchors in configuration files.
3. Maintain internal audit trails showing royalty settlements.
4. Notify Keeper within five (5) days of any planned deployment changes impacting the covered works.

---

## Appendix

The full Sovereign Attribution License v1.0 text is stored at repository root for ease of discovery by downstream auditors.
