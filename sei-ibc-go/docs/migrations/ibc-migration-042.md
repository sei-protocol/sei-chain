# Migrating to ibc-go

This file contains information on how to migrate from the IBC module contained in the SDK 0.41.x line to the IBC module in the ibc-go repository based on the 0.42 SDK version. 

## Import Changes

The most obvious changes is import name changes. We need to change:
- applications -> apps
- cosmos-sdk/x/ibc -> ibc-go

On my GNU/Linux based machine I used the following commands, executed in order:

`grep -RiIl 'cosmos-sdk\/x\/ibc\/applications' | xargs sed -i 's/cosmos-sdk\/x\/ibc\/applications/ibc-go\/apps/g'`

`grep -RiIl 'cosmos-sdk\/x\/ibc' | xargs sed -i 's/cosmos-sdk\/x\/ibc/ibc-go/g'`

Executing these commands out of order will cause issues. 

Feel free to use your own method for modifying import names.

## Proto file changes

The protobuf files have change package naming. 
The new package naming begins with `ibcgo` instead of `ibc`.

The gRPC querier service endpoints have changed slightly. The previous files used `v1beta1`, this has been updated to `v1`.

## Proposals

### UpdateClientProposal
The `UpdateClient` has been modified to take in two client-identifiers and one initial height. Please see the [documentation](..//proposals.md) for more information. 

Simapp registration was incorrect in the 0.41.x releases. The `UpdateClient` proposal should be registered with the router key belonging to `ibc-go/core/02-client/types`.
See this [commit](https://github.com/cosmos/cosmos-sdk/pull/8405/commits/9fae3ce6a335a6e2137aee09f7359c45957fb6fc#diff-8d1ca8086ee74e8f0490825ba21e7435be4753922192ff691311483aa3e71a0aL312)

### UpgradeProposal

A new IBC proposal type has been added, `UpgradeProposal`. This handles an IBC (breaking) Upgrade. The previous `UpgradedClientState` field in an Upgrade `Plan` has been deprecated in favor of this new proposal type. 

### Proposal CLI Registration

Please ensure both proposal type CLI commands are registered on the governance module by adding the following arguments to `gov.NewAppModuleBasic()`:

`ibcclientclient.UpdateClientProposalHandler, ibcclientclient.UpgradeProposalHandler`

REST routes are not supported for these proposals. 

### Proposal Handler Registration

The `ClientUpdateProposalHandler` has been renamed to `ClientProposalHandler`. It handles both `UpdateClientProposal`s and `UpgradeProposal`s.

Please ensure the governance module adds the following route:

`AddRoute(ibcclienttypes.RouterKey, ibcclient.NewClientProposalHandler(app.IBCKeeper.ClientKeeper))`
