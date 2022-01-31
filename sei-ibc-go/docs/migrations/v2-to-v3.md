# Migrating from ibc-go v2 to v3

This document is intended to highlight significant changes which may require more information than presented in the CHANGELOG.
Any changes that must be done by a user of ibc-go should be documented here.

There are four sections based on the four potential user groups of this document:
- Chains
- IBC Apps
- Relayers
- IBC Light Clients

**Note:** ibc-go supports golang semantic versioning and therefore all imports must be updated to bump the version number on major releases.
```go
github.com/cosmos/ibc-go/v2 -> github.com/cosmos/ibc-go/v3
```

No genesis or in-place migrations are required when upgrading from v1 or v2 of ibc-go.

## Chains

### ICS20

The `transferkeeper.NewKeeper(...)` now takes in an ICS4Wrapper. 
The ICS4Wrapper should be the IBC Channel Keeper unless ICS 20 is being connected to a middleware application.

### ICS27

ICS27 Interchain Accounts has been added as a supported IBC application of ibc-go.
Please see the [ICS27 documentation](../app-modules/interchain-accounts/overview.md) for more information.

## IBC Apps


### `OnChanOpenTry` must return negotiated application version

The `OnChanOpenTry` application callback has been modified.
The return signature now includes the application version. 
IBC applications must perform application version negoitation in `OnChanOpenTry` using the counterparty version. 
The negotiated application version then must be returned in `OnChanOpenTry` to core IBC.
Core IBC will set this version in the TRYOPEN channel.

### `NegotiateAppVersion` removed from `IBCModule` interface

Previously this logic was handled by the `NegotiateAppVersion` function.
Relayers would query this function before calling `ChanOpenTry`.
Applications would then need to verify that the passed in version was correct.
Now applications will perform this version negotiation during the channel handshake, thus removing the need for `NegotiateAppVersion`.

### Channel state will not be set before application callback

The channel handshake logic has been reorganized within core IBC. 
Channel state will not be set in state after the application callback is performed.
Applications must rely only on the passed in channel parameters instead of querying the channel keeper for channel state.

### IBC application callbacks moved from `AppModule` to `IBCModule`

Previously, IBC module callbacks were apart of the `AppModule` type. 
The recommended approach is to create an `IBCModule` type and move the IBC module callbacks from `AppModule` to `IBCModule` in a separate file `ibc_module.go`. 

The mock module go API has been broken in this release by applying the above format. 
The IBC module callbacks have been moved from the mock modules `AppModule` into a new type `IBCModule`. 

As apart of this release, the mock module now supports middleware testing. Please see the [README](../../testing/README.md#middleware-testing) for more information.

Please review the [mock](../../testing/mock/ibc_module.go) and [transfer](../../modules/apps/transfer/ibc_module.go) modules as examples. Additionally, [simapp](../../testing/simapp/app.go) provides an example of how `IBCModule` types should now be added to the IBC router in favour of `AppModule`.

## Relayers

`AppVersion` gRPC has been removed.
The `version` string in `MsgChanOpenTry` has been deprecated and will be ignored by core IBC. 
Relayers no longer need to determine the version to use on the `ChanOpenTry` step.
IBC applications will determine the correct version using the counterparty version. 

## IBC Light Clients

The `GetProofSpecs` function has been removed from the `ClientState` interface. This function was previously unused by core IBC. Light clients which don't use this function may remove it. 

