# Migrating from v2.0.0 to v3.0.0

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

ICS27 Interchain Accounts has been added as a supported IBC application of ibc-go.

## IBC Apps

Previously, IBC module callbacks were apart of the `AppModule` type. 
The recommended approach is to create an `IBCModule` type and move the IBC module callbacks from `AppModule` to `IBCModule` in a separate file `ibc_module.go`. 

The mock module go API has been broken in this release by applying the above format. 
The IBC module callbacks have been moved from the mock modules `AppModule` into a new type `IBCModule`. 

As apart of this release, the mock module now supports middleware testing. Please see the [README](../../testing/README.md#middleware-testing) for more information.

Please review the [mock](../../testing/mock/ibc_module.go) and [transfer](../../modules/apps/transfer/ibc_module.go) modules as examples. Additionally, [simapp](../../testing/simapp/app.go) provides an example of how `IBCModule` types should now be added to the IBC router in favour of `AppModule`.

## Relayers

- No relevant changes were made in this release.

## IBC Light Clients

- No relevant changes were made in this release.
