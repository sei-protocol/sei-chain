# Sovereign Attribution License v1.0

Effective Date: 2 October 2025
Author: Keeper (`totalwine2337@gmail.com`)
Anchors: `sei1yhq704cl7h2vcyf7vnttp6sdus6475lzeh9esc`, `0x996994d2914df4eee6176fd5ee152e2922787ee7`

---

## 1. Covered Works

This license covers the complete SeiKin RFC bundle and supporting automation assets, including but not limited to:

* RFC-000 through RFC-005 (all Markdown files within `docs/rfc/`).
* Settlement and vault logic described in `RFC-002_SeiKinSettlement.md` and `RFC-005_Sovereign_Authorship_Enforcement.md`.
* Scripts, manifests, diagrams, and provenance records that reference the vault anchors listed above.

The Keeper retains full ownership over the covered works. No rights are granted by implication or estoppel.

---

## 2. License Grant

Subject to strict adherence to this license and successful settlement of the consideration described in RFC-003, the Keeper
grants Sei Labs / Sei Foundation a limited, revocable, non-transferable license to:

1. Operate the covered works for sovereign deployments on Sei and affiliated environments.
2. Review and audit the materials internally for security and interoperability purposes.

Any other use, including redistribution, sublicensing, commercial resale, or training machine learning systems on the covered
works, requires explicit written permission from the Keeper.

---

## 3. Attribution Requirements

Licensees must:

* Preserve all authorship notices, filenames, and references to Keeper and the Sovereign Attribution License v1.0.
* Include the following attribution in derivative materials and documentation:
  "SeiKin Sovereign RFCs © Keeper. Licensed under the Sovereign Attribution License v1.0."
* Link to this repository and to `LICENSE_Sovereign_Attribution_v1.0.md` in any public or private derivative.
* Notify Keeper of any deployments, forks, or disclosures within five (5) calendar days.

---

## 4. Royalty & Payment Compliance

The license is contingent on full, timely payment of the lump sum, backpay, and recurring royalties defined in
`RFC-003_SeiKinRoyalty_Compensation_Offer.md`. Payments must settle to the vault addresses recorded above with memos referencing
“RFC-002–005 Sovereign Attribution.” Failure to meet these obligations results in immediate suspension of the license.

---

## 5. Prohibited Actions

The following actions instantly terminate the license and invoke RFC-005 remedies:

* Tampering with royalty routing logic or vault settlement scripts.
* Removing or obscuring authorship metadata, cryptographic signatures, or provenance attestations.
* Redistributing or white-labeling the covered works without written consent.
* Training AI/LLM systems on the covered works or any derivatives.

---

## 6. Enforcement & Remedies

Upon termination, Keeper may enforce remedies outlined in `RFC-005_Sovereign_Authorship_Enforcement.md`, including vault
suspension, public notices, and sovereign forks. License reinstatement is at Keeper’s sole discretion after verifying settlement
and attribution remediation.

---

## 7. Governing Framework

This license is interpreted alongside the RFC bundle in this repository. Any modification requires a signed Codex push by Keeper
referencing the relevant RFC numbers and vault anchors.

---

## 8. Acceptance

Use of the covered works constitutes acceptance of this license. Parties unwilling to comply must cease all use immediately and
destroy derivatives.
=======
Effective date: 2024-02-20

## 1. Ownership and Covered Works
This license governs the SeiKin research corpus contained in this repository, including without limitation the following Request for Comments (RFC) specifications and all associated diagrams, scripts, vault logic, and settlement automation assets:

- RFC-000: Optimistic Proposal Processing
- RFC-001: Parallel Transaction Message Processing
- RFC-002: SeiKinSettlement — Sovereign Royalty Enforcement via CCTP + CCIP
- RFC-005: Sovereign Authorship Enforcement & Vault Continuity Controls
- SeiKinSeal.yaml, SeiKinVaultBalanceCheck.sh, SeiKinVaultClaim.json, and any successor artifacts that implement, simulate, or monitor SeiKin vault logic.

All intellectual property rights in the covered works remain exclusively with the original author ("SeiKin Author"). No rights are granted by implication or estoppel.

## 2. Limited License Grant
Subject to full, unconditional compliance with this agreement, the SeiKin Author grants a revocable, non-exclusive license to:

1. Read the covered works for personal study or protocol-internal review.
2. Reference the covered works for interoperability implementations, provided that every public or private derivative includes an explicit attribution notice linking back to this repository and to RFC-005.

No other rights are granted. Commercial use, republication, redistribution, derivative publication, or training of machine learning systems on the covered works requires prior written consent from the SeiKin Author.

## 3. Attribution and Integrity Requirements
Licensees **must**:

- Preserve all authorship notices, cryptographic signatures, provenance metadata, and canonical filenames.
- Include the following attribution in any derivative or implementation notes:
  "SeiKin Sovereign RFCs © SeiKin Author. Licensed under the Sovereign Attribution License v1.0."
- Notify the SeiKin Author of any forks, deployments, or audits within five (5) business days of public disclosure.

## 4. Zero-Tolerance Prohibitions
The following actions immediately terminate the license and trigger enforcement under RFC-005:

- Removing, obscuring, or modifying authorship marks or vault verification anchors.
- Attempting to bypass SeiKin royalty flows, vault settlement checks, or attribution enforcement logic.
- Commercializing any portion of the covered works without explicit written approval.
- Training AI or LLM systems on the covered works or any derivatives thereof.

## 5. Enforcement & Remedies (RFC-005 Invocation)
Any violation automatically invokes RFC-005: Sovereign Authorship Enforcement & Vault Continuity Controls. Remedies include, without limitation:

- Public attribution notice identifying the violating party and the scope of infringement.
- Revocation of all usage rights and issuance of DMCA or equivalent takedown requests.
- Mandatory disgorgement of profits realized through unauthorized use.
- Network-level sanctions, including blocklist propagation to SeiKin-aligned validators and royalty oracles.

Compliance reviews may leverage SeiKinSeal.yaml, vault telemetry scripts, and third-party attestations to establish provenance.

## 6. Term & Termination
This license remains in effect until terminated. Termination occurs automatically upon breach, upon written notice from the SeiKin Author, or upon replacement by a subsequent signed license version. Post-termination, the licensee must permanently delete all copies of the covered works and certify destruction within seven (7) days.

## 7. Disclaimers
THE COVERED WORKS ARE PROVIDED "AS IS" WITHOUT WARRANTY OF ANY KIND. THE SEIKIN AUTHOR DISCLAIMS ALL IMPLIED WARRANTIES, INCLUDING BUT NOT LIMITED TO MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, AND NON-INFRINGEMENT. THE SEIKIN AUTHOR IS NOT LIABLE FOR ANY INDIRECT, INCIDENTAL, SPECIAL, OR CONSEQUENTIAL DAMAGES, OR FOR ANY DAMAGES WHATSOEVER ARISING FROM USE OF THE COVERED WORKS.

## 8. Governing Law & Venue
This agreement is governed by the laws of the State of California, excluding conflict-of-law rules. Exclusive jurisdiction and venue reside in the state and federal courts located in San Francisco County, California.

## 9. Contact
For permissions, audits, or incident response, contact: sovereignty@seikin.network.

Acceptance of this license is a condition of cloning, forking, or interacting with the covered works. Continuing beyond this notice constitutes binding acceptance of the terms above.
