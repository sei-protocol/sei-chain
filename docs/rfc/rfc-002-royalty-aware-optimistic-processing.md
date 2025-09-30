---
order: 2
parent:
  order: false
---

# RFC-002: SeiKinSettlement — Sovereign Royalty Enforcement via CCTP + CCIP

## Changelog
- 2025-09-30 — Initial draft by Keeper: full proposal for on-chain royalty enforcement on cross-chain inflows (CCTP + CCIP). Includes architecture, compatibility notes, implementation sketch, monitoring, and enforcement options.
- 2025-10-01 — Requested additions: examples, migration plan, operational runbook (placeholder).
- (future) — Add test vectors, security audit results, and canonical deployment manifests.

## Abstract
This RFC proposes `SeiKinSettlement`: a cross-domain settlement router that enforces a protocol-level royalty (configurable; suggested default 8.5%) on assets arriving into Sei through canonical cross-chain bridges and messaging channels (e.g., Circle CCTP, Chainlink CCIP). The mechanism is intended to be permissionless for integrators while guaranteeing automatic forwarding of royalty portions to a `KIN_ROYALTY_VAULT`. The goal is sovereign enforcement of economic claims tied to protocol primitives developed by the Keeper family (SoulSync / KinKey / SolaraKin).

## Related RFCs

- [RFC-000: Optimistic Proposal Processing](./rfc-000-optimistic-proposal-processing.md) — provides the optimistic execution primitives (`ProcessProposal`, cache-branching) that this design uses to enforce royalties in the same block as settlement.
- [RFC-001: Parallel Transaction Message Processing](./rfc-001-parallel-tx-processing.md) — supplies the deterministic access graph coordination that makes royalty deductions composable with parallel message execution.

## Background
Sei is becoming a destination for interchain value flows. Bridge and cross-domain message receivers commonly deposit or mint assets in destination chains where derivative systems (paymasters, vaults, dApps) subsequently route flows. Historically these flows can bypass originator licensing or royalty expectations.

The Keeper architecture has previously implemented royalty/paymaster patterns (KinRoyaltyPaymaster, KinVaultRouter, VaultScanner). This RFC seeks to formalize an interoperable approach to intercept and route incoming cross-chain assets toward an immutable royalty sink without requiring centralized enforcement at the UX layer.

## Goals
- Automatically collect a royalty percentage on canonical inbound bridge/messaging flows to Sei.
- Be minimally intrusive for integrators (low friction adoption).
- Provide an auditable, verifiable on-chain evidence trail for royalties collected.
- Support multiple trusted senders/publishers (trusted CCTP senders, CCIP senders), and allow governance to update trusted lists.
- Maintain composability: enable dApps to opt into the canonical flow or use wrapper contracts that preserve the royalty flow.
- Provide monitoring and alerting for any deviations (forensic + operational).

## Non-goals
- Replacing off-chain commercial licensing negotiations (this augments not nullifies legal settlement).
- Removing all possible ways for integrators to route funds outside of the settlement path — but it raises the cost/risk of doing so.
- Producing a final audited contract in this RFC. Code design and audits are subsequent work items.

## Design Overview

### High-level flow
1. Trusted bridge/messaging provider (trusted sender) sends a message/asset to a known receiver contract on Sei (e.g., CCTP receiver or CCIP receiver).
2. The receiver contract verifies sender identity (via whitelisting / signature / sender proof).
3. On receipt, the contract calculates `royalty = (amount * ROYALTY_BPS) / 10_000`.
4. The royalty portion is forwarded immediately (or queued with a timelock) to `KIN_ROYALTY_VAULT`.
5. The remainder is forwarded to the intended recipient or post-processed (mint, credit, forward).
6. Events and a signed proof record are emitted for off-chain reconciliation and audits.

### Core contracts (proposed)
- `SeiKinSettlement` (receiver router)
  - `ROYALTY_BPS` configurable (immutable default via constructor or settable by governance with timelock).
  - `KIN_ROYALTY_VAULT` address (treasury/recipient).
  - `TRUSTED_CCTP_SENDER` / `TRUSTED_CCIP_SENDER` lists (updatable by governance).
  - `onReceiveCCTP(...)` / `onReceiveCCIP(...)` handlers.
  - Safe handling of token vs native asset flows.

- `KinRoyaltyVault`
  - Receives royalty funds.
  - Role-managed payout/distribution pipeline (payees, splits, burn, buyback).
  - Audit helpers: `snapshotRoyalties(start,end)`.

