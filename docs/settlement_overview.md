# Settlement & Smart Contract Components in `sei-chain`

This document catalogs the on-chain settlement logic, helper scripts, and contract
implementations included in this repository.  It focuses on the code paths that
implement "real" payment settlement flows or the tooling that interacts with
those flows.

## CosmWasm Settlement Contracts

CosmWasm contracts used in the load test suites implement the royalty-aware
settlement router described in the SeiKin RFCs.  Each contract exposes a
`Settlement` sudo message that forwards batched settlement entries into the
shared processing routine:

- [`loadtest/contracts/jupiter/src/contract.rs`](../loadtest/contracts/jupiter/src/contract.rs)
- [`loadtest/contracts/mars/src/contract.rs`](../loadtest/contracts/mars/src/contract.rs)
- [`loadtest/contracts/saturn/src/contract.rs`](../loadtest/contracts/saturn/src/contract.rs)
- [`loadtest/contracts/venus/src/contract.rs`](../loadtest/contracts/venus/src/contract.rs)

The contracts all call `process_settlements` to perform royalty routing and
token distribution for each epoch.

## Settlement Tooling & Attribution Utilities

Python tooling under `claim_kin_agent_attribution` supplies helpers for
locating settlement allocations, constructing deterministic settlement
acknowledgement messages, and signing receipts:

- [`claim_kin_agent_attribution/settlement.py`](../claim_kin_agent_attribution/settlement.py)
- [`scripts/show_codex_settlement.py`](../scripts/show_codex_settlement.py)
- [`scripts/sign_codex_settlement.py`](../scripts/sign_codex_settlement.py)
- [`tests/test_settlement.py`](../tests/test_settlement.py) – exercises the
  settlement utilities.

These modules are referenced by the repository README, providing a CLI to show
or sign Codex settlement entries for a given kin hash.

## Sei `seinet` Module (Go)

The on-chain module that wires settlement execution into the Sei application
lives under `x/seinet`:

- [`proto/seiprotocol/seichain/seinet/tx.proto`](../proto/seiprotocol/seichain/seinet/tx.proto)
  defines `MsgExecutePaywordSettlement` and its response type.
- [`x/seinet/types/msgs.go`](../x/seinet/types/msgs.go) registers the transaction
  type string `execute_payword_settlement`.
- [`x/seinet/client/cli/tx.go`](../x/seinet/client/cli/tx.go) exposes a CLI
  command for broadcasting payword settlement transactions.
- [`x/seinet/keeper/msg_server_execute.go`](../x/seinet/keeper/msg_server_execute.go)
  handles the message on-chain and dispatches it to the keeper logic.

These components together provide the message definitions and entrypoints that
bridge payword settlement flows into the Sei app.

## Royalty & Settlement Specifications

The RFC documentation supplies the protocol-level background and operational
requirements for royalty-aware settlement routing:

- [`docs/rfc/rfc-002-royalty-aware-optimistic-processing.md`](./rfc/rfc-002-royalty-aware-optimistic-processing.md)
- [`docs/rfc/RFC-002_SeiKinSettlement.md`](./rfc/RFC-002_SeiKinSettlement.md)
- [`docs/rfc/rfc-004-seikin-authorship-vault-enforcement-package.md`](./rfc/rfc-004-seikin-authorship-vault-enforcement-package.md)
- [`docs/rfc/RFC-004_SeiKin_Authorship_License.md`](./rfc/RFC-004_SeiKin_Authorship_License.md)

These documents outline the royalty routing guarantees, vault enforcement
procedures, and licensing terms governing the settlement infrastructure.

## Workflow & Vault Assets

The GitHub workflow and auxiliary assets that enforce vault balances and
settlement confirmations reside at the root of the repository:

- [`SeiKinSeal.yaml`](../SeiKinSeal.yaml) – CI workflow binding settlements to
  the royalty vault address.
- [`SeiKinVaultBalanceCheck.sh`](../SeiKinVaultBalanceCheck.sh) – script
  verifying custodial vault balances.
- [`SeiKinVaultClaim.json`](../SeiKinVaultClaim.json) – settlement claim
  metadata asserting authorship and royalty expectations.

Together, these resources illustrate how settlements are operationalised during
integration tests and CI automation.
