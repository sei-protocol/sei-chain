# Migrating from ibc-go v1 to v2

This document is intended to highlight significant changes which may require more information than presented in the CHANGELOG.
Any changes that must be done by a user of ibc-go should be documented here.

There are four sections based on the four potential user groups of this document:
- Chains
- IBC Apps
- Relayers
- IBC Light Clients

**Note:** ibc-go supports golang semantic versioning and therefore all imports must be updated to bump the version number on major releases.
```go
github.com/cosmos/ibc-go -> github.com/cosmos/ibc-go/v2
```

## Chains

- No relevant changes were made in this release.

## IBC Apps

A new function has been added to the app module interface:
```go
// NegotiateAppVersion performs application version negotiation given the provided channel ordering, connectionID, portID, counterparty and proposed version.
    // An error is returned if version negotiation cannot be performed. For example, an application module implementing this interface
    // may decide to return an error in the event of the proposed version being incompatible with it's own
    NegotiateAppVersion(
        ctx sdk.Context,
        order channeltypes.Order,
        connectionID string,
        portID string,
        counterparty channeltypes.Counterparty,
        proposedVersion string,
    ) (version string, err error)
}
```

This function should perform application version negotiation and return the negotiated version. If the version cannot be negotiated, an error should be returned. This function is only used on the client side.

#### sdk.Result removed

sdk.Result has been removed as a return value in the application callbacks. Previously it was being discarded by core IBC and was thus unused.

## Relayers

A new gRPC has been added to 05-port, `AppVersion`. It returns the negotiated app version. This function should be used for the `ChanOpenTry` channel handshake step to decide upon the application version which should be set in the channel.

## IBC Light Clients

- No relevant changes were made in this release.