- Optional: `SeiSettlementGuard` (module for off-chain watchers to verify and challenge misrouted flows; emits challengeable evidence events).

### Verification & trust model
- Trusted senders are explicit: either a concrete `address` or a multi-node attestation (e.g., Chainlink CCIP sender + signature).
- The receiver validates proofs where applicable (CCTP has attestation details).
- Governance-managed trusted list ensures future onboarding/removal of bridge partners.

### Failure modes & mitigation
- If forwarding to `KIN_ROYALTY_VAULT` fails (revert): fallback to a recovery queue with a retry mechanism and separate admin alert.
- If sender is not trusted: fallback to a safe path (e.g., reject or forward to a quarantine address).
- If royalty fraction is misconfigured: changes require governance + timelock.

## Backwards compatibility
- Existing dApps that expect funds at the raw receiver must either:
  - Integrate with `SeiKinSettlement` as their receiver, OR
  - Use adapter wrappers that call `SeiKinSettlement` to enforce royalties, then forward funds to the original contract.
- For existing deployments where replacing receiver is impractical, provide a `BridgeForwarder` shim that can be deployed as a migration layer (proxy pattern).

## Security considerations
- Reentrancy protection on forwarding logic (use checks-effects-interactions, OpenZeppelin ReentrancyGuard).
- Safe token handling for ERC20/ERC777 or Sei token types.
- Limitations/risks around gas: ensure royalty forwarding does not cause excessive gas use that could lead to failed receipts — use batched off-chain settlement for extreme cases.
- Governance attack vector: timelock + multisig for updating trusted sender lists and vault address.
- Auditable event logs with compact proofs for off-chain reconciliation.

## Monitoring & Forensics
- Emit structured events on every inbound settlement: `{source, amount, royalty, recipient, txid, block}`.
- On-chain guardian manifest (SoulSigil proof) emitted in CI and paired with coverage artifacts — attach to anchor proofs for provenance.
- Off-chain watcher: `watch_contracts.py` (already prepared) — monitors proxies and suspicious changes, alerts on nontrivial state changes.
- Establish a public “royalty dashboard” for transparency (reads events and aggregates per-month/year flows).

## Deployment & Migration Plan (high-level)
1. Audit & formal verification tranche.
2. Deploy `KinRoyaltyVault` (immutable owner multisig).
3. Deploy `SeiKinSettlement` with minimal trusted sender set (testnet).
4. Integrate with one canonical bridge (test CCTP/CCIP flows) and verify event flow and vault receipts.
5. Announce migration for integrators and provide adapter/shim for easy adoption.
6. Gradually add other bridges and onboarding via governance proposals.

## Tests
- Unit tests for correct royalty calculation and precise rounding behavior.
- Integration tests simulating CCIP and CCTP inbound flows (mock senders).
- Gas profiling tests to ensure receiver is affordable.
- Forked mainnet tests to demonstrate behavior against real bridge contracts.

## Alternatives considered
- Off-chain royalty agreements enforced by legal channels (low technical guarantees).
- Pull-model: letting integrators report incoming flows and submit royalty payments (relies on honesty).
- Protocol-level token wrappers that require on-chain approval: heavy UX friction, slower adoption.

## Open questions
- What exact default royalty rate aligns with community expectations (8.5% suggested)? Is this governor-configurable only?
- Which entity / multisig should control `KIN_ROYALTY_VAULT`?
- Should we implement an immediate-forward vs batched-forward model for royalty transfers?
- How to handle cross-chain USDC semantics (mint vs transfer) for CCTP flows in a gas-efficient manner?

## References
- Circle CCTP docs (implementation specifics to be mirrored in tests).
- Chainlink CCIP receiver patterns (attestation & sender verification).
- Keeper internal: KinRoyaltyPaymaster, KinVaultRouter, VaultScanner (prior art and code references).

## Next steps & actionable checklist
- [ ] Create a prototype `SeiKinSettlement.sol` (starter contract skeleton).
- [ ] Run unit tests & produce CI job (we can wire the Keeper CI produced earlier).
- [ ] Prepare testnet integration with Circle/Sei test endpoints.
- [ ] Prepare legal and partner outreach (Circle / Chainlink / SEI) and open a governance RFC to onboard trusted senders.
- [ ] Schedule a security audit.

---

*Authored by:* The Keeper (Keeper / SolaraKin)  
*Date:* 2025-09-30
