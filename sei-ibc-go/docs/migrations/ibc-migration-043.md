# Migrating to ibc-go

This file contains information on how to migrate from the IBC module contained in the SDK 0.41.x line to the IBC module in the ibc-go repository based on the 0.43 SDK version. 

## Import Changes

The most obvious changes is import name changes. We need to change:
- applications -> apps
- cosmos-sdk/x/ibc -> ibc-go

On my GNU/Linux based machine I used the following commands, executed in order:

`grep -RiIl 'cosmos-sdk\/x\/ibc\/applications' | xargs sed -i 's/cosmos-sdk\/x\/ibc\/applications/ibc-go\/modules\/apps/g'`

`grep -RiIl 'cosmos-sdk\/x\/ibc' | xargs sed -i 's/cosmos-sdk\/x\/ibc/ibc-go\/modules/g'`

ref: [explanation of the above commands](https://www.internalpointers.com/post/linux-find-and-replace-text-multiple-files)

Executing these commands out of order will cause issues. 

Feel free to use your own method for modifying import names.

NOTE: Updating to the `v0.43.0` SDK release and then running `go mod tidy` will cause a downgrade to `v0.42.0` in order to support the old IBC import paths.
Update the import paths before running `go mod tidy`.  

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

The `UpdateClient` has been modified to take in two client-identifiers and one initial height. Please see the [documentation](../proposals.md) for more information. 

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

The gRPC querier service endpoints have changed slightly. The previous files used `v1beta1`, this has been updated to `v1`.

## IBC callback changes

### OnRecvPacket

Application developers need to update their `OnRecvPacket` callback logic. 

The `OnRecvPacket` callback has been modified to only return the acknowledgement. The acknowledgement returned must implement the `Acknowledgement` interface. The acknowledgement should indicate if it represents a successful processing of a packet by returning true on `Success()` and false in all other cases. A return value of false on `Success()` will result in all state changes which occurred in the callback being discarded. More information can be found in the [documentation](https://github.com/cosmos/ibc-go/blob/main/docs/custom.md#receiving-packets).

## IBC Event changes

The `packet_data` attribute has been deprecated in favor of `packet_data_hex`, in order to provide standardized encoding/decoding of packet data in events. While the `packet_data` event still exists, all relayers and IBC Event consumers are strongly encouraged to switch over to using `packet_data_hex` as soon as possible.
