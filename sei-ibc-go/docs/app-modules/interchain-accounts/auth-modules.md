<!--
order: 2
-->

# Building an authentication module

Authentication modules play the role of the `Base Application` as described in [ICS30 IBC Middleware](https://github.com/cosmos/ibc/tree/master/spec/app/ics-030-middleware), and enable application developers to perform custom logic when working with the Interchain Accounts controller API. {synopsis}

The controller submodule is used for account registration and packet sending. 
It executes only logic required of all controllers of interchain accounts. 
The type of authentication used to manage the interchain accounts remains unspecified. 
There may exist many different types of authentication which are desirable for different use cases. 
Thus the purpose of the authentication module is to wrap the controller module with custom authentication logic.

In ibc-go, authentication modules are connected to the controller chain via a middleware stack.
The controller module is implemented as [middleware](https://github.com/cosmos/ibc/tree/master/spec/app/ics-030-middleware) and the authentication module is connected to the controller module as the base application of the middleware stack. 
To implement an authentication module, the `IBCModule` interface must be fulfilled. 
By implementing the controller module as middleware, any amount of authentication modules can be created and connected to the controller module without writing redundant code. 

The authentication module must:
- Authenticate interchain account owners
- Track the associated interchain account address for an owner
- Claim the channel capability in `OnChanOpenInit`
- Send packets on behalf of an owner (after authentication)

### IBCModule implementation

The following `IBCModule` callbacks must be implemented with appropriate custom logic:

```go
// OnChanOpenInit implements the IBCModule interface
func (im IBCModule) OnChanOpenInit(
    ctx sdk.Context,
    order channeltypes.Order,
    connectionHops []string,
    portID string,
    channelID string,
    chanCap *capabilitytypes.Capability,
    counterparty channeltypes.Counterparty,
    version string,
) error {
    // the authentication module *must* claim the channel capability on OnChanOpenInit
    if err := im.keeper.ClaimCapability(ctx, chanCap, host.ChannelCapabilityPath(portID, channelID)); err != nil {
        return err
    }

    // perform custom logic

    return nil
}

// OnChanOpenAck implements the IBCModule interface
func (im IBCModule) OnChanOpenAck(
    ctx sdk.Context,
    portID,
    channelID string,
    counterpartyVersion string,
) error {
    // perform custom logic

    return nil
}

// OnChanCloseConfirm implements the IBCModule interface
func (im IBCModule) OnChanCloseConfirm(
    ctx sdk.Context,
    portID,
    channelID string,
) error {
    // perform custom logic

    return nil
}

// OnAcknowledgementPacket implements the IBCModule interface
func (im IBCModule) OnAcknowledgementPacket(
    ctx sdk.Context,
    packet channeltypes.Packet,
    acknowledgement []byte,
    relayer sdk.AccAddress,
) error {
    // perform custom logic

    return nil
}

// OnTimeoutPacket implements the IBCModule interface.
func (im IBCModule) OnTimeoutPacket(
    ctx sdk.Context,
    packet channeltypes.Packet,
    relayer sdk.AccAddress,
) error {
    // perform custom logic

    return nil
}
```

**Note**: The channel capability must be claimed by the authentication module in `OnChanOpenInit` otherwise the authentication module will not be able to send packets on the channel created for the associated interchain account. 

The following functions must be defined to fulfill the `IBCModule` interface, but they will never be called by the controller module so they may error or panic.

```go
// OnChanOpenTry implements the IBCModule interface
func (im IBCModule) OnChanOpenTry(
    ctx sdk.Context,
    order channeltypes.Order,
    connectionHops []string,
    portID,
    channelID string,
    chanCap *capabilitytypes.Capability,
    counterparty channeltypes.Counterparty,
    counterpartyVersion string,
) (string, error) {
    panic("UNIMPLEMENTED")
}

// OnChanOpenConfirm implements the IBCModule interface
func (im IBCModule) OnChanOpenConfirm(
    ctx sdk.Context,
    portID,
    channelID string,
) error {
    panic("UNIMPLEMENTED")
}

// OnChanCloseInit implements the IBCModule interface
func (im IBCModule) OnChanCloseInit(
    ctx sdk.Context,
    portID,
    channelID string,
) error {
    panic("UNIMPLEMENTED")
}

// OnRecvPacket implements the IBCModule interface. A successful acknowledgement
// is returned if the packet data is succesfully decoded and the receive application
// logic returns without error.
func (im IBCModule) OnRecvPacket(
    ctx sdk.Context,
    packet channeltypes.Packet,
    relayer sdk.AccAddress,
) ibcexported.Acknowledgement {
    panic("UNIMPLEMENTED")
}
```

## `RegisterInterchainAccount`

The authentication module can begin registering interchain accounts by calling `RegisterInterchainAccount`:

```go
if err := keeper.icaControllerKeeper.RegisterInterchainAccount(ctx, connectionID, owner.String()); err != nil {
    return err
}

return nil
```

## `SendTx`

The authentication module can attempt to send a packet by calling `SendTx`:
```go

// Authenticate owner
// perform custom logic
    
// Construct controller portID based on interchain account owner address
portID, err := icatypes.NewControllerPortID(owner.String())
if err != nil {
    return err
}

channelID, found := keeper.icaControllerKeeper.GetActiveChannelID(ctx, portID)
if !found {
    return sdkerrors.Wrapf(icatypes.ErrActiveChannelNotFound, "failed to retrieve active channel for port %s", portID)
}
    
// Obtain the channel capability, claimed in OnChanOpenInit
chanCap, found := keeper.scopedKeeper.GetCapability(ctx, host.ChannelCapabilityPath(portID, channelID))
if !found {
    return sdkerrors.Wrap(channeltypes.ErrChannelCapabilityNotFound, "module does not own channel capability")
}
    
// Obtain data to be sent to the host chain. 
// In this example, the owner of the interchain account would like to send a bank MsgSend to the host chain. 
// The appropriate serialization function should be called. The host chain must be able to deserialize the transaction. 
// If the host chain is using the ibc-go host module, `SerializeCosmosTx` should be used. 
msg := &banktypes.MsgSend{FromAddress: fromAddr, ToAddress: toAddr, Amount: amt}
data, err := icatypes.SerializeCosmosTx(keeper.cdc, []sdk.Msg{msg})
if err != nil {
    return err
}

// Construct packet data
packetData := icatypes.InterchainAccountPacketData{
    Type: icatypes.EXECUTE_TX,
    Data: data,
}

// Obtain timeout timestamp
// An appropriate timeout timestamp must be determined based on the usage of the interchain account.
// If the packet times out, the channel will be closed requiring a new channel to be created 
timeoutTimestamp := obtainTimeoutTimestamp()

// Send the interchain accounts packet, returning the packet sequence
seq, err = keeper.icaControllerKeeper.SendTx(ctx, chanCap, portID, packetData, timeoutTimestamp)
```

The data within an `InterchainAccountPacketData` must be serialized using a format supported by the host chain. 
If the host chain is using the ibc-go host chain submodule, `SerializeCosmosTx` should be used. If the `InterchainAccountPacketData.Data` is serialized using a format not support by the host chain, the packet will not be successfully received.  

## `OnAcknowledgementPacket`

Controller chains will be able to access the acknowledgement written into the host chain state once a relayer relays the acknowledgement. 
The acknowledgement bytes will be passed to the auth module via the `OnAcknowledgementPacket` callback. 
Auth modules are expected to know how to decode the acknowledgement. 

If the controller chain is connected to a host chain using the host module on ibc-go, it may interpret the acknowledgement bytes as follows:

Begin by unmarshaling the acknowledgement into sdk.TxMsgData:
```go
txMsgData := &sdk.TxMsgData{}
if err := proto.Unmarshal(ack.Acknowledgement(), txMsgData); err != nil {
    return err
}
```

If the txMsgData.Data field is non nil, the host chain is using SDK version <= v0.45. 
The auth module should interpret the txMsgData.Data as follows:

```go
switch len(txMsgData.Data) {
case 0:
    for _, msgData := range txMsgData.Data {
        if err := handler(msgData); err != nil {
            return err
        }
    }
...
}            
```

A handler will be needed to interpret what actions to perform based on the message type sent.
A router could be used, or more simply a switch statement.

```go
func handler(msgData sdk.MsgData) error {
switch msgData.TypeURL {
case banktypes.MsgSend:
    msgResponse := &banktypes.MsgSendResponse{}
    if err := proto.Unmarshal(msgData.Data, msgResponse}; err != nil {
        return err
    }

    handleBankSendMsg(msgResponse)

case stakingtypes.MsgDelegate:
    msgResponse := &stakingtypes.MsgDelegateResponse{}
    if err := proto.Unmarshal(msgData.Data, msgResponse}; err != nil {
        return err
    }

    handleStakingDelegateMsg(msgResponse)

case transfertypes.MsgTransfer:
    msgResponse := &transfertypes.MsgTransferResponse{}
    if err := proto.Unmarshal(msgData.Data, msgResponse}; err != nil {
        return err
    }

    handleIBCTransferMsg(msgResponse)
 
default:
    return
}
```

If the txMsgData.Data is empty, the host chain is using SDK version > v0.45.
The auth module should interpret the txMsgData.Responses as follows:

```go
...
// switch statement from above continued
default:
    for _, any := range txMsgData.MsgResponses {
        if err := handleAny(any); err != nil {
            return err
        }
    }
}
``` 

A handler will be needed to interpret what actions to perform based on the type url of the Any. 
A router could be used, or more simply a switch statement. 
It may be possible to deduplicate logic between `handler` and `handleAny`.

```go
func handleAny(any *codectypes.Any) error {
switch any.TypeURL {
case banktypes.MsgSend:
    msgResponse, err := unpackBankMsgSendResponse(any)
    if err != nil {
        return err
    }

    handleBankSendMsg(msgResponse)

case stakingtypes.MsgDelegate:
    msgResponse, err := unpackStakingDelegateResponse(any)
    if err != nil {
        return err
    }

    handleStakingDelegateMsg(msgResponse)

    case transfertypes.MsgTransfer:
    msgResponse, err := unpackIBCTransferMsgResponse(any)
    if err != nil {
        return err
    }

    handleIBCTransferMsg(msgResponse)
 
default:
    return
}
```

### Integration into `app.go` file

To integrate the authentication module into your chain, please follow the steps outlined above in [app.go integration](./integration.md#example-integration).
