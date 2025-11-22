---
order: 1
---

# Roadmap ibc-go

_Lastest update: Dec 22, 2021_

This document endeavours to inform the wider IBC community about plans and priorities for work on ibc-go byt the team at Interchain GmbH. It is intended to broadly inform all users of ibc-go, including developers and operators of IBC, relayer, chain and wallet applications.

This roadmap should be read as a high-level guide, rather than a commitment to schedules and deliverables. The degree of specificity is inversely proportional to the timeline. We will update this document periodically to reflect the status and plans.

The release tags and timelines are educated guesses based on the information at hand at the moment of updating this document. Since predicting the final version number (specially for minor and patch numbers) can be challenging (since we might need to release unforeseen security vulnerability patches or urgent bug fixes), we are using alphabet letters as placeholders. Once we get closer to the release date, the placeholder will be replaced with the right number. An example for clarification...

Let's assume that the planned release schedule looks like the following:
- At time `t0`:
  - The first planned patch release for the `v2.0.x` release series with release tag `v2.0.a`. The placeholder is `a` since this is the first patch release in the planning.
  - The first planned minor release for the `v2.x` release series with release tag `v2.a.0`. The placeholder is `a` since this is the first minor release in the planning.
- At time `t0 + delta`:
  - The second planned patch release for the `v2.0.x` release series with release tag `v2.0.b`. The placehoder is `b` since this is the next patch release of this release series after `v2.0.a` in the planning.
  - The first planned patch release for the new `v2.a.x` release series with release tag `v2.a.a`. The patch version placeholder is `a` because this is the first planned patch release of the `v2.a.x` release series.

## Q1 - 2022

## Features 

### Interchain accounts 

- Work on any issues that may come out of the two external audits.
- Create beta, release candidate and final releases.

### Relayer incentivisation

- Work on issues that may arise from internal audit.
- External audit (issues may arise that we need to work on before release).
- Create alpha, beta, release candidate and final release.
 
### Align implementation with ICS02

- Finalize work for: [#284](https://github.com/cosmos/ibc-go/issues/284), [#285](https://github.com/cosmos/ibc-go/issues/285), [#286](https://github.com/cosmos/ibc-go/issues/286), [#594](https://github.com/cosmos/ibc-go/issues/594) and [#599](https://github.com/cosmos/ibc-go/issues/599). 

### Interchain security

- Testnet testing of [V1](https://github.com/cosmos/gaia/blob/main/docs/interchain-security.md#v1---full-validator-set).

### Backlog issues

- [#545](https://github.com/cosmos/ibc-go/issues/545): Remove the `GetTransferAccount` function, since we never use the ICS20 transfer module account (every escrow address is created as a regular account).
- [#559](https://github.com/cosmos/ibc-go/issues/559): Changes needed to support the migration to SMT storage. This is basically adding a new proof spec that will be used during connection handshake with a chain that has migrated to SMT to verify that the light client of the counterparty chain uses the correct proof specs to be able to verify proofs for that chain.
- And more to be added later!

## Release schedule

|Release|Milestone|Date|
|-------|---------|----|
|[`v3.0.0-alpha2`](https://github.com/cosmos/ibc-go/releases/tag/v3.0.0-alpha2)||Jan 07, 2021|

During this quarter we will also probably release versions that bump the Cosmos SDK to `v0.45` and Tendermint to `v0.35`, but at the moment of writing it is difficult to estimate when. Check our roadmap regularly for updates.

### H2 January

- [`v3.0.0-beta1`](https://github.com/cosmos/ibc-go/milestone/12): Beta 1 release of `v3.0.0` including Interchain Accounts, an update of Golang from `v1.15` to `v1.17`, and some core improvements. This is a Go-API breaking release because of [#472](https://github.com/cosmos/ibc-go/issues/472) and [#675](https://github.com/cosmos/ibc-go/pull/675).

### H1 February

- [`v3.0.0-rc0`](https://github.com/cosmos/ibc-go/milestone/12): Release candidate 0 of `v3.0.0` including Interchain Accounts, an update of Golang from `v1.15` to `v1.17`, and some core improvements. This is a Go-API breaking release because of [#472](https://github.com/cosmos/ibc-go/issues/472) and [#675](https://github.com/cosmos/ibc-go/pull/675).

### H2 February

- [`v3.a.0-alpha1`](https://github.com/cosmos/ibc-go/milestone/16): Alpha release of `v3.a.0` including Relayer Incentivisation. This release will include fixes to issues that surfaced during the internal audit.

### H1 March

- [`v3.0.0`](https://github.com/cosmos/ibc-go/milestone/12): Final release of `v3.0.0` including Interchain Accounts, an update of Golang from `v1.15` to `v1.17`, and some core improvements. This is a Go-API breaking release because of [#472](https://github.com/cosmos/ibc-go/issues/472) and [#675](https://github.com/cosmos/ibc-go/pull/675).
- [`v3.a.0-beta1`](https://github.com/cosmos/ibc-go/milestone/16): Beta release of `v3.a.0` including Relayer Incentivisation. This release will include fixes to issues that surfaced during the external audit.

### H2 March

- [`v3.a.0-rc0`](https://github.com/cosmos/ibc-go/milestone/16): Release candiate 0 `v3.1.0` including Relayer Incentivisation.

## Q2 - 2022

### Features

> Full scope still TBD.

### Support for Wasm-based light clients

There is an open [PR](https://github.com/cosmos/ibc-go/pull/208) that implements support for Wasm-based light clients, but it needs to be updated after the finalization of the [ICS28](https://github.com/cosmos/ibc/tree/master/spec/client/ics-008-wasm-client) specification. The PR need thorough review, more tests and potentially implementation changes.

## Release schedule

During this quarter we will also probably release versions that bump the Cosmos SDK to `v0.46` and to `v1.0`, but at the moment of writing it is difficult to estimate when. Check our roadmap regularly for updates.

### H1 April

- [`v3.a.0`](https://github.com/cosmos/ibc-go/milestone/16): Final release of `v3.a.0` including Relayer Incentivisation.
- [`v4.0.0-rc0`](https://github.com/cosmos/ibc-go/milestone/16): Release candidate 0 of `v4.0.0` including the work for the issues to bring ibc-go implementation in line with ICS02 (which are Go-API breaking changes).

### H2 April

- [`v4.0.0`](https://github.com/cosmos/ibc-go/milestone/16): Release candidate 0 of `v4.0.0` including the work for the issues to bring ibc-go implementation in line with ICS02 (which are Go-API breaking changes).


