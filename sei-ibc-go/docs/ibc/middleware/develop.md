<!--
order: 1
-->

# IBC Middleware

Learn how to write your own custom middleware to wrap an IBC application, and understand how to hook different middleware to IBC base applications to form different IBC application stacks {synopsis}.

This document serves as a guide for middleware developers who want to write their own middleware and for chain developers who want to use IBC middleware on their chains.

IBC applications are designed to be self-contained modules that implement their own application-specific logic through a set of interfaces with the core IBC handlers. These core IBC handlers, in turn, are designed to enforce the correctness properties of IBC (transport, authentication, ordering) while delegating all application-specific handling to the IBC application modules. However, there are cases where some functionality may be desired by many applications, yet not appropriate to place in core IBC.

Middleware allows developers to define the extensions as separate modules that can wrap over the base application. This middleware can thus perform its own custom logic, and pass data into the application so that it may run its logic without being aware of the middleware's existence. This allows both the application and the middleware to implement its own isolated logic while still being able to run as part of a single packet flow.

## Pre-requisite Readings

- [IBC Overview](../overview.md) {prereq}
- [IBC Integration](../integration.md) {prereq}
- [IBC Application Developer Guide](../apps.md) {prereq}

## Definitions

`Middleware`: A self-contained module that sits between core IBC and an underlying IBC application during packet execution. All messages between core IBC and underlying application must flow through middleware, which may perform its own custom logic.

`Underlying Application`: An underlying application is the application that is directly connected to the middleware in question. This underlying application may itself be middleware that is chained to a base application.

`Base Application`: A base application is an IBC application that does not contain any middleware. It may be nested by 0 or multiple middleware to form an application stack.

`Application Stack (or stack)`: A stack is the complete set of application logic (middleware(s) +  base application) that gets connected to core IBC. A stack may be just a base application, or it may be a series of middlewares that nest a base application.

## Create a custom IBC Middleware

IBC Middleware will wrap over an underlying IBC application and sits between core IBC and the application. It has complete control in modifying any message coming from IBC to the application, and any message coming from the application to core IBC. Thus, middleware must be completely trusted by chain developers who wish to integrate them, however this gives them complete flexibility in modifying the application(s) they wrap.

#### Interfaces

```go
// Middleware implements the ICS26 Module interface
type Middleware interface {
    porttypes.IBCModule // middleware has acccess to an underlying application which may be wrapped by more middleware
    ics4Wrapper: ICS4Wrapper // middleware has access to ICS4Wrapper which may be core IBC Channel Handler or a higher-level middleware that wraps this middleware.
}
```

```typescript
// This is implemented by ICS4 and all middleware that are wrapping base application.
// The base application will call `sendPacket` or `writeAcknowledgement` of the middleware directly above them
// which will call the next middleware until it reaches the core IBC handler.
type ICS4Wrapper interface {
    SendPacket(ctx sdk.Context, chanCap *capabilitytypes.Capability, packet exported.Packet) error
    WriteAcknowledgement(ctx sdk.Context, chanCap *capabilitytypes.Capability, packet exported.Packet, ack []byte) error
}
```

### Implement `IBCModule` interface and callbacks

The IBCModule is struct that implements the ICS26Interface (`porttypes.IBCModule`). It is recommended to separate these callbacks into a separate file `ibc_module.go`. As will be mentioned in the [integration doc](./integration.md), this struct should be different than the struct that implements `AppModule` in case the middleware maintains its own internal state and processes separate SDK messages.

The middleware must have access to the underlying application, and be called before during all ICS-26 callbacks. It may execute custom logic during these callbacks, and then call the underlying application's callback. Middleware **may** choose not to call the underlying application's callback at all. Though these should generally be limited to error cases.

In the case where the IBC middleware expects to speak to a compatible IBC middleware on the counterparty chain; they must use the channel handshake to negotiate the middleware version without interfering in the version negotiation of the underlying application.

Middleware accomplishes this by formatting the version in the following format: `{mw-version}:{app-version}`.

During the handshake callbacks, the middleware can split the version into: `mw-version`, `app-version`. It can do its negotiation logic on `mw-version`, and pass the `app-version` to the underlying application.

The middleware should simply pass the capability in the callback arguments along to the underlying application so that it may be claimed by the base application. The base application will then pass the capability up the stack in order to authenticate an outgoing packet/acknowledgement.

In the case where the middleware wishes to send a packet or acknowledgment without the involvement of the underlying application, it should be given access to the same `scopedKeeper` as the base application so that it can retrieve the capabilities by itself.

### Handshake callbacks

