# Migrating from ibc-go v3 to v4

This document is intended to highlight significant changes which may require more information than presented in the CHANGELOG.
Any changes that must be done by a user of ibc-go should be documented here.

There are four sections based on the four potential user groups of this document:
- Chains
- IBC Apps
- Relayers
- IBC Light Clients

**Note:** ibc-go supports golang semantic versioning and therefore all imports must be updated to bump the version number on major releases.
```go
github.com/cosmos/ibc-go/v3 -> github.com/cosmos/ibc-go/v4
```

No genesis or in-place migrations required when upgrading from v1 or v2 of ibc-go.

## Chains

### IS04 - Channel 

The `WriteAcknowledgement` API now takes the `exported.Acknowledgement` type instead of passing in the acknowledgement byte array directly. 
This is an API breaking change and as such IBC application developers will have to update any calls to `WriteAcknowledgement`. 

The `OnChanOpenInit` application callback has been modified.
The return signature now includes the application version as detailed in the latest IBC [spec changes](https://github.com/cosmos/ibc/pull/629).

## Relayers

When using the `DenomTrace` gRPC, the full IBC denomination with the `ibc/` prefix may now be passed in.
