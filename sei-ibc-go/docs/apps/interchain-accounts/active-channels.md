<!--
order: 3
-->

# Understanding Active Channels 

The Interchain Accounts module uses [ORDERED channels](https://github.com/cosmos/ibc/tree/master/spec/core/ics-004-channel-and-packet-semantics#ordering) to maintain the order of transactions when sending packets from a controller to a host chain. A limitation when using ORDERED channels is that when a packet times out the channel will be closed. 

In the case of a channel closing, a controller chain needs to be able to regain access to the interchain account registered on this channel. `Active Channels` enable this functionality. Future versions of the ICS-27 protocol and the Interchain Accounts module will likely use a new 
channel type that provides ordering of packets without the channel closing on timing out, thus removing the need for `Active Channels` entirely.  

When an Interchain Account is registered using the `RegisterInterchainAccount` API, a new channel is created on a particular port. During the `OnChanOpenAck` and `OnChanOpenConfirm` steps (controller & host chain) the `Active Channel` for this interchain account
is stored in state.

It is possible to create a new channel using the same controller chain portID if the previously set `Active Channel` is now in a `CLOSED` state. This channel creation can be initialized programatically by sending a new `OnChanOpenInit` message like so:

```go
	msg := channeltypes.NewMsgChannelOpenInit(portID, string(versionBytes), channeltypes.ORDERED, []string{connectionID}, icatypes.PortID, icatypes.ModuleName)
	handler := k.msgRouter.Handler(msg)
```

Alternatively, any relayer operator may initiate a new channel handshake for this interchain account once the previously set `Active Channel` is in a `CLOSED` state. This is done by initiating the channel handshake on the controller chain using the same portID associated with the interchain account in question.  

It is important to note that once a channel has been opened for a given Interchain Account, new channels can not be opened for this account until the currently set `Active Channel` is set to `CLOSED`. 