```go
func (im IBCModule) OnChanOpenInit(ctx sdk.Context,
    order channeltypes.Order,
    connectionHops []string,
    portID string,
    channelID string,
    channelCap *capabilitytypes.Capability,
    counterparty channeltypes.Counterparty,
    version string,
) error {
    // core/04-channel/types contains a helper function to split middleware and underlying app version
    middlewareVersion, appVersion = channeltypes.SplitChannelVersion(version)
    doCustomLogic()
    im.app.OnChanOpenInit(
        ctx,
        order,
        connectionHops,
        portID,
        channelID,
        channelCap,
        counterparty,
        appVersion, // note we only pass app version here
    )
}

func OnChanOpenTry(
    ctx sdk.Context,
    order channeltypes.Order,
    connectionHops []string,
    portID,
    channelID string,
    channelCap *capabilitytypes.Capability,
    counterparty channeltypes.Counterparty,
    counterpartyVersion string,
) (string, error) {
    doCustomLogic()

    // core/04-channel/types contains a helper function to split middleware and underlying app version
    cpMiddlewareVersion, cpAppVersion = channeltypes.SplitChannelVersion(counterpartyVersion)

    // call the underlying applications OnChanOpenTry callback
    appVersion, err := app.OnChanOpenTry(
        ctx,
        order,
        connectionHops,
        portID,
        channelID,
        channelCap,
        counterparty,
        cpAppVersion, // note we only pass counterparty app version here
    )
    if err != nil {
        return err
    }
    
    middlewareVersion := negotiateMiddlewareVersion(cpMiddlewareVersion)
    version := constructVersion(middlewareVersion, appVersion)

    return version
}

func OnChanOpenAck(
    ctx sdk.Context,
    portID,
    channelID string,
    counterpartyVersion string,
) error {
    // core/04-channel/types contains a helper function to split middleware and underlying app version
    middlewareVersion, appVersion = channeltypes.SplitChannelVersion(version)
    if !isCompatible(middlewareVersion) {
        return error
    }
    doCustomLogic()
      
    // call the underlying applications OnChanOpenTry callback
    app.OnChanOpenAck(ctx, portID, channelID, appVersion)
}

func OnChanOpenConfirm(
    ctx sdk.Context,
    portID,
    channelID string,
) error {
    doCustomLogic()

    app.OnChanOpenConfirm(ctx, portID, channelID)
}

OnChanCloseInit(
    ctx sdk.Context,
    portID,
    channelID string,
) error {
    doCustomLogic()

    app.OnChanCloseInit(ctx, portID, channelID)
}

OnChanCloseConfirm(
    ctx sdk.Context,
    portID,
    channelID string,
) error {
    doCustomLogic()

    app.OnChanCloseConfirm(ctx, portID, channelID)
}
```

NOTE: Middleware that does not need to negotiate with a counterparty middleware on the remote stack will not implement the version splitting and negotiation, and will simply perform its own custom logic on the callbacks without relying on the counterparty behaving similarly.

### Packet callbacks

The packet callbacks just like the handshake callbacks wrap the application's packet callbacks. The packet callbacks are where the middleware performs most of its custom logic. The middleware may read the packet flow data and perform some additional packet handling, or it may modify the incoming data before it reaches the underlying application. This enables a wide degree of usecases, as a simple base application like token-transfer can be transformed for a variety of usecases by combining it with custom middleware.

```go
OnRecvPacket(
    ctx sdk.Context,
    packet channeltypes.Packet,
) ibcexported.Acknowledgement {
    doCustomLogic(packet)

    ack := app.OnRecvPacket(ctx, packet)

    doCustomLogic(ack) // middleware may modify outgoing ack
    return ack
}

OnAcknowledgementPacket(
    ctx sdk.Context,
    packet channeltypes.Packet,
    acknowledgement []byte,
) (*sdk.Result, error) {
    doCustomLogic(packet, ack)

    app.OnAcknowledgementPacket(ctx, packet, ack)
}

OnTimeoutPacket(
    ctx sdk.Context,
    packet channeltypes.Packet,
) (*sdk.Result, error) {
    doCustomLogic(packet)

    app.OnTimeoutPacket(ctx, packet)
}
```

### ICS-4 Wrappers

Middleware must also wrap ICS-4 so that any communication from the application to the channelKeeper goes through the middleware first. Similar to the packet callbacks, the middleware may modify outgoing acknowledgements and packets in any way it wishes.

```go
// only called for async acks
func WriteAcknowledgement(
  packet channeltypes.Packet,
  acknowledgement []bytes) {
    // middleware may modify acknowledgement
    ack_bytes = doCustomLogic(acknowledgement)

    return ics4Keeper.WriteAcknowledgement(packet, ack_bytes)
}

func SendPacket(appPacket channeltypes.Packet) {
    // middleware may modify packet
    packet = doCustomLogic(app_packet)

    return ics4Keeper.SendPacket(packet)
}
```
