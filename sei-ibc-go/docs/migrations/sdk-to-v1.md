# Migrating to ibc-go

This file contains information on how to migrate from the IBC module contained in the SDK 0.41.x and 0.42.x lines to the IBC module in the ibc-go repository based on the 0.44 SDK version. 

## Import Changes

The most obvious changes is import name changes. We need to change:
- applications -> apps
- cosmos-sdk/x/ibc -> ibc-go

On my GNU/Linux based machine I used the following commands, executed in order:

```
grep -RiIl 'cosmos-sdk\/x\/ibc\/applications' | xargs sed -i 's/cosmos-sdk\/x\/ibc\/applications/ibc-go\/modules\/apps/g'
```

```
grep -RiIl 'cosmos-sdk\/x\/ibc' | xargs sed -i 's/cosmos-sdk\/x\/ibc/ibc-go\/modules/g'
```

ref: [explanation of the above commands](https://www.internalpointers.com/post/linux-find-and-replace-text-multiple-files)

Executing these commands out of order will cause issues. 

Feel free to use your own method for modifying import names.

NOTE: Updating to the `v0.44.0` SDK release and then running `go mod tidy` will cause a downgrade to `v0.42.0` in order to support the old IBC import paths.
Update the import paths before running `go mod tidy`.  

## Chain Upgrades

Chains may choose to upgrade via an upgrade proposal or genesis upgrades. Both in-place store migrations and genesis migrations are supported. 

**WARNING**: Please read at least the quick guide for [IBC client upgrades](../ibc/upgrades/README.md) before upgrading your chain. It is highly recommended you do not change the chain-ID during an upgrade, otherwise you must follow the IBC client upgrade instructions.

Both in-place store migrations and genesis migrations will:
- migrate the solo machine client state from v1 to v2 protobuf definitions
- prune all solo machine consensus states
- prune all expired tendermint consensus states

Chains must set a new connection parameter during either in place store migrations or genesis migration. The new parameter, max expected block time, is used to enforce packet processing delays on the receiving end of an IBC packet flow. Checkout the [docs](https://github.com/cosmos/ibc-go/blob/release/v1.0.x/docs/ibc/proto-docs.md#params-2) for more information.

### In-Place Store Migrations

The new chain binary will need to run migrations in the upgrade handler. The fromVM (previous module version) for the IBC module should be 1. This will allow migrations to be run for IBC updating the version from 1 to 2.

Ex:
```go
app.UpgradeKeeper.SetUpgradeHandler("my-upgrade-proposal",
        func(ctx sdk.Context, _ upgradetypes.Plan, _ module.VersionMap) (module.VersionMap, error) {
            // set max expected block time parameter. Replace the default with your expected value
            // https://github.com/cosmos/ibc-go/blob/release/v1.0.x/docs/ibc/proto-docs.md#params-2
            app.IBCKeeper.ConnectionKeeper.SetParams(ctx, ibcconnectiontypes.DefaultParams())

            fromVM := map[string]uint64{
                ... // other modules
                "ibc":          1,
                ... 
            }   
            return app.mm.RunMigrations(ctx, app.configurator, fromVM)
        })      

```

### Genesis Migrations

To perform genesis migrations, the following code must be added to your existing migration code.

```go
// add imports as necessary
import (
    ibcv100 "github.com/cosmos/ibc-go/modules/core/legacy/v100"
    ibchost "github.com/cosmos/ibc-go/modules/core/24-host"
)

...

// add in migrate cmd function
// expectedTimePerBlock is a new connection parameter
// https://github.com/cosmos/ibc-go/blob/release/v1.0.x/docs/ibc/proto-docs.md#params-2
newGenState, err = ibcv100.MigrateGenesis(newGenState, clientCtx, *genDoc, expectedTimePerBlock)
if err != nil {
    return err 
}
```

**NOTE:** The genesis chain-id, time and height MUST be updated before migrating IBC, otherwise the tendermint consensus state will not be pruned.


## IBC Keeper Changes

The IBC Keeper now takes in the Upgrade Keeper. Please add the chains' Upgrade Keeper after the Staking Keeper:

```diff
        // Create IBC Keeper
        app.IBCKeeper = ibckeeper.NewKeeper(
-               appCodec, keys[ibchost.StoreKey], app.GetSubspace(ibchost.ModuleName), app.StakingKeeper, scopedIBCKeeper,
+               appCodec, keys[ibchost.StoreKey], app.GetSubspace(ibchost.ModuleName), app.StakingKeeper, app.UpgradeKeeper, scopedIBCKeeper,
        )

``` 

## Proposals

### UpdateClientProposal

The `UpdateClient` has been modified to take in two client-identifiers and one initial height. Please see the [documentation](../ibc/proposals.md) for more information. 

### UpgradeProposal

A new IBC proposal type has been added, `UpgradeProposal`. This handles an IBC (breaking) Upgrade. 
The previous `UpgradedClientState` field in an Upgrade `Plan` has been deprecated in favor of this new proposal type. 

### Proposal Handler Registration

The `ClientUpdateProposalHandler` has been renamed to `ClientProposalHandler`. 
It handles both `UpdateClientProposal`s and `UpgradeProposal`s.

Add this import: 

```diff
+       ibcclienttypes "github.com/cosmos/ibc-go/modules/core/02-client/types"
```

Please ensure the governance module adds the correct route:

```diff
-               AddRoute(ibchost.RouterKey, ibcclient.NewClientUpdateProposalHandler(app.IBCKeeper.ClientKeeper))
+               AddRoute(ibcclienttypes.RouterKey, ibcclient.NewClientProposalHandler(app.IBCKeeper.ClientKeeper))
```

NOTE: Simapp registration was incorrect in the 0.41.x releases. The `UpdateClient` proposal handler should be registered with the router key belonging to `ibc-go/core/02-client/types` 
as shown in the diffs above. 

### Proposal CLI Registration

Please ensure both proposal type CLI commands are registered on the governance module by adding the following arguments to `gov.NewAppModuleBasic()`:

Add the following import:
```diff
+       ibcclientclient "github.com/cosmos/ibc-go/modules/core/02-client/client"
```

Register the cli commands: 

```diff 
       gov.NewAppModuleBasic(
             paramsclient.ProposalHandler, distrclient.ProposalHandler, upgradeclient.ProposalHandler, upgradeclient.CancelProposalHandler,
+            ibcclientclient.UpdateClientProposalHandler, ibcclientclient.UpgradeProposalHandler,
       ),
```

REST routes are not supported for these proposals. 

## Proto file changes

The gRPC querier service endpoints have changed slightly. The previous files used `v1beta1` gRPC route, this has been updated to `v1`.

The solo machine has replaced the FrozenSequence uint64 field with a IsFrozen boolean field. The package has been bumped from `v1` to `v2`

## IBC callback changes

### OnRecvPacket

Application developers need to update their `OnRecvPacket` callback logic. 

The `OnRecvPacket` callback has been modified to only return the acknowledgement. The acknowledgement returned must implement the `Acknowledgement` interface. The acknowledgement should indicate if it represents a successful processing of a packet by returning true on `Success()` and false in all other cases. A return value of false on `Success()` will result in all state changes which occurred in the callback being discarded. More information can be found in the [documentation](https://github.com/cosmos/ibc-go/blob/main/docs/ibc/apps.md#receiving-packets).

The `OnRecvPacket`, `OnAcknowledgementPacket`, and `OnTimeoutPacket` callbacks are now passed the `sdk.AccAddress` of the relayer who relayed the IBC packet. Applications may use or ignore this information. 

## IBC Event changes

The `packet_data` attribute has been deprecated in favor of `packet_data_hex`, in order to provide standardized encoding/decoding of packet data in events. While the `packet_data` event still exists, all relayers and IBC Event consumers are strongly encouraged to switch over to using `packet_data_hex` as soon as possible.

The `packet_ack` attribute has also been deprecated in favor of `packet_ack_hex` for the same reason stated above. All relayers and IBC Event consumers are strongly encouraged to switch over to using `packet_ack_hex` as soon as possible.

The `consensus_height` attribute has been removed in the Misbehaviour event emitted. IBC clients no longer have a frozen height and misbehaviour does not necessarily have an associated height.

## Relevant SDK changes

* (codec) [\#9226](https://github.com/cosmos/cosmos-sdk/pull/9226) Rename codec interfaces and methods, to follow a general Go interfaces:
  * `codec.Marshaler` → `codec.Codec` (this defines objects which serialize other objects)
  * `codec.BinaryMarshaler` → `codec.BinaryCodec`
  * `codec.JSONMarshaler` → `codec.JSONCodec`
  * Removed `BinaryBare` suffix from `BinaryCodec` methods (`MarshalBinaryBare`, `UnmarshalBinaryBare`, ...)
  * Removed `Binary` infix from `BinaryCodec` methods (`MarshalBinaryLengthPrefixed`, `UnmarshalBinaryLengthPrefixed`, ...)
