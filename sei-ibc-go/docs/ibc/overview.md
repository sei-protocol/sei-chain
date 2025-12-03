<!--
order: false
parent: 
  order: 1
-->

# Overview

Learn about IBC, its components, and IBC use cases. {synopsis}

## What is the Interblockchain Communication Protocol (IBC)?

This document serves as a guide for developers who want to write their own Inter-Blockchain
Communication protocol (IBC) applications for custom use cases.

> IBC applications must be written as self-contained modules. 

Due to the modular design of the IBC protocol, IBC
application developers do not need to be concerned with the low-level details of clients,
connections, and proof verification. 

This brief explanation of the lower levels of the
stack gives application developers a broad understanding of the IBC
protocol. Abstraction layer details for channels and ports are most relevant for application developers and describe how to define custom packets and `IBCModule` callbacks.

The requirements to have your module interact over IBC are: 

- Bind to a port or ports.
- Define your packet data.
- Use the default acknowledgment struct provided by core IBC or optionally define a custom acknowledgment struct.
- Standardize an encoding of the packet data.
- Implement the `IBCModule` interface.

Read on for a detailed explanation of how to write a self-contained IBC application module.

## Components Overview

### [Clients](https://github.com/cosmos/ibc-go/blob/main/modules/core/02-client)

IBC clients are on-chain light clients. Each light client is identified by a unique client-id. 
IBC clients track the consensus states of other blockchains, along with the proof spec necessary to 
properly verify proofs against the client's consensus state. A client can be associated with any number 
of connections to the counterparty chain. The client identifier is auto generated using the client type 
and the global client counter appended in the format: `{client-type}-{N}`. 

A `ClientState` should contain chain specific and light client specific information necessary for verifying updates
and upgrades to the IBC client. The `ClientState` may contain information such as chain-id, latest height, proof specs, 
unbonding periods or the status of the light client. The `ClientState` should not contain information that
is specific to a given block at a certain height, this is the function of the `CosnensusState`. Each `ConsensusState`
should be associated with a unique block and should be referenced using a height. IBC clients are given a 
client identifier prefixed store to store their associated client state and consensus states along with 
any metadata associated with the consensus states. Consensus states are stored using their associated height. 

The supported IBC clients are:

