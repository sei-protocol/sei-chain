# Migrating from not supporing base denoms with slashes to supporting base denoms with slashes

This document is intended to highlight significant changes which may require more information than presented in the CHANGELOG.
Any changes that must be done by a user of ibc-go should be documented here.

There are four sections based on the four potential user groups of this document:
- Chains
- IBC Apps
- Relayers
- IBC Light Clients

This document is necessary when chains are upgrading from a version that does not support base denoms with slashes (e.g. v3.0.0) to a version that does (e.g. v3.1.0). All versions of ibc-go smaller than v1.5.0 for the v1.x release line, v2.3.0 for the v2.x release line, and v3.1.0 for the v3.x release line do *NOT** support IBC token transfers of coins whose base denoms contain slashes. Therefore the in-place of genesis migration described in this document are required when upgrading.

If a chain receives coins of a base denom with slashes before it upgrades to supporting it, the receive may pass however the trace information will be incorrect.

E.g. If a base denom of `testcoin/testcoin/testcoin` is sent to a chain that does not support slashes in the base denom, the receive will be successful. However, the trace information stored on the receiving chain will be: `Trace: "transfer/{channel-id}/testcoin/testcoin", BaseDenom: "testcoin"`.

This incorrect trace information must be corrected when the chain does upgrade to fully supporting denominations with slashes.

To do so, chain binaries should include a migration script that will run when the chain upgrades from not supporting base denominations with slashes to supporting base denominations with slashes.

## Chains

### ICS20 - Transfer

The transfer module will now support slashes in base denoms, so we must iterate over current traces to check if any of them are incorrectly formed and correct the trace information.

### Upgrade Proposal

```go
// Here the upgrade name is the upgrade name set by the chain
app.UpgradeKeeper.SetUpgradeHandler("supportSlashedDenomsUpgrade",
    func(ctx sdk.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
        // list of traces that must replace the old traces in store
        var newTraces []ibctransfertypes.DenomTrace
        app.TransferKeeper.IterateDenomTraces(ctx,
            func(dt ibctransfertypes.DenomTrace) bool {
                // check if the new way of splitting FullDenom
                // into Trace and BaseDenom passes validation and
                // is the same as the current DenomTrace.
                // If it isn't then store the new DenomTrace in the list of new traces.
                newTrace := ibctransfertypes.ParseDenomTrace(dt.GetFullDenomPath())
                if err := newTrace.Validate(); err == nil && !reflect.DeepEqual(newTrace, dt) {
                    newTraces = append(newTraces, newTrace)
                }

                return false
            })

        // replace the outdated traces with the new trace information
        for _, nt := range newTraces {
            app.TransferKeeper.SetDenomTrace(ctx, nt)
        }

        return app.mm.RunMigrations(ctx, app.configurator, fromVM)
    })
```

This is only necessary if there are denom traces in the store with incorrect trace information from previously received coins that had a slash in the base denom. However, it is recommended that any chain upgrading to support base denominations with slashes runs this code for safety.

For a more detailed sample, please check out the code changes in [this pull request](https://github.com/cosmos/ibc-go/pull/1527).

### Genesis Migration

If the chain chooses to add support for slashes in base denoms via genesis export, then the trace information must be corrected during genesis migration.

The migration code required may look like:

```go
func migrateGenesisSlashedDenomsUpgrade(appState genutiltypes.AppMap, clientCtx client.Context, genDoc *tmtypes.GenesisDoc) (genutiltypes.AppMap, error) {
	if appState[ibctransfertypes.ModuleName] != nil {
		transferGenState := &ibctransfertypes.GenesisState{}
		clientCtx.Codec.MustUnmarshalJSON(appState[ibctransfertypes.ModuleName], transferGenState)

		substituteTraces := make([]ibctransfertypes.DenomTrace, len(transferGenState.DenomTraces))
		for i, dt := range transferGenState.DenomTraces {
			// replace all previous traces with the latest trace if validation passes
			// note most traces will have same value
			newTrace := ibctransfertypes.ParseDenomTrace(dt.GetFullDenomPath())

			if err := newTrace.Validate(); err != nil {
				substituteTraces[i] = dt
			} else {
				substituteTraces[i] = newTrace
			}
		}

		transferGenState.DenomTraces = substituteTraces

		// delete old genesis state
		delete(appState, ibctransfertypes.ModuleName)

		// set new ibc transfer genesis state
		appState[ibctransfertypes.ModuleName] = clientCtx.Codec.MustMarshalJSON(transferGenState)
	}

	return appState, nil
}
```

For a more detailed sample, please check out the code changes in [this pull request](https://github.com/cosmos/ibc-go/pull/1528).