* [Solo Machine light client](https://github.com/cosmos/ibc-go/blob/main/modules/light-clients/06-solomachine): Devices such as phones, browsers, or laptops.
* [Tendermint light client](https://github.com/cosmos/ibc-go/blob/main/modules/light-clients/07-tendermint): The default for Cosmos SDK-based chains.
* [Localhost (loopback) client](https://github.com/cosmos/ibc-go/blob/main/modules/light-clients/09-localhost): Useful for
testing, simulation, and relaying packets to modules on the same application.

### IBC Client Heights

IBC Client Heights are represented by the struct:

```go
type Height struct {
   RevisionNumber uint64
   RevisionHeight uint64
}
```

The `RevisionNumber` represents the revision of the chain that the height is representing.
A revision typically represents a continuous, monotonically increasing range of block-heights.
The `RevisionHeight` represents the height of the chain within the given revision.

On any reset of the `RevisionHeight`—for example, when hard-forking a Tendermint chain—
the `RevisionNumber` will get incremented. This allows IBC clients to distinguish between a
block-height `n` of a previous revision of the chain (at revision `p`) and block-height `n` of the current
revision of the chain (at revision `e`).

`Height`s that share the same revision number can be compared by simply comparing their respective `RevisionHeight`s.
`Height`s that do not share the same revision number will only be compared using their respective `RevisionNumber`s.
Thus a height `h` with revision number `e+1` will always be greater than a height `g` with revision number `e`,
**REGARDLESS** of the difference in revision heights.

Ex:

```go
Height{RevisionNumber: 3, RevisionHeight: 0} > Height{RevisionNumber: 2, RevisionHeight: 100000000000}
```

When a Tendermint chain is running a particular revision, relayers can simply submit headers and proofs with the revision number
given by the chain's `chainID`, and the revision height given by the Tendermint block height. When a chain updates using a hard-fork 
and resets its block-height, it is responsible for updating its `chainID` to increment the revision number.
IBC Tendermint clients then verifies the revision number against their `chainID` and treat the `RevisionHeight` as the Tendermint block-height.

Tendermint chains wishing to use revisions to maintain persistent IBC connections even across height-resetting upgrades must format their `chainID`s
in the following manner: `{chainID}-{revision_number}`. On any height-resetting upgrade, the `chainID` **MUST** be updated with a higher revision number
than the previous value.

Ex:

- Before upgrade `chainID`: `gaiamainnet-3`
- After upgrade `chainID`: `gaiamainnet-4`

Clients that do not require revisions, such as the solo-machine client, simply hardcode `0` into the revision number whenever they
need to return an IBC height when implementing IBC interfaces and use the `RevisionHeight` exclusively.

Other client-types can implement their own logic to verify the IBC heights that relayers provide in their `Update`, `Misbehavior`, and
`Verify` functions respectively.

The IBC interfaces expect an `ibcexported.Height` interface, however all clients must use the concrete implementation provided in
`02-client/types` and reproduced above.

### [Connections](https://github.com/cosmos/ibc-go/blob/main/modules/core/03-connection)

Connections encapsulate two `ConnectionEnd` objects on two separate blockchains. Each
`ConnectionEnd` is associated with a client of the other blockchain (for example, the counterparty blockchain).
The connection handshake is responsible for verifying that the light clients on each chain are
correct for their respective counterparties. Connections, once established, are responsible for
facilitating all cross-chain verifications of IBC state. A connection can be associated with any
number of channels.

### [Proofs](https://github.com/cosmos/ibc-go/blob/main/modules/core/23-commitment) and [Paths](https://github.com/cosmos/ibc-go/blob/main/modules/core/24-host)
  
In IBC, blockchains do not directly pass messages to each other over the network. Instead, to
communicate, a blockchain commits some state to a specifically defined path that is reserved for a
specific message type and a specific counterparty. For example, for storing a specific connectionEnd as part
of a handshake or a packet intended to be relayed to a module on the counterparty chain. A relayer
process monitors for updates to these paths and relays messages by submitting the data stored
under the path and a proof to the counterparty chain. 

Proofs are passed from core IBC to light-clients as bytes. It is up to light client implementation to interpret these bytes appropriately.

- The paths that all IBC implementations must use for committing IBC messages is defined in
[ICS-24 Host State Machine Requirements](https://github.com/cosmos/ics/tree/master/spec/core/ics-024-host-requirements). 
- The proof format that all implementations must be able to produce and verify is defined in [ICS-23 Proofs](https://github.com/confio/ics23) implementation.

### [Capabilities](https://github.com/cosmos/cosmos-sdk/blob/main/docs/docs/core/10-ocap.md)

IBC is intended to work in execution environments where modules do not necessarily trust each
other. Thus, IBC must authenticate module actions on ports and channels so that only modules with the
appropriate permissions can use them. 

This module authentication is accomplished using a [dynamic
capability store](https://github.com/cosmos/cosmos-sdk/blob/master/docs/architecture/adr-003-dynamic-capability-store.md). Upon binding to a port or
creating a channel for a module, IBC returns a dynamic capability that the module must claim in
order to use that port or channel. The dynamic capability module prevents other modules from using that port or channel since
they do not own the appropriate capability.

While this background information is useful, IBC modules do not need to interact at all with
these lower-level abstractions. The relevant abstraction layer for IBC application developers is
that of channels and ports. IBC applications must be written as self-contained **modules**. 

A module on one blockchain can communicate with other modules on other blockchains by sending,
receiving, and acknowledging packets through channels that are uniquely identified by the
`(channelID, portID)` tuple. 

A useful analogy is to consider IBC modules as internet applications on
a computer. A channel can then be conceptualized as an IP connection, with the IBC portID being
analogous to an IP port and the IBC channelID being analogous to an IP address. Thus, a single
instance of an IBC module can communicate on the same port with any number of other modules and
IBC correctly routes all packets to the relevant module using the (channelID, portID tuple). An
IBC module can also communicate with another IBC module over multiple ports, with each
`(portID<->portID)` packet stream being sent on a different unique channel.

### [Ports](https://github.com/cosmos/ibc-go/blob/main/modules/core/05-port)

An IBC module can bind to any number of ports. Each port must be identified by a unique `portID`.
Since IBC is designed to be secure with mutually distrusted modules operating on the same ledger,
binding a port returns a dynamic object capability. In order to take action on a particular port
(for example, an open channel with its portID), a module must provide the dynamic object capability to the IBC
handler. This requirement prevents a malicious module from opening channels with ports it does not own. Thus,
IBC modules are responsible for claiming the capability that is returned on `BindPort`.

### [Channels](https://github.com/cosmos/ibc-go/blob/main/modules/core/04-channel)

An IBC channel can be established between two IBC ports. Currently, a port is exclusively owned by a
single module. IBC packets are sent over channels. Just as IP packets contain the destination IP
address and IP port, and the source IP address and source IP port, IBC packets contain
the destination portID and channelID, and the source portID and channelID. This packet structure enables IBC to
correctly route packets to the destination module while allowing modules receiving packets to
know the sender module.

A channel can be `ORDERED`, where packets from a sending module must be processed by the
receiving module in the order they were sent. Or a channel can be `UNORDERED`, where packets
from a sending module are processed in the order they arrive (might be in a different order than they were sent).

Modules can choose which channels they wish to communicate over with, thus IBC expects modules to
implement callbacks that are called during the channel handshake. These callbacks can do custom
channel initialization logic. If any callback returns an error, the channel handshake fails. Thus, by
returning errors on callbacks, modules can programmatically reject and accept channels.

The channel handshake is a 4-step handshake. Briefly, if a given chain A wants to open a channel with
chain B using an already established connection:

1. chain A sends a `ChanOpenInit` message to signal a channel initialization attempt with chain B.
2. chain B sends a `ChanOpenTry` message to try opening the channel on chain A.
3. chain A sends a `ChanOpenAck` message to mark its channel end status as open.
4. chain B sends a `ChanOpenConfirm` message to mark its channel end status as open.

If all handshake steps are successful, the channel is opened on both sides. At each step in the handshake, the module
associated with the `ChannelEnd` executes its callback. So
on `ChanOpenInit`, the module on chain A executes its callback `OnChanOpenInit`.

The channel identifier is auto derived in the format: `channel-{N}` where N is the next sequence to be used. 

Just as ports came with dynamic capabilities, channel initialization returns a dynamic capability
that the module **must** claim so that they can pass in a capability to authenticate channel actions
like sending packets. The channel capability is passed into the callback on the first parts of the
handshake; either `OnChanOpenInit` on the initializing chain or `OnChanOpenTry` on the other chain.

#### Closing channels

Closing a channel occurs in 2 handshake steps as defined in [ICS 04](https://github.com/cosmos/ibc/tree/master/spec/core/ics-004-channel-and-packet-semantics).

`ChanCloseInit` closes a channel on the executing chain if the channel exists, it is not 
already closed and the connection it exists upon is OPEN. Channels can only be closed by a 
calling module or in the case of a packet timeout on an ORDERED channel.

`ChanCloseConfirm` is a response to a counterparty channel executing `ChanCloseInit`. The channel
on the executing chain closes if the channel exists, the channel is not already closed, 
the connection the channel exists upon is OPEN and the executing chain successfully verifies
that the counterparty channel has been closed.


### [Packets](https://github.com/cosmos/ibc-go/blob/main/modules/core/04-channel)

Modules communicate with each other by sending packets over IBC channels. All
IBC packets contain the destination `portID` and `channelID` along with the source `portID` and
`channelID`. This packet structure allows modules to know the sender module of a given packet. IBC packets 
contain a sequence to optionally enforce ordering. 

IBC packets also contain a `TimeoutHeight` and a `TimeoutTimestamp` that determine the deadline before the receiving module must process a packet. 

Modules send custom application data to each other inside the `Data []byte` field of the IBC packet.
Thus, packet data is opaque to IBC handlers. It is incumbent on a sender module to encode
their application-specific packet information into the `Data` field of packets. The receiver
module must decode that `Data` back to the original application data.

### [Receipts and Timeouts](https://github.com/cosmos/ibc-go/blob/main/modules/core/04-channel)

Since IBC works over a distributed network and relies on potentially faulty relayers to relay messages between ledgers, 
IBC must handle the case where a packet does not get sent to its destination in a timely manner or at all. Packets must 
specify a non-zero value for timeout height (`TimeoutHeight`) or timeout timestamp (`TimeoutTimestamp` ) after which a packet can no longer be successfully received on the destination chain.

- The `timeoutHeight` indicates a consensus height on the destination chain after which the packet is no longer be processed, and instead counts as having timed-out.
- The `timeoutTimestamp` indicates a timestamp on the destination chain after which the packet is no longer be processed, and instead counts as having timed-out.

If the timeout passes without the packet being successfully received, the packet can no longer be
received on the destination chain. The sending module can timeout the packet and take appropriate actions.

If the timeout is reached, then a proof of packet timeout can be submitted to the original chain. The original chain can then perform 
application-specific logic to timeout the packet, perhaps by rolling back the packet send changes (refunding senders any locked funds, etc.).

- In ORDERED channels, a timeout of a single packet in the channel causes the channel to close. 

    - If packet sequence `n` times out, then a packet at sequence `k > n` cannot be received without violating the contract of ORDERED channels that packets are processed in the order that they are sent. 
    - Since ORDERED channels enforce this invariant, a proof that sequence `n` has not been received on the destination chain by the specified timeout of packet `n` is sufficient to timeout packet `n` and close the channel.

- In UNORDERED channels, the application-specific timeout logic for that packet is applied and the channel is not closed.

    - Packets can be received in any order. 

    - IBC writes a packet receipt for each sequence receives in the UNORDERED channel. This receipt does not contain information; it is simply a marker intended to signify that the UNORDERED channel has received a packet at the specified sequence. 

    - To timeout a packet on an UNORDERED channel, a proof is required that a packet receipt **does not exist** for the packet's sequence by the specified timeout.  

For this reason, most modules should use UNORDERED channels as they require fewer liveness guarantees to function effectively for users of that channel.

### [Acknowledgments](https://github.com/cosmos/ibc-go/blob/main/modules/core/04-channel)

Modules can also choose to write application-specific acknowledgments upon processing a packet. Acknowledgments can be done:

- Synchronously on `OnRecvPacket` if the module processes packets as soon as they are received from IBC module. 
- Asynchronously if module processes packets at some later point after receiving the packet.

This acknowledgment data is opaque to IBC much like the packet `Data` and is treated by IBC as a simple byte string `[]byte`. Receiver modules must encode their acknowledgment so that the sender module can decode it correctly. The encoding must be negotiated between the two parties during version negotiation in the channel handshake. 

The acknowledgment can encode whether the packet processing succeeded or failed, along with additional information that allows the sender module to take appropriate action.

After the acknowledgment has been written by the receiving chain, a relayer relays the acknowledgment back to the original sender module.

The original sender module then executes application-specific acknowledgment logic using the contents of the acknowledgment. 

- After an acknowledgement fails, packet-send changes can be rolled back (for example, refunding senders in ICS20).

- After an acknowledgment is received successfully on the original sender on the chain, the corresponding packet commitment is deleted since it is no longer needed.

## Further Readings and Specs

If you want to learn more about IBC, check the following specifications:

* [IBC specification overview](https://github.com/cosmos/ibc/blob/master/README.md)

## Next {hide}

Learn about how to [integrate](./integration.md) IBC to your application {hide}
