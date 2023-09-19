<!-- This file is auto-generated. Please do not modify it yourself. -->
# Protobuf Documentation
<a name="top"></a>

## Table of Contents

- [ibc/applications/interchain_accounts/v1/account.proto](#ibc/applications/interchain_accounts/v1/account.proto)
    - [InterchainAccount](#ibc.applications.interchain_accounts.v1.InterchainAccount)
  
- [ibc/applications/interchain_accounts/v1/genesis.proto](#ibc/applications/interchain_accounts/v1/genesis.proto)
    - [ActiveChannel](#ibc.applications.interchain_accounts.v1.ActiveChannel)
    - [ControllerGenesisState](#ibc.applications.interchain_accounts.v1.ControllerGenesisState)
    - [GenesisState](#ibc.applications.interchain_accounts.v1.GenesisState)
    - [HostGenesisState](#ibc.applications.interchain_accounts.v1.HostGenesisState)
    - [RegisteredInterchainAccount](#ibc.applications.interchain_accounts.v1.RegisteredInterchainAccount)
  
- [ibc/applications/interchain_accounts/v1/metadata.proto](#ibc/applications/interchain_accounts/v1/metadata.proto)
    - [Metadata](#ibc.applications.interchain_accounts.v1.Metadata)
  
- [ibc/applications/interchain_accounts/v1/packet.proto](#ibc/applications/interchain_accounts/v1/packet.proto)
    - [CosmosTx](#ibc.applications.interchain_accounts.v1.CosmosTx)
    - [InterchainAccountPacketData](#ibc.applications.interchain_accounts.v1.InterchainAccountPacketData)
  
    - [Type](#ibc.applications.interchain_accounts.v1.Type)
  
- [ibc/applications/transfer/v1/transfer.proto](#ibc/applications/transfer/v1/transfer.proto)
    - [DenomTrace](#ibc.applications.transfer.v1.DenomTrace)
    - [Params](#ibc.applications.transfer.v1.Params)
  
- [ibc/applications/transfer/v1/genesis.proto](#ibc/applications/transfer/v1/genesis.proto)
    - [GenesisState](#ibc.applications.transfer.v1.GenesisState)
  
- [ibc/applications/transfer/v1/query.proto](#ibc/applications/transfer/v1/query.proto)
    - [QueryDenomHashRequest](#ibc.applications.transfer.v1.QueryDenomHashRequest)
    - [QueryDenomHashResponse](#ibc.applications.transfer.v1.QueryDenomHashResponse)
    - [QueryDenomTraceRequest](#ibc.applications.transfer.v1.QueryDenomTraceRequest)
    - [QueryDenomTraceResponse](#ibc.applications.transfer.v1.QueryDenomTraceResponse)
    - [QueryDenomTracesRequest](#ibc.applications.transfer.v1.QueryDenomTracesRequest)
    - [QueryDenomTracesResponse](#ibc.applications.transfer.v1.QueryDenomTracesResponse)
    - [QueryEscrowAddressRequest](#ibc.applications.transfer.v1.QueryEscrowAddressRequest)
    - [QueryEscrowAddressResponse](#ibc.applications.transfer.v1.QueryEscrowAddressResponse)
    - [QueryParamsRequest](#ibc.applications.transfer.v1.QueryParamsRequest)
    - [QueryParamsResponse](#ibc.applications.transfer.v1.QueryParamsResponse)
  
    - [Query](#ibc.applications.transfer.v1.Query)
  
- [ibc/core/client/v1/client.proto](#ibc/core/client/v1/client.proto)
    - [ClientConsensusStates](#ibc.core.client.v1.ClientConsensusStates)
    - [ClientUpdateProposal](#ibc.core.client.v1.ClientUpdateProposal)
    - [ConsensusStateWithHeight](#ibc.core.client.v1.ConsensusStateWithHeight)
    - [Height](#ibc.core.client.v1.Height)
    - [IdentifiedClientState](#ibc.core.client.v1.IdentifiedClientState)
    - [Params](#ibc.core.client.v1.Params)
    - [UpgradeProposal](#ibc.core.client.v1.UpgradeProposal)
  
- [ibc/applications/transfer/v1/tx.proto](#ibc/applications/transfer/v1/tx.proto)
    - [MsgTransfer](#ibc.applications.transfer.v1.MsgTransfer)
    - [MsgTransferResponse](#ibc.applications.transfer.v1.MsgTransferResponse)
  
    - [Msg](#ibc.applications.transfer.v1.Msg)
  
- [ibc/applications/transfer/v2/packet.proto](#ibc/applications/transfer/v2/packet.proto)
    - [FungibleTokenPacketData](#ibc.applications.transfer.v2.FungibleTokenPacketData)
  
- [ibc/core/channel/v1/channel.proto](#ibc/core/channel/v1/channel.proto)
    - [Acknowledgement](#ibc.core.channel.v1.Acknowledgement)
    - [Channel](#ibc.core.channel.v1.Channel)
    - [Counterparty](#ibc.core.channel.v1.Counterparty)
    - [IdentifiedChannel](#ibc.core.channel.v1.IdentifiedChannel)
    - [Packet](#ibc.core.channel.v1.Packet)
    - [PacketState](#ibc.core.channel.v1.PacketState)
  
    - [Order](#ibc.core.channel.v1.Order)
    - [State](#ibc.core.channel.v1.State)
  
- [ibc/core/channel/v1/genesis.proto](#ibc/core/channel/v1/genesis.proto)
    - [GenesisState](#ibc.core.channel.v1.GenesisState)
    - [PacketSequence](#ibc.core.channel.v1.PacketSequence)
  
- [ibc/core/channel/v1/query.proto](#ibc/core/channel/v1/query.proto)
    - [QueryChannelClientStateRequest](#ibc.core.channel.v1.QueryChannelClientStateRequest)
    - [QueryChannelClientStateResponse](#ibc.core.channel.v1.QueryChannelClientStateResponse)
    - [QueryChannelConsensusStateRequest](#ibc.core.channel.v1.QueryChannelConsensusStateRequest)
    - [QueryChannelConsensusStateResponse](#ibc.core.channel.v1.QueryChannelConsensusStateResponse)
    - [QueryChannelRequest](#ibc.core.channel.v1.QueryChannelRequest)
    - [QueryChannelResponse](#ibc.core.channel.v1.QueryChannelResponse)
    - [QueryChannelsRequest](#ibc.core.channel.v1.QueryChannelsRequest)
    - [QueryChannelsResponse](#ibc.core.channel.v1.QueryChannelsResponse)
    - [QueryConnectionChannelsRequest](#ibc.core.channel.v1.QueryConnectionChannelsRequest)
    - [QueryConnectionChannelsResponse](#ibc.core.channel.v1.QueryConnectionChannelsResponse)
    - [QueryNextSequenceReceiveRequest](#ibc.core.channel.v1.QueryNextSequenceReceiveRequest)
    - [QueryNextSequenceReceiveResponse](#ibc.core.channel.v1.QueryNextSequenceReceiveResponse)
    - [QueryPacketAcknowledgementRequest](#ibc.core.channel.v1.QueryPacketAcknowledgementRequest)
    - [QueryPacketAcknowledgementResponse](#ibc.core.channel.v1.QueryPacketAcknowledgementResponse)
    - [QueryPacketAcknowledgementsRequest](#ibc.core.channel.v1.QueryPacketAcknowledgementsRequest)
    - [QueryPacketAcknowledgementsResponse](#ibc.core.channel.v1.QueryPacketAcknowledgementsResponse)
    - [QueryPacketCommitmentRequest](#ibc.core.channel.v1.QueryPacketCommitmentRequest)
    - [QueryPacketCommitmentResponse](#ibc.core.channel.v1.QueryPacketCommitmentResponse)
    - [QueryPacketCommitmentsRequest](#ibc.core.channel.v1.QueryPacketCommitmentsRequest)
    - [QueryPacketCommitmentsResponse](#ibc.core.channel.v1.QueryPacketCommitmentsResponse)
    - [QueryPacketReceiptRequest](#ibc.core.channel.v1.QueryPacketReceiptRequest)
    - [QueryPacketReceiptResponse](#ibc.core.channel.v1.QueryPacketReceiptResponse)
    - [QueryUnreceivedAcksRequest](#ibc.core.channel.v1.QueryUnreceivedAcksRequest)
    - [QueryUnreceivedAcksResponse](#ibc.core.channel.v1.QueryUnreceivedAcksResponse)
    - [QueryUnreceivedPacketsRequest](#ibc.core.channel.v1.QueryUnreceivedPacketsRequest)
    - [QueryUnreceivedPacketsResponse](#ibc.core.channel.v1.QueryUnreceivedPacketsResponse)
  
    - [Query](#ibc.core.channel.v1.Query)
  
- [ibc/core/channel/v1/tx.proto](#ibc/core/channel/v1/tx.proto)
    - [MsgAcknowledgement](#ibc.core.channel.v1.MsgAcknowledgement)
    - [MsgAcknowledgementResponse](#ibc.core.channel.v1.MsgAcknowledgementResponse)
    - [MsgChannelCloseConfirm](#ibc.core.channel.v1.MsgChannelCloseConfirm)
    - [MsgChannelCloseConfirmResponse](#ibc.core.channel.v1.MsgChannelCloseConfirmResponse)
    - [MsgChannelCloseInit](#ibc.core.channel.v1.MsgChannelCloseInit)
    - [MsgChannelCloseInitResponse](#ibc.core.channel.v1.MsgChannelCloseInitResponse)
    - [MsgChannelOpenAck](#ibc.core.channel.v1.MsgChannelOpenAck)
    - [MsgChannelOpenAckResponse](#ibc.core.channel.v1.MsgChannelOpenAckResponse)
    - [MsgChannelOpenConfirm](#ibc.core.channel.v1.MsgChannelOpenConfirm)
    - [MsgChannelOpenConfirmResponse](#ibc.core.channel.v1.MsgChannelOpenConfirmResponse)
    - [MsgChannelOpenInit](#ibc.core.channel.v1.MsgChannelOpenInit)
    - [MsgChannelOpenInitResponse](#ibc.core.channel.v1.MsgChannelOpenInitResponse)
    - [MsgChannelOpenTry](#ibc.core.channel.v1.MsgChannelOpenTry)
    - [MsgChannelOpenTryResponse](#ibc.core.channel.v1.MsgChannelOpenTryResponse)
    - [MsgRecvPacket](#ibc.core.channel.v1.MsgRecvPacket)
    - [MsgRecvPacketResponse](#ibc.core.channel.v1.MsgRecvPacketResponse)
    - [MsgTimeout](#ibc.core.channel.v1.MsgTimeout)
    - [MsgTimeoutOnClose](#ibc.core.channel.v1.MsgTimeoutOnClose)
    - [MsgTimeoutOnCloseResponse](#ibc.core.channel.v1.MsgTimeoutOnCloseResponse)
    - [MsgTimeoutResponse](#ibc.core.channel.v1.MsgTimeoutResponse)
  
    - [ResponseResultType](#ibc.core.channel.v1.ResponseResultType)
  
    - [Msg](#ibc.core.channel.v1.Msg)
  
- [ibc/core/client/v1/genesis.proto](#ibc/core/client/v1/genesis.proto)
    - [GenesisMetadata](#ibc.core.client.v1.GenesisMetadata)
    - [GenesisState](#ibc.core.client.v1.GenesisState)
    - [IdentifiedGenesisMetadata](#ibc.core.client.v1.IdentifiedGenesisMetadata)
  
- [ibc/core/client/v1/query.proto](#ibc/core/client/v1/query.proto)
    - [QueryClientParamsRequest](#ibc.core.client.v1.QueryClientParamsRequest)
    - [QueryClientParamsResponse](#ibc.core.client.v1.QueryClientParamsResponse)
    - [QueryClientStateRequest](#ibc.core.client.v1.QueryClientStateRequest)
    - [QueryClientStateResponse](#ibc.core.client.v1.QueryClientStateResponse)
    - [QueryClientStatesRequest](#ibc.core.client.v1.QueryClientStatesRequest)
    - [QueryClientStatesResponse](#ibc.core.client.v1.QueryClientStatesResponse)
    - [QueryClientStatusRequest](#ibc.core.client.v1.QueryClientStatusRequest)
    - [QueryClientStatusResponse](#ibc.core.client.v1.QueryClientStatusResponse)
    - [QueryConsensusStateHeightsRequest](#ibc.core.client.v1.QueryConsensusStateHeightsRequest)
    - [QueryConsensusStateHeightsResponse](#ibc.core.client.v1.QueryConsensusStateHeightsResponse)
    - [QueryConsensusStateRequest](#ibc.core.client.v1.QueryConsensusStateRequest)
    - [QueryConsensusStateResponse](#ibc.core.client.v1.QueryConsensusStateResponse)
    - [QueryConsensusStatesRequest](#ibc.core.client.v1.QueryConsensusStatesRequest)
    - [QueryConsensusStatesResponse](#ibc.core.client.v1.QueryConsensusStatesResponse)
    - [QueryUpgradedClientStateRequest](#ibc.core.client.v1.QueryUpgradedClientStateRequest)
    - [QueryUpgradedClientStateResponse](#ibc.core.client.v1.QueryUpgradedClientStateResponse)
    - [QueryUpgradedConsensusStateRequest](#ibc.core.client.v1.QueryUpgradedConsensusStateRequest)
    - [QueryUpgradedConsensusStateResponse](#ibc.core.client.v1.QueryUpgradedConsensusStateResponse)
  
    - [Query](#ibc.core.client.v1.Query)
  
- [ibc/core/client/v1/tx.proto](#ibc/core/client/v1/tx.proto)
    - [MsgCreateClient](#ibc.core.client.v1.MsgCreateClient)
    - [MsgCreateClientResponse](#ibc.core.client.v1.MsgCreateClientResponse)
    - [MsgSubmitMisbehaviour](#ibc.core.client.v1.MsgSubmitMisbehaviour)
    - [MsgSubmitMisbehaviourResponse](#ibc.core.client.v1.MsgSubmitMisbehaviourResponse)
    - [MsgUpdateClient](#ibc.core.client.v1.MsgUpdateClient)
    - [MsgUpdateClientResponse](#ibc.core.client.v1.MsgUpdateClientResponse)
    - [MsgUpgradeClient](#ibc.core.client.v1.MsgUpgradeClient)
    - [MsgUpgradeClientResponse](#ibc.core.client.v1.MsgUpgradeClientResponse)
  
    - [Msg](#ibc.core.client.v1.Msg)
  
- [ibc/core/commitment/v1/commitment.proto](#ibc/core/commitment/v1/commitment.proto)
    - [MerklePath](#ibc.core.commitment.v1.MerklePath)
    - [MerklePrefix](#ibc.core.commitment.v1.MerklePrefix)
    - [MerkleProof](#ibc.core.commitment.v1.MerkleProof)
    - [MerkleRoot](#ibc.core.commitment.v1.MerkleRoot)
  
- [ibc/core/connection/v1/connection.proto](#ibc/core/connection/v1/connection.proto)
    - [ClientPaths](#ibc.core.connection.v1.ClientPaths)
    - [ConnectionEnd](#ibc.core.connection.v1.ConnectionEnd)
    - [ConnectionPaths](#ibc.core.connection.v1.ConnectionPaths)
    - [Counterparty](#ibc.core.connection.v1.Counterparty)
    - [IdentifiedConnection](#ibc.core.connection.v1.IdentifiedConnection)
    - [Params](#ibc.core.connection.v1.Params)
    - [Version](#ibc.core.connection.v1.Version)
  
    - [State](#ibc.core.connection.v1.State)
  
- [ibc/core/connection/v1/genesis.proto](#ibc/core/connection/v1/genesis.proto)
    - [GenesisState](#ibc.core.connection.v1.GenesisState)
  
- [ibc/core/connection/v1/query.proto](#ibc/core/connection/v1/query.proto)
    - [QueryClientConnectionsRequest](#ibc.core.connection.v1.QueryClientConnectionsRequest)
    - [QueryClientConnectionsResponse](#ibc.core.connection.v1.QueryClientConnectionsResponse)
    - [QueryConnectionClientStateRequest](#ibc.core.connection.v1.QueryConnectionClientStateRequest)
    - [QueryConnectionClientStateResponse](#ibc.core.connection.v1.QueryConnectionClientStateResponse)
    - [QueryConnectionConsensusStateRequest](#ibc.core.connection.v1.QueryConnectionConsensusStateRequest)
    - [QueryConnectionConsensusStateResponse](#ibc.core.connection.v1.QueryConnectionConsensusStateResponse)
    - [QueryConnectionRequest](#ibc.core.connection.v1.QueryConnectionRequest)
    - [QueryConnectionResponse](#ibc.core.connection.v1.QueryConnectionResponse)
    - [QueryConnectionsRequest](#ibc.core.connection.v1.QueryConnectionsRequest)
    - [QueryConnectionsResponse](#ibc.core.connection.v1.QueryConnectionsResponse)
  
    - [Query](#ibc.core.connection.v1.Query)
  
- [ibc/core/connection/v1/tx.proto](#ibc/core/connection/v1/tx.proto)
    - [MsgConnectionOpenAck](#ibc.core.connection.v1.MsgConnectionOpenAck)
    - [MsgConnectionOpenAckResponse](#ibc.core.connection.v1.MsgConnectionOpenAckResponse)
    - [MsgConnectionOpenConfirm](#ibc.core.connection.v1.MsgConnectionOpenConfirm)
    - [MsgConnectionOpenConfirmResponse](#ibc.core.connection.v1.MsgConnectionOpenConfirmResponse)
    - [MsgConnectionOpenInit](#ibc.core.connection.v1.MsgConnectionOpenInit)
    - [MsgConnectionOpenInitResponse](#ibc.core.connection.v1.MsgConnectionOpenInitResponse)
    - [MsgConnectionOpenTry](#ibc.core.connection.v1.MsgConnectionOpenTry)
    - [MsgConnectionOpenTryResponse](#ibc.core.connection.v1.MsgConnectionOpenTryResponse)
  
    - [Msg](#ibc.core.connection.v1.Msg)
  
- [ibc/core/types/v1/genesis.proto](#ibc/core/types/v1/genesis.proto)
    - [GenesisState](#ibc.core.types.v1.GenesisState)
  
- [ibc/lightclients/localhost/v1/localhost.proto](#ibc/lightclients/localhost/v1/localhost.proto)
    - [ClientState](#ibc.lightclients.localhost.v1.ClientState)
  
- [ibc/lightclients/solomachine/v1/solomachine.proto](#ibc/lightclients/solomachine/v1/solomachine.proto)
    - [ChannelStateData](#ibc.lightclients.solomachine.v1.ChannelStateData)
    - [ClientState](#ibc.lightclients.solomachine.v1.ClientState)
    - [ClientStateData](#ibc.lightclients.solomachine.v1.ClientStateData)
    - [ConnectionStateData](#ibc.lightclients.solomachine.v1.ConnectionStateData)
    - [ConsensusState](#ibc.lightclients.solomachine.v1.ConsensusState)
    - [ConsensusStateData](#ibc.lightclients.solomachine.v1.ConsensusStateData)
    - [Header](#ibc.lightclients.solomachine.v1.Header)
    - [HeaderData](#ibc.lightclients.solomachine.v1.HeaderData)
    - [Misbehaviour](#ibc.lightclients.solomachine.v1.Misbehaviour)
    - [NextSequenceRecvData](#ibc.lightclients.solomachine.v1.NextSequenceRecvData)
    - [PacketAcknowledgementData](#ibc.lightclients.solomachine.v1.PacketAcknowledgementData)
    - [PacketCommitmentData](#ibc.lightclients.solomachine.v1.PacketCommitmentData)
    - [PacketReceiptAbsenceData](#ibc.lightclients.solomachine.v1.PacketReceiptAbsenceData)
    - [SignBytes](#ibc.lightclients.solomachine.v1.SignBytes)
    - [SignatureAndData](#ibc.lightclients.solomachine.v1.SignatureAndData)
    - [TimestampedSignatureData](#ibc.lightclients.solomachine.v1.TimestampedSignatureData)
  
    - [DataType](#ibc.lightclients.solomachine.v1.DataType)
  
- [ibc/lightclients/solomachine/v2/solomachine.proto](#ibc/lightclients/solomachine/v2/solomachine.proto)
    - [ChannelStateData](#ibc.lightclients.solomachine.v2.ChannelStateData)
    - [ClientState](#ibc.lightclients.solomachine.v2.ClientState)
    - [ClientStateData](#ibc.lightclients.solomachine.v2.ClientStateData)
    - [ConnectionStateData](#ibc.lightclients.solomachine.v2.ConnectionStateData)
    - [ConsensusState](#ibc.lightclients.solomachine.v2.ConsensusState)
    - [ConsensusStateData](#ibc.lightclients.solomachine.v2.ConsensusStateData)
    - [Header](#ibc.lightclients.solomachine.v2.Header)
    - [HeaderData](#ibc.lightclients.solomachine.v2.HeaderData)
    - [Misbehaviour](#ibc.lightclients.solomachine.v2.Misbehaviour)
    - [NextSequenceRecvData](#ibc.lightclients.solomachine.v2.NextSequenceRecvData)
    - [PacketAcknowledgementData](#ibc.lightclients.solomachine.v2.PacketAcknowledgementData)
    - [PacketCommitmentData](#ibc.lightclients.solomachine.v2.PacketCommitmentData)
    - [PacketReceiptAbsenceData](#ibc.lightclients.solomachine.v2.PacketReceiptAbsenceData)
    - [SignBytes](#ibc.lightclients.solomachine.v2.SignBytes)
    - [SignatureAndData](#ibc.lightclients.solomachine.v2.SignatureAndData)
    - [TimestampedSignatureData](#ibc.lightclients.solomachine.v2.TimestampedSignatureData)
  
    - [DataType](#ibc.lightclients.solomachine.v2.DataType)
  
- [ibc/lightclients/tendermint/v1/tendermint.proto](#ibc/lightclients/tendermint/v1/tendermint.proto)
    - [ClientState](#ibc.lightclients.tendermint.v1.ClientState)
    - [ConsensusState](#ibc.lightclients.tendermint.v1.ConsensusState)
    - [Fraction](#ibc.lightclients.tendermint.v1.Fraction)
    - [Header](#ibc.lightclients.tendermint.v1.Header)
    - [Misbehaviour](#ibc.lightclients.tendermint.v1.Misbehaviour)
  
- [Scalar Value Types](#scalar-value-types)



<a name="ibc/applications/interchain_accounts/v1/account.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/applications/interchain_accounts/v1/account.proto



<a name="ibc.applications.interchain_accounts.v1.InterchainAccount"></a>

### InterchainAccount
An InterchainAccount is defined as a BaseAccount & the address of the account owner on the controller chain


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `base_account` | [cosmos.auth.v1beta1.BaseAccount](#cosmos.auth.v1beta1.BaseAccount) |  |  |
| `account_owner` | [string](#string) |  |  |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/applications/interchain_accounts/v1/genesis.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/applications/interchain_accounts/v1/genesis.proto



<a name="ibc.applications.interchain_accounts.v1.ActiveChannel"></a>

### ActiveChannel
ActiveChannel contains a connection ID, port ID and associated active channel ID


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connection_id` | [string](#string) |  |  |
| `port_id` | [string](#string) |  |  |
| `channel_id` | [string](#string) |  |  |






<a name="ibc.applications.interchain_accounts.v1.ControllerGenesisState"></a>

### ControllerGenesisState
ControllerGenesisState defines the interchain accounts controller genesis state


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `active_channels` | [ActiveChannel](#ibc.applications.interchain_accounts.v1.ActiveChannel) | repeated |  |
| `interchain_accounts` | [RegisteredInterchainAccount](#ibc.applications.interchain_accounts.v1.RegisteredInterchainAccount) | repeated |  |
| `ports` | [string](#string) | repeated |  |
| `params` | [ibc.applications.interchain_accounts.controller.v1.Params](#ibc.applications.interchain_accounts.controller.v1.Params) |  |  |






<a name="ibc.applications.interchain_accounts.v1.GenesisState"></a>

### GenesisState
GenesisState defines the interchain accounts genesis state


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `controller_genesis_state` | [ControllerGenesisState](#ibc.applications.interchain_accounts.v1.ControllerGenesisState) |  |  |
| `host_genesis_state` | [HostGenesisState](#ibc.applications.interchain_accounts.v1.HostGenesisState) |  |  |






<a name="ibc.applications.interchain_accounts.v1.HostGenesisState"></a>

### HostGenesisState
HostGenesisState defines the interchain accounts host genesis state


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `active_channels` | [ActiveChannel](#ibc.applications.interchain_accounts.v1.ActiveChannel) | repeated |  |
| `interchain_accounts` | [RegisteredInterchainAccount](#ibc.applications.interchain_accounts.v1.RegisteredInterchainAccount) | repeated |  |
| `port` | [string](#string) |  |  |
| `params` | [ibc.applications.interchain_accounts.host.v1.Params](#ibc.applications.interchain_accounts.host.v1.Params) |  |  |






<a name="ibc.applications.interchain_accounts.v1.RegisteredInterchainAccount"></a>

### RegisteredInterchainAccount
RegisteredInterchainAccount contains a connection ID, port ID and associated interchain account address


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connection_id` | [string](#string) |  |  |
| `port_id` | [string](#string) |  |  |
| `account_address` | [string](#string) |  |  |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/applications/interchain_accounts/v1/metadata.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/applications/interchain_accounts/v1/metadata.proto



<a name="ibc.applications.interchain_accounts.v1.Metadata"></a>

### Metadata
Metadata defines a set of protocol specific data encoded into the ICS27 channel version bytestring
See ICS004: https://github.com/cosmos/ibc/tree/master/spec/core/ics-004-channel-and-packet-semantics#Versioning


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `version` | [string](#string) |  | version defines the ICS27 protocol version |
| `controller_connection_id` | [string](#string) |  | controller_connection_id is the connection identifier associated with the controller chain |
| `host_connection_id` | [string](#string) |  | host_connection_id is the connection identifier associated with the host chain |
| `address` | [string](#string) |  | address defines the interchain account address to be fulfilled upon the OnChanOpenTry handshake step NOTE: the address field is empty on the OnChanOpenInit handshake step |
| `encoding` | [string](#string) |  | encoding defines the supported codec format |
| `tx_type` | [string](#string) |  | tx_type defines the type of transactions the interchain account can execute |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/applications/interchain_accounts/v1/packet.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/applications/interchain_accounts/v1/packet.proto



<a name="ibc.applications.interchain_accounts.v1.CosmosTx"></a>

### CosmosTx
CosmosTx contains a list of sdk.Msg's. It should be used when sending transactions to an SDK host chain.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `messages` | [google.protobuf.Any](#google.protobuf.Any) | repeated |  |






<a name="ibc.applications.interchain_accounts.v1.InterchainAccountPacketData"></a>

### InterchainAccountPacketData
InterchainAccountPacketData is comprised of a raw transaction, type of transaction and optional memo field.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `type` | [Type](#ibc.applications.interchain_accounts.v1.Type) |  |  |
| `data` | [bytes](#bytes) |  |  |
| `memo` | [string](#string) |  |  |





 <!-- end messages -->


<a name="ibc.applications.interchain_accounts.v1.Type"></a>

### Type
Type defines a classification of message issued from a controller chain to its associated interchain accounts
host

| Name | Number | Description |
| ---- | ------ | ----------- |
| TYPE_UNSPECIFIED | 0 | Default zero value enumeration |
| TYPE_EXECUTE_TX | 1 | Execute a transaction on an interchain accounts host chain |


 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/applications/transfer/v1/transfer.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/applications/transfer/v1/transfer.proto



<a name="ibc.applications.transfer.v1.DenomTrace"></a>

### DenomTrace
DenomTrace contains the base denomination for ICS20 fungible tokens and the
source tracing information path.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [string](#string) |  | path defines the chain of port/channel identifiers used for tracing the source of the fungible token. |
| `base_denom` | [string](#string) |  | base denomination of the relayed fungible token. |






<a name="ibc.applications.transfer.v1.Params"></a>

### Params
Params defines the set of IBC transfer parameters.
NOTE: To prevent a single token from being transferred, set the
TransfersEnabled parameter to true and then set the bank module's SendEnabled
parameter for the denomination to false.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `send_enabled` | [bool](#bool) |  | send_enabled enables or disables all cross-chain token transfers from this chain. |
| `receive_enabled` | [bool](#bool) |  | receive_enabled enables or disables all cross-chain token transfers to this chain. |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/applications/transfer/v1/genesis.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/applications/transfer/v1/genesis.proto



<a name="ibc.applications.transfer.v1.GenesisState"></a>

### GenesisState
GenesisState defines the ibc-transfer genesis state


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  |  |
| `denom_traces` | [DenomTrace](#ibc.applications.transfer.v1.DenomTrace) | repeated |  |
| `params` | [Params](#ibc.applications.transfer.v1.Params) |  |  |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/applications/transfer/v1/query.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/applications/transfer/v1/query.proto



<a name="ibc.applications.transfer.v1.QueryDenomHashRequest"></a>

### QueryDenomHashRequest
QueryDenomHashRequest is the request type for the Query/DenomHash RPC
method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `trace` | [string](#string) |  | The denomination trace ([port_id]/[channel_id])+/[denom] |






<a name="ibc.applications.transfer.v1.QueryDenomHashResponse"></a>

### QueryDenomHashResponse
QueryDenomHashResponse is the response type for the Query/DenomHash RPC
method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `hash` | [string](#string) |  | hash (in hex format) of the denomination trace information. |






<a name="ibc.applications.transfer.v1.QueryDenomTraceRequest"></a>

### QueryDenomTraceRequest
QueryDenomTraceRequest is the request type for the Query/DenomTrace RPC
method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `hash` | [string](#string) |  | hash (in hex format) or denom (full denom with ibc prefix) of the denomination trace information. |






<a name="ibc.applications.transfer.v1.QueryDenomTraceResponse"></a>

### QueryDenomTraceResponse
QueryDenomTraceResponse is the response type for the Query/DenomTrace RPC
method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `denom_trace` | [DenomTrace](#ibc.applications.transfer.v1.DenomTrace) |  | denom_trace returns the requested denomination trace information. |






<a name="ibc.applications.transfer.v1.QueryDenomTracesRequest"></a>

### QueryDenomTracesRequest
QueryConnectionsRequest is the request type for the Query/DenomTraces RPC
method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `pagination` | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination defines an optional pagination for the request. |






<a name="ibc.applications.transfer.v1.QueryDenomTracesResponse"></a>

### QueryDenomTracesResponse
QueryConnectionsResponse is the response type for the Query/DenomTraces RPC
method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `denom_traces` | [DenomTrace](#ibc.applications.transfer.v1.DenomTrace) | repeated | denom_traces returns all denominations trace information. |
| `pagination` | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination defines the pagination in the response. |






<a name="ibc.applications.transfer.v1.QueryEscrowAddressRequest"></a>

### QueryEscrowAddressRequest
QueryEscrowAddressRequest is the request type for the EscrowAddress RPC method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | unique port identifier |
| `channel_id` | [string](#string) |  | unique channel identifier |






<a name="ibc.applications.transfer.v1.QueryEscrowAddressResponse"></a>

### QueryEscrowAddressResponse
QueryEscrowAddressResponse is the response type of the EscrowAddress RPC method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `escrow_address` | [string](#string) |  | the escrow account address |






<a name="ibc.applications.transfer.v1.QueryParamsRequest"></a>

### QueryParamsRequest
QueryParamsRequest is the request type for the Query/Params RPC method.






<a name="ibc.applications.transfer.v1.QueryParamsResponse"></a>

### QueryParamsResponse
QueryParamsResponse is the response type for the Query/Params RPC method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `params` | [Params](#ibc.applications.transfer.v1.Params) |  | params defines the parameters of the module. |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->


<a name="ibc.applications.transfer.v1.Query"></a>

### Query
Query provides defines the gRPC querier service.

| Method Name | Request Type | Response Type | Description | HTTP Verb | Endpoint |
| ----------- | ------------ | ------------- | ------------| ------- | -------- |
| `DenomTrace` | [QueryDenomTraceRequest](#ibc.applications.transfer.v1.QueryDenomTraceRequest) | [QueryDenomTraceResponse](#ibc.applications.transfer.v1.QueryDenomTraceResponse) | DenomTrace queries a denomination trace information. | GET|/ibc/apps/transfer/v1/denom_traces/{hash}|
| `DenomTraces` | [QueryDenomTracesRequest](#ibc.applications.transfer.v1.QueryDenomTracesRequest) | [QueryDenomTracesResponse](#ibc.applications.transfer.v1.QueryDenomTracesResponse) | DenomTraces queries all denomination traces. | GET|/ibc/apps/transfer/v1/denom_traces|
| `Params` | [QueryParamsRequest](#ibc.applications.transfer.v1.QueryParamsRequest) | [QueryParamsResponse](#ibc.applications.transfer.v1.QueryParamsResponse) | Params queries all parameters of the ibc-transfer module. | GET|/ibc/apps/transfer/v1/params|
| `DenomHash` | [QueryDenomHashRequest](#ibc.applications.transfer.v1.QueryDenomHashRequest) | [QueryDenomHashResponse](#ibc.applications.transfer.v1.QueryDenomHashResponse) | DenomHash queries a denomination hash information. | GET|/ibc/apps/transfer/v1/denom_hashes/{trace}|
| `EscrowAddress` | [QueryEscrowAddressRequest](#ibc.applications.transfer.v1.QueryEscrowAddressRequest) | [QueryEscrowAddressResponse](#ibc.applications.transfer.v1.QueryEscrowAddressResponse) | EscrowAddress returns the escrow address for a particular port and channel id. | GET|/ibc/apps/transfer/v1/channels/{channel_id}/ports/{port_id}/escrow_address|

 <!-- end services -->



<a name="ibc/core/client/v1/client.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/client/v1/client.proto



<a name="ibc.core.client.v1.ClientConsensusStates"></a>

### ClientConsensusStates
ClientConsensusStates defines all the stored consensus states for a given
client.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client identifier |
| `consensus_states` | [ConsensusStateWithHeight](#ibc.core.client.v1.ConsensusStateWithHeight) | repeated | consensus states and their heights associated with the client |






<a name="ibc.core.client.v1.ClientUpdateProposal"></a>

### ClientUpdateProposal
ClientUpdateProposal is a governance proposal. If it passes, the substitute
client's latest consensus state is copied over to the subject client. The proposal
handler may fail if the subject and the substitute do not match in client and
chain parameters (with exception to latest height, frozen height, and chain-id).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `title` | [string](#string) |  | the title of the update proposal |
| `description` | [string](#string) |  | the description of the proposal |
| `subject_client_id` | [string](#string) |  | the client identifier for the client to be updated if the proposal passes |
| `substitute_client_id` | [string](#string) |  | the substitute client identifier for the client standing in for the subject client |






<a name="ibc.core.client.v1.ConsensusStateWithHeight"></a>

### ConsensusStateWithHeight
ConsensusStateWithHeight defines a consensus state with an additional height
field.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `height` | [Height](#ibc.core.client.v1.Height) |  | consensus state height |
| `consensus_state` | [google.protobuf.Any](#google.protobuf.Any) |  | consensus state |






<a name="ibc.core.client.v1.Height"></a>

### Height
Height is a monotonically increasing data type
that can be compared against another Height for the purposes of updating and
freezing clients

Normally the RevisionHeight is incremented at each height while keeping
RevisionNumber the same. However some consensus algorithms may choose to
reset the height in certain conditions e.g. hard forks, state-machine
breaking changes In these cases, the RevisionNumber is incremented so that
height continues to be monitonically increasing even as the RevisionHeight
gets reset


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `revision_number` | [uint64](#uint64) |  | the revision that the client is currently on |
| `revision_height` | [uint64](#uint64) |  | the height within the given revision |






<a name="ibc.core.client.v1.IdentifiedClientState"></a>

### IdentifiedClientState
IdentifiedClientState defines a client state with an additional client
identifier field.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client identifier |
| `client_state` | [google.protobuf.Any](#google.protobuf.Any) |  | client state |






<a name="ibc.core.client.v1.Params"></a>

### Params
Params defines the set of IBC light client parameters.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `allowed_clients` | [string](#string) | repeated | allowed_clients defines the list of allowed client state types. |






<a name="ibc.core.client.v1.UpgradeProposal"></a>

### UpgradeProposal
UpgradeProposal is a gov Content type for initiating an IBC breaking
upgrade.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `title` | [string](#string) |  |  |
| `description` | [string](#string) |  |  |
| `plan` | [cosmos.upgrade.v1beta1.Plan](#cosmos.upgrade.v1beta1.Plan) |  |  |
| `upgraded_client_state` | [google.protobuf.Any](#google.protobuf.Any) |  | An UpgradedClientState must be provided to perform an IBC breaking upgrade. This will make the chain commit to the correct upgraded (self) client state before the upgrade occurs, so that connecting chains can verify that the new upgraded client is valid by verifying a proof on the previous version of the chain. This will allow IBC connections to persist smoothly across planned chain upgrades |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/applications/transfer/v1/tx.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/applications/transfer/v1/tx.proto



<a name="ibc.applications.transfer.v1.MsgTransfer"></a>

### MsgTransfer
MsgTransfer defines a msg to transfer fungible tokens (i.e Coins) between
ICS20 enabled chains. See ICS Spec here:
https://github.com/cosmos/ibc/tree/master/spec/app/ics-020-fungible-token-transfer#data-structures


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `source_port` | [string](#string) |  | the port on which the packet will be sent |
| `source_channel` | [string](#string) |  | the channel by which the packet will be sent |
| `token` | [cosmos.base.v1beta1.Coin](#cosmos.base.v1beta1.Coin) |  | the tokens to be transferred |
| `sender` | [string](#string) |  | the sender address |
| `receiver` | [string](#string) |  | the recipient address on the destination chain |
| `timeout_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | Timeout height relative to the current block height. The timeout is disabled when set to 0. |
| `timeout_timestamp` | [uint64](#uint64) |  | Timeout timestamp in absolute nanoseconds since unix epoch. The timeout is disabled when set to 0. |






<a name="ibc.applications.transfer.v1.MsgTransferResponse"></a>

### MsgTransferResponse
MsgTransferResponse defines the Msg/Transfer response type.





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->


<a name="ibc.applications.transfer.v1.Msg"></a>

### Msg
Msg defines the ibc/transfer Msg service.

| Method Name | Request Type | Response Type | Description | HTTP Verb | Endpoint |
| ----------- | ------------ | ------------- | ------------| ------- | -------- |
| `Transfer` | [MsgTransfer](#ibc.applications.transfer.v1.MsgTransfer) | [MsgTransferResponse](#ibc.applications.transfer.v1.MsgTransferResponse) | Transfer defines a rpc handler method for MsgTransfer. | |

 <!-- end services -->



<a name="ibc/applications/transfer/v2/packet.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/applications/transfer/v2/packet.proto



<a name="ibc.applications.transfer.v2.FungibleTokenPacketData"></a>

### FungibleTokenPacketData
FungibleTokenPacketData defines a struct for the packet payload
See FungibleTokenPacketData spec:
https://github.com/cosmos/ibc/tree/master/spec/app/ics-020-fungible-token-transfer#data-structures


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `denom` | [string](#string) |  | the token denomination to be transferred |
| `amount` | [string](#string) |  | the token amount to be transferred |
| `sender` | [string](#string) |  | the sender address |
| `receiver` | [string](#string) |  | the recipient address on the destination chain |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/core/channel/v1/channel.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/channel/v1/channel.proto



<a name="ibc.core.channel.v1.Acknowledgement"></a>

### Acknowledgement
Acknowledgement is the recommended acknowledgement format to be used by
app-specific protocols.
NOTE: The field numbers 21 and 22 were explicitly chosen to avoid accidental
conflicts with other protobuf message formats used for acknowledgements.
The first byte of any message with this format will be the non-ASCII values
`0xaa` (result) or `0xb2` (error). Implemented as defined by ICS:
https://github.com/cosmos/ibc/tree/master/spec/core/ics-004-channel-and-packet-semantics#acknowledgement-envelope


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `result` | [bytes](#bytes) |  |  |
| `error` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.Channel"></a>

### Channel
Channel defines pipeline for exactly-once packet delivery between specific
modules on separate blockchains, which has at least one end capable of
sending packets and one end capable of receiving packets.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `state` | [State](#ibc.core.channel.v1.State) |  | current state of the channel end |
| `ordering` | [Order](#ibc.core.channel.v1.Order) |  | whether the channel is ordered or unordered |
| `counterparty` | [Counterparty](#ibc.core.channel.v1.Counterparty) |  | counterparty channel end |
| `connection_hops` | [string](#string) | repeated | list of connection identifiers, in order, along which packets sent on this channel will travel |
| `version` | [string](#string) |  | opaque channel version, which is agreed upon during the handshake |






<a name="ibc.core.channel.v1.Counterparty"></a>

### Counterparty
Counterparty defines a channel end counterparty


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port on the counterparty chain which owns the other end of the channel. |
| `channel_id` | [string](#string) |  | channel end on the counterparty chain |






<a name="ibc.core.channel.v1.IdentifiedChannel"></a>

### IdentifiedChannel
IdentifiedChannel defines a channel with additional port and channel
identifier fields.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `state` | [State](#ibc.core.channel.v1.State) |  | current state of the channel end |
| `ordering` | [Order](#ibc.core.channel.v1.Order) |  | whether the channel is ordered or unordered |
| `counterparty` | [Counterparty](#ibc.core.channel.v1.Counterparty) |  | counterparty channel end |
| `connection_hops` | [string](#string) | repeated | list of connection identifiers, in order, along which packets sent on this channel will travel |
| `version` | [string](#string) |  | opaque channel version, which is agreed upon during the handshake |
| `port_id` | [string](#string) |  | port identifier |
| `channel_id` | [string](#string) |  | channel identifier |






<a name="ibc.core.channel.v1.Packet"></a>

### Packet
Packet defines a type that carries data across different chains through IBC


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `sequence` | [uint64](#uint64) |  | number corresponds to the order of sends and receives, where a Packet with an earlier sequence number must be sent and received before a Packet with a later sequence number. |
| `source_port` | [string](#string) |  | identifies the port on the sending chain. |
| `source_channel` | [string](#string) |  | identifies the channel end on the sending chain. |
| `destination_port` | [string](#string) |  | identifies the port on the receiving chain. |
| `destination_channel` | [string](#string) |  | identifies the channel end on the receiving chain. |
| `data` | [bytes](#bytes) |  | actual opaque bytes transferred directly to the application module |
| `timeout_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | block height after which the packet times out |
| `timeout_timestamp` | [uint64](#uint64) |  | block timestamp (in nanoseconds) after which the packet times out |






<a name="ibc.core.channel.v1.PacketState"></a>

### PacketState
PacketState defines the generic type necessary to retrieve and store
packet commitments, acknowledgements, and receipts.
Caller is responsible for knowing the context necessary to interpret this
state as a commitment, acknowledgement, or a receipt.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | channel port identifier. |
| `channel_id` | [string](#string) |  | channel unique identifier. |
| `sequence` | [uint64](#uint64) |  | packet sequence. |
| `data` | [bytes](#bytes) |  | embedded data that represents packet state. |





 <!-- end messages -->


<a name="ibc.core.channel.v1.Order"></a>

### Order
Order defines if a channel is ORDERED or UNORDERED

| Name | Number | Description |
| ---- | ------ | ----------- |
| ORDER_NONE_UNSPECIFIED | 0 | zero-value for channel ordering |
| ORDER_UNORDERED | 1 | packets can be delivered in any order, which may differ from the order in which they were sent. |
| ORDER_ORDERED | 2 | packets are delivered exactly in the order which they were sent |



<a name="ibc.core.channel.v1.State"></a>

### State
State defines if a channel is in one of the following states:
CLOSED, INIT, TRYOPEN, OPEN or UNINITIALIZED.

| Name | Number | Description |
| ---- | ------ | ----------- |
| STATE_UNINITIALIZED_UNSPECIFIED | 0 | Default State |
| STATE_INIT | 1 | A channel has just started the opening handshake. |
| STATE_TRYOPEN | 2 | A channel has acknowledged the handshake step on the counterparty chain. |
| STATE_OPEN | 3 | A channel has completed the handshake. Open channels are ready to send and receive packets. |
| STATE_CLOSED | 4 | A channel has been closed and can no longer be used to send or receive packets. |


 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/core/channel/v1/genesis.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/channel/v1/genesis.proto



<a name="ibc.core.channel.v1.GenesisState"></a>

### GenesisState
GenesisState defines the ibc channel submodule's genesis state.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `channels` | [IdentifiedChannel](#ibc.core.channel.v1.IdentifiedChannel) | repeated |  |
| `acknowledgements` | [PacketState](#ibc.core.channel.v1.PacketState) | repeated |  |
| `commitments` | [PacketState](#ibc.core.channel.v1.PacketState) | repeated |  |
| `receipts` | [PacketState](#ibc.core.channel.v1.PacketState) | repeated |  |
| `send_sequences` | [PacketSequence](#ibc.core.channel.v1.PacketSequence) | repeated |  |
| `recv_sequences` | [PacketSequence](#ibc.core.channel.v1.PacketSequence) | repeated |  |
| `ack_sequences` | [PacketSequence](#ibc.core.channel.v1.PacketSequence) | repeated |  |
| `next_channel_sequence` | [uint64](#uint64) |  | the sequence for the next generated channel identifier |






<a name="ibc.core.channel.v1.PacketSequence"></a>

### PacketSequence
PacketSequence defines the genesis type necessary to retrieve and store
next send and receive sequences.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  |  |
| `channel_id` | [string](#string) |  |  |
| `sequence` | [uint64](#uint64) |  |  |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/core/channel/v1/query.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/channel/v1/query.proto



<a name="ibc.core.channel.v1.QueryChannelClientStateRequest"></a>

### QueryChannelClientStateRequest
QueryChannelClientStateRequest is the request type for the Query/ClientState
RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port unique identifier |
| `channel_id` | [string](#string) |  | channel unique identifier |






<a name="ibc.core.channel.v1.QueryChannelClientStateResponse"></a>

### QueryChannelClientStateResponse
QueryChannelClientStateResponse is the Response type for the
Query/QueryChannelClientState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `identified_client_state` | [ibc.core.client.v1.IdentifiedClientState](#ibc.core.client.v1.IdentifiedClientState) |  | client state associated with the channel |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.channel.v1.QueryChannelConsensusStateRequest"></a>

### QueryChannelConsensusStateRequest
QueryChannelConsensusStateRequest is the request type for the
Query/ConsensusState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port unique identifier |
| `channel_id` | [string](#string) |  | channel unique identifier |
| `revision_number` | [uint64](#uint64) |  | revision number of the consensus state |
| `revision_height` | [uint64](#uint64) |  | revision height of the consensus state |






<a name="ibc.core.channel.v1.QueryChannelConsensusStateResponse"></a>

### QueryChannelConsensusStateResponse
QueryChannelClientStateResponse is the Response type for the
Query/QueryChannelClientState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `consensus_state` | [google.protobuf.Any](#google.protobuf.Any) |  | consensus state associated with the channel |
| `client_id` | [string](#string) |  | client ID associated with the consensus state |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.channel.v1.QueryChannelRequest"></a>

### QueryChannelRequest
QueryChannelRequest is the request type for the Query/Channel RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port unique identifier |
| `channel_id` | [string](#string) |  | channel unique identifier |






<a name="ibc.core.channel.v1.QueryChannelResponse"></a>

### QueryChannelResponse
QueryChannelResponse is the response type for the Query/Channel RPC method.
Besides the Channel end, it includes a proof and the height from which the
proof was retrieved.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `channel` | [Channel](#ibc.core.channel.v1.Channel) |  | channel associated with the request identifiers |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.channel.v1.QueryChannelsRequest"></a>

### QueryChannelsRequest
QueryChannelsRequest is the request type for the Query/Channels RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `pagination` | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination request |






<a name="ibc.core.channel.v1.QueryChannelsResponse"></a>

### QueryChannelsResponse
QueryChannelsResponse is the response type for the Query/Channels RPC method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `channels` | [IdentifiedChannel](#ibc.core.channel.v1.IdentifiedChannel) | repeated | list of stored channels of the chain. |
| `pagination` | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination response |
| `height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | query block height |






<a name="ibc.core.channel.v1.QueryConnectionChannelsRequest"></a>

### QueryConnectionChannelsRequest
QueryConnectionChannelsRequest is the request type for the
Query/QueryConnectionChannels RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connection` | [string](#string) |  | connection unique identifier |
| `pagination` | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination request |






<a name="ibc.core.channel.v1.QueryConnectionChannelsResponse"></a>

### QueryConnectionChannelsResponse
QueryConnectionChannelsResponse is the Response type for the
Query/QueryConnectionChannels RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `channels` | [IdentifiedChannel](#ibc.core.channel.v1.IdentifiedChannel) | repeated | list of channels associated with a connection. |
| `pagination` | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination response |
| `height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | query block height |






<a name="ibc.core.channel.v1.QueryNextSequenceReceiveRequest"></a>

### QueryNextSequenceReceiveRequest
QueryNextSequenceReceiveRequest is the request type for the
Query/QueryNextSequenceReceiveRequest RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port unique identifier |
| `channel_id` | [string](#string) |  | channel unique identifier |






<a name="ibc.core.channel.v1.QueryNextSequenceReceiveResponse"></a>

### QueryNextSequenceReceiveResponse
QuerySequenceResponse is the request type for the
Query/QueryNextSequenceReceiveResponse RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `next_sequence_receive` | [uint64](#uint64) |  | next sequence receive number |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.channel.v1.QueryPacketAcknowledgementRequest"></a>

### QueryPacketAcknowledgementRequest
QueryPacketAcknowledgementRequest is the request type for the
Query/PacketAcknowledgement RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port unique identifier |
| `channel_id` | [string](#string) |  | channel unique identifier |
| `sequence` | [uint64](#uint64) |  | packet sequence |






<a name="ibc.core.channel.v1.QueryPacketAcknowledgementResponse"></a>

### QueryPacketAcknowledgementResponse
QueryPacketAcknowledgementResponse defines the client query response for a
packet which also includes a proof and the height from which the
proof was retrieved


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `acknowledgement` | [bytes](#bytes) |  | packet associated with the request fields |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.channel.v1.QueryPacketAcknowledgementsRequest"></a>

### QueryPacketAcknowledgementsRequest
QueryPacketAcknowledgementsRequest is the request type for the
Query/QueryPacketCommitments RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port unique identifier |
| `channel_id` | [string](#string) |  | channel unique identifier |
| `pagination` | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination request |
| `packet_commitment_sequences` | [uint64](#uint64) | repeated | list of packet sequences |






<a name="ibc.core.channel.v1.QueryPacketAcknowledgementsResponse"></a>

### QueryPacketAcknowledgementsResponse
QueryPacketAcknowledgemetsResponse is the request type for the
Query/QueryPacketAcknowledgements RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `acknowledgements` | [PacketState](#ibc.core.channel.v1.PacketState) | repeated |  |
| `pagination` | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination response |
| `height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | query block height |






<a name="ibc.core.channel.v1.QueryPacketCommitmentRequest"></a>

### QueryPacketCommitmentRequest
QueryPacketCommitmentRequest is the request type for the
Query/PacketCommitment RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port unique identifier |
| `channel_id` | [string](#string) |  | channel unique identifier |
| `sequence` | [uint64](#uint64) |  | packet sequence |






<a name="ibc.core.channel.v1.QueryPacketCommitmentResponse"></a>

### QueryPacketCommitmentResponse
QueryPacketCommitmentResponse defines the client query response for a packet
which also includes a proof and the height from which the proof was
retrieved


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `commitment` | [bytes](#bytes) |  | packet associated with the request fields |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.channel.v1.QueryPacketCommitmentsRequest"></a>

### QueryPacketCommitmentsRequest
QueryPacketCommitmentsRequest is the request type for the
Query/QueryPacketCommitments RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port unique identifier |
| `channel_id` | [string](#string) |  | channel unique identifier |
| `pagination` | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination request |






<a name="ibc.core.channel.v1.QueryPacketCommitmentsResponse"></a>

### QueryPacketCommitmentsResponse
QueryPacketCommitmentsResponse is the request type for the
Query/QueryPacketCommitments RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `commitments` | [PacketState](#ibc.core.channel.v1.PacketState) | repeated |  |
| `pagination` | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination response |
| `height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | query block height |






<a name="ibc.core.channel.v1.QueryPacketReceiptRequest"></a>

### QueryPacketReceiptRequest
QueryPacketReceiptRequest is the request type for the
Query/PacketReceipt RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port unique identifier |
| `channel_id` | [string](#string) |  | channel unique identifier |
| `sequence` | [uint64](#uint64) |  | packet sequence |






<a name="ibc.core.channel.v1.QueryPacketReceiptResponse"></a>

### QueryPacketReceiptResponse
QueryPacketReceiptResponse defines the client query response for a packet
receipt which also includes a proof, and the height from which the proof was
retrieved


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `received` | [bool](#bool) |  | success flag for if receipt exists |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.channel.v1.QueryUnreceivedAcksRequest"></a>

### QueryUnreceivedAcksRequest
QueryUnreceivedAcks is the request type for the
Query/UnreceivedAcks RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port unique identifier |
| `channel_id` | [string](#string) |  | channel unique identifier |
| `packet_ack_sequences` | [uint64](#uint64) | repeated | list of acknowledgement sequences |






<a name="ibc.core.channel.v1.QueryUnreceivedAcksResponse"></a>

### QueryUnreceivedAcksResponse
QueryUnreceivedAcksResponse is the response type for the
Query/UnreceivedAcks RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `sequences` | [uint64](#uint64) | repeated | list of unreceived acknowledgement sequences |
| `height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | query block height |






<a name="ibc.core.channel.v1.QueryUnreceivedPacketsRequest"></a>

### QueryUnreceivedPacketsRequest
QueryUnreceivedPacketsRequest is the request type for the
Query/UnreceivedPackets RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  | port unique identifier |
| `channel_id` | [string](#string) |  | channel unique identifier |
| `packet_commitment_sequences` | [uint64](#uint64) | repeated | list of packet sequences |






<a name="ibc.core.channel.v1.QueryUnreceivedPacketsResponse"></a>

### QueryUnreceivedPacketsResponse
QueryUnreceivedPacketsResponse is the response type for the
Query/UnreceivedPacketCommitments RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `sequences` | [uint64](#uint64) | repeated | list of unreceived packet sequences |
| `height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | query block height |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->


<a name="ibc.core.channel.v1.Query"></a>

### Query
Query provides defines the gRPC querier service

| Method Name | Request Type | Response Type | Description | HTTP Verb | Endpoint |
| ----------- | ------------ | ------------- | ------------| ------- | -------- |
| `Channel` | [QueryChannelRequest](#ibc.core.channel.v1.QueryChannelRequest) | [QueryChannelResponse](#ibc.core.channel.v1.QueryChannelResponse) | Channel queries an IBC Channel. | GET|/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}|
| `Channels` | [QueryChannelsRequest](#ibc.core.channel.v1.QueryChannelsRequest) | [QueryChannelsResponse](#ibc.core.channel.v1.QueryChannelsResponse) | Channels queries all the IBC channels of a chain. | GET|/ibc/core/channel/v1/channels|
| `ConnectionChannels` | [QueryConnectionChannelsRequest](#ibc.core.channel.v1.QueryConnectionChannelsRequest) | [QueryConnectionChannelsResponse](#ibc.core.channel.v1.QueryConnectionChannelsResponse) | ConnectionChannels queries all the channels associated with a connection end. | GET|/ibc/core/channel/v1/connections/{connection}/channels|
| `ChannelClientState` | [QueryChannelClientStateRequest](#ibc.core.channel.v1.QueryChannelClientStateRequest) | [QueryChannelClientStateResponse](#ibc.core.channel.v1.QueryChannelClientStateResponse) | ChannelClientState queries for the client state for the channel associated with the provided channel identifiers. | GET|/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/client_state|
| `ChannelConsensusState` | [QueryChannelConsensusStateRequest](#ibc.core.channel.v1.QueryChannelConsensusStateRequest) | [QueryChannelConsensusStateResponse](#ibc.core.channel.v1.QueryChannelConsensusStateResponse) | ChannelConsensusState queries for the consensus state for the channel associated with the provided channel identifiers. | GET|/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/consensus_state/revision/{revision_number}/height/{revision_height}|
| `PacketCommitment` | [QueryPacketCommitmentRequest](#ibc.core.channel.v1.QueryPacketCommitmentRequest) | [QueryPacketCommitmentResponse](#ibc.core.channel.v1.QueryPacketCommitmentResponse) | PacketCommitment queries a stored packet commitment hash. | GET|/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_commitments/{sequence}|
| `PacketCommitments` | [QueryPacketCommitmentsRequest](#ibc.core.channel.v1.QueryPacketCommitmentsRequest) | [QueryPacketCommitmentsResponse](#ibc.core.channel.v1.QueryPacketCommitmentsResponse) | PacketCommitments returns all the packet commitments hashes associated with a channel. | GET|/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_commitments|
| `PacketReceipt` | [QueryPacketReceiptRequest](#ibc.core.channel.v1.QueryPacketReceiptRequest) | [QueryPacketReceiptResponse](#ibc.core.channel.v1.QueryPacketReceiptResponse) | PacketReceipt queries if a given packet sequence has been received on the queried chain | GET|/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_receipts/{sequence}|
| `PacketAcknowledgement` | [QueryPacketAcknowledgementRequest](#ibc.core.channel.v1.QueryPacketAcknowledgementRequest) | [QueryPacketAcknowledgementResponse](#ibc.core.channel.v1.QueryPacketAcknowledgementResponse) | PacketAcknowledgement queries a stored packet acknowledgement hash. | GET|/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_acks/{sequence}|
| `PacketAcknowledgements` | [QueryPacketAcknowledgementsRequest](#ibc.core.channel.v1.QueryPacketAcknowledgementsRequest) | [QueryPacketAcknowledgementsResponse](#ibc.core.channel.v1.QueryPacketAcknowledgementsResponse) | PacketAcknowledgements returns all the packet acknowledgements associated with a channel. | GET|/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_acknowledgements|
| `UnreceivedPackets` | [QueryUnreceivedPacketsRequest](#ibc.core.channel.v1.QueryUnreceivedPacketsRequest) | [QueryUnreceivedPacketsResponse](#ibc.core.channel.v1.QueryUnreceivedPacketsResponse) | UnreceivedPackets returns all the unreceived IBC packets associated with a channel and sequences. | GET|/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_commitments/{packet_commitment_sequences}/unreceived_packets|
| `UnreceivedAcks` | [QueryUnreceivedAcksRequest](#ibc.core.channel.v1.QueryUnreceivedAcksRequest) | [QueryUnreceivedAcksResponse](#ibc.core.channel.v1.QueryUnreceivedAcksResponse) | UnreceivedAcks returns all the unreceived IBC acknowledgements associated with a channel and sequences. | GET|/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_commitments/{packet_ack_sequences}/unreceived_acks|
| `NextSequenceReceive` | [QueryNextSequenceReceiveRequest](#ibc.core.channel.v1.QueryNextSequenceReceiveRequest) | [QueryNextSequenceReceiveResponse](#ibc.core.channel.v1.QueryNextSequenceReceiveResponse) | NextSequenceReceive returns the next receive sequence for a given channel. | GET|/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/next_sequence|

 <!-- end services -->



<a name="ibc/core/channel/v1/tx.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/channel/v1/tx.proto



<a name="ibc.core.channel.v1.MsgAcknowledgement"></a>

### MsgAcknowledgement
MsgAcknowledgement receives incoming IBC acknowledgement


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `packet` | [Packet](#ibc.core.channel.v1.Packet) |  |  |
| `acknowledgement` | [bytes](#bytes) |  |  |
| `proof_acked` | [bytes](#bytes) |  |  |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgAcknowledgementResponse"></a>

### MsgAcknowledgementResponse
MsgAcknowledgementResponse defines the Msg/Acknowledgement response type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `result` | [ResponseResultType](#ibc.core.channel.v1.ResponseResultType) |  |  |






<a name="ibc.core.channel.v1.MsgChannelCloseConfirm"></a>

### MsgChannelCloseConfirm
MsgChannelCloseConfirm defines a msg sent by a Relayer to Chain B
to acknowledge the change of channel state to CLOSED on Chain A.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  |  |
| `channel_id` | [string](#string) |  |  |
| `proof_init` | [bytes](#bytes) |  |  |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgChannelCloseConfirmResponse"></a>

### MsgChannelCloseConfirmResponse
MsgChannelCloseConfirmResponse defines the Msg/ChannelCloseConfirm response
type.






<a name="ibc.core.channel.v1.MsgChannelCloseInit"></a>

### MsgChannelCloseInit
MsgChannelCloseInit defines a msg sent by a Relayer to Chain A
to close a channel with Chain B.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  |  |
| `channel_id` | [string](#string) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgChannelCloseInitResponse"></a>

### MsgChannelCloseInitResponse
MsgChannelCloseInitResponse defines the Msg/ChannelCloseInit response type.






<a name="ibc.core.channel.v1.MsgChannelOpenAck"></a>

### MsgChannelOpenAck
MsgChannelOpenAck defines a msg sent by a Relayer to Chain A to acknowledge
the change of channel state to TRYOPEN on Chain B.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  |  |
| `channel_id` | [string](#string) |  |  |
| `counterparty_channel_id` | [string](#string) |  |  |
| `counterparty_version` | [string](#string) |  |  |
| `proof_try` | [bytes](#bytes) |  |  |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgChannelOpenAckResponse"></a>

### MsgChannelOpenAckResponse
MsgChannelOpenAckResponse defines the Msg/ChannelOpenAck response type.






<a name="ibc.core.channel.v1.MsgChannelOpenConfirm"></a>

### MsgChannelOpenConfirm
MsgChannelOpenConfirm defines a msg sent by a Relayer to Chain B to
acknowledge the change of channel state to OPEN on Chain A.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  |  |
| `channel_id` | [string](#string) |  |  |
| `proof_ack` | [bytes](#bytes) |  |  |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgChannelOpenConfirmResponse"></a>

### MsgChannelOpenConfirmResponse
MsgChannelOpenConfirmResponse defines the Msg/ChannelOpenConfirm response
type.






<a name="ibc.core.channel.v1.MsgChannelOpenInit"></a>

### MsgChannelOpenInit
MsgChannelOpenInit defines an sdk.Msg to initialize a channel handshake. It
is called by a relayer on Chain A.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  |  |
| `channel` | [Channel](#ibc.core.channel.v1.Channel) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgChannelOpenInitResponse"></a>

### MsgChannelOpenInitResponse
MsgChannelOpenInitResponse defines the Msg/ChannelOpenInit response type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `channel_id` | [string](#string) |  |  |
| `version` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgChannelOpenTry"></a>

### MsgChannelOpenTry
MsgChannelOpenInit defines a msg sent by a Relayer to try to open a channel
on Chain B. The version field within the Channel field has been deprecated. Its
value will be ignored by core IBC.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `port_id` | [string](#string) |  |  |
| `previous_channel_id` | [string](#string) |  | in the case of crossing hello's, when both chains call OpenInit, we need the channel identifier of the previous channel in state INIT |
| `channel` | [Channel](#ibc.core.channel.v1.Channel) |  | NOTE: the version field within the channel has been deprecated. Its value will be ignored by core IBC. |
| `counterparty_version` | [string](#string) |  |  |
| `proof_init` | [bytes](#bytes) |  |  |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgChannelOpenTryResponse"></a>

### MsgChannelOpenTryResponse
MsgChannelOpenTryResponse defines the Msg/ChannelOpenTry response type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `version` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgRecvPacket"></a>

### MsgRecvPacket
MsgRecvPacket receives incoming IBC packet


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `packet` | [Packet](#ibc.core.channel.v1.Packet) |  |  |
| `proof_commitment` | [bytes](#bytes) |  |  |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgRecvPacketResponse"></a>

### MsgRecvPacketResponse
MsgRecvPacketResponse defines the Msg/RecvPacket response type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `result` | [ResponseResultType](#ibc.core.channel.v1.ResponseResultType) |  |  |






<a name="ibc.core.channel.v1.MsgTimeout"></a>

### MsgTimeout
MsgTimeout receives timed-out packet


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `packet` | [Packet](#ibc.core.channel.v1.Packet) |  |  |
| `proof_unreceived` | [bytes](#bytes) |  |  |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `next_sequence_recv` | [uint64](#uint64) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgTimeoutOnClose"></a>

### MsgTimeoutOnClose
MsgTimeoutOnClose timed-out packet upon counterparty channel closure.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `packet` | [Packet](#ibc.core.channel.v1.Packet) |  |  |
| `proof_unreceived` | [bytes](#bytes) |  |  |
| `proof_close` | [bytes](#bytes) |  |  |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `next_sequence_recv` | [uint64](#uint64) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.channel.v1.MsgTimeoutOnCloseResponse"></a>

### MsgTimeoutOnCloseResponse
MsgTimeoutOnCloseResponse defines the Msg/TimeoutOnClose response type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `result` | [ResponseResultType](#ibc.core.channel.v1.ResponseResultType) |  |  |






<a name="ibc.core.channel.v1.MsgTimeoutResponse"></a>

### MsgTimeoutResponse
MsgTimeoutResponse defines the Msg/Timeout response type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `result` | [ResponseResultType](#ibc.core.channel.v1.ResponseResultType) |  |  |





 <!-- end messages -->


<a name="ibc.core.channel.v1.ResponseResultType"></a>

### ResponseResultType
ResponseResultType defines the possible outcomes of the execution of a message

| Name | Number | Description |
| ---- | ------ | ----------- |
| RESPONSE_RESULT_TYPE_UNSPECIFIED | 0 | Default zero value enumeration |
| RESPONSE_RESULT_TYPE_NOOP | 1 | The message did not call the IBC application callbacks (because, for example, the packet had already been relayed) |
| RESPONSE_RESULT_TYPE_SUCCESS | 2 | The message was executed successfully |


 <!-- end enums -->

 <!-- end HasExtensions -->


<a name="ibc.core.channel.v1.Msg"></a>

### Msg
Msg defines the ibc/channel Msg service.

| Method Name | Request Type | Response Type | Description | HTTP Verb | Endpoint |
| ----------- | ------------ | ------------- | ------------| ------- | -------- |
| `ChannelOpenInit` | [MsgChannelOpenInit](#ibc.core.channel.v1.MsgChannelOpenInit) | [MsgChannelOpenInitResponse](#ibc.core.channel.v1.MsgChannelOpenInitResponse) | ChannelOpenInit defines a rpc handler method for MsgChannelOpenInit. | |
| `ChannelOpenTry` | [MsgChannelOpenTry](#ibc.core.channel.v1.MsgChannelOpenTry) | [MsgChannelOpenTryResponse](#ibc.core.channel.v1.MsgChannelOpenTryResponse) | ChannelOpenTry defines a rpc handler method for MsgChannelOpenTry. | |
| `ChannelOpenAck` | [MsgChannelOpenAck](#ibc.core.channel.v1.MsgChannelOpenAck) | [MsgChannelOpenAckResponse](#ibc.core.channel.v1.MsgChannelOpenAckResponse) | ChannelOpenAck defines a rpc handler method for MsgChannelOpenAck. | |
| `ChannelOpenConfirm` | [MsgChannelOpenConfirm](#ibc.core.channel.v1.MsgChannelOpenConfirm) | [MsgChannelOpenConfirmResponse](#ibc.core.channel.v1.MsgChannelOpenConfirmResponse) | ChannelOpenConfirm defines a rpc handler method for MsgChannelOpenConfirm. | |
| `ChannelCloseInit` | [MsgChannelCloseInit](#ibc.core.channel.v1.MsgChannelCloseInit) | [MsgChannelCloseInitResponse](#ibc.core.channel.v1.MsgChannelCloseInitResponse) | ChannelCloseInit defines a rpc handler method for MsgChannelCloseInit. | |
| `ChannelCloseConfirm` | [MsgChannelCloseConfirm](#ibc.core.channel.v1.MsgChannelCloseConfirm) | [MsgChannelCloseConfirmResponse](#ibc.core.channel.v1.MsgChannelCloseConfirmResponse) | ChannelCloseConfirm defines a rpc handler method for MsgChannelCloseConfirm. | |
| `RecvPacket` | [MsgRecvPacket](#ibc.core.channel.v1.MsgRecvPacket) | [MsgRecvPacketResponse](#ibc.core.channel.v1.MsgRecvPacketResponse) | RecvPacket defines a rpc handler method for MsgRecvPacket. | |
| `Timeout` | [MsgTimeout](#ibc.core.channel.v1.MsgTimeout) | [MsgTimeoutResponse](#ibc.core.channel.v1.MsgTimeoutResponse) | Timeout defines a rpc handler method for MsgTimeout. | |
| `TimeoutOnClose` | [MsgTimeoutOnClose](#ibc.core.channel.v1.MsgTimeoutOnClose) | [MsgTimeoutOnCloseResponse](#ibc.core.channel.v1.MsgTimeoutOnCloseResponse) | TimeoutOnClose defines a rpc handler method for MsgTimeoutOnClose. | |
| `Acknowledgement` | [MsgAcknowledgement](#ibc.core.channel.v1.MsgAcknowledgement) | [MsgAcknowledgementResponse](#ibc.core.channel.v1.MsgAcknowledgementResponse) | Acknowledgement defines a rpc handler method for MsgAcknowledgement. | |

 <!-- end services -->



<a name="ibc/core/client/v1/genesis.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/client/v1/genesis.proto



<a name="ibc.core.client.v1.GenesisMetadata"></a>

### GenesisMetadata
GenesisMetadata defines the genesis type for metadata that clients may return
with ExportMetadata


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `key` | [bytes](#bytes) |  | store key of metadata without clientID-prefix |
| `value` | [bytes](#bytes) |  | metadata value |






<a name="ibc.core.client.v1.GenesisState"></a>

### GenesisState
GenesisState defines the ibc client submodule's genesis state.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `clients` | [IdentifiedClientState](#ibc.core.client.v1.IdentifiedClientState) | repeated | client states with their corresponding identifiers |
| `clients_consensus` | [ClientConsensusStates](#ibc.core.client.v1.ClientConsensusStates) | repeated | consensus states from each client |
| `clients_metadata` | [IdentifiedGenesisMetadata](#ibc.core.client.v1.IdentifiedGenesisMetadata) | repeated | metadata from each client |
| `params` | [Params](#ibc.core.client.v1.Params) |  |  |
| `create_localhost` | [bool](#bool) |  | create localhost on initialization |
| `next_client_sequence` | [uint64](#uint64) |  | the sequence for the next generated client identifier |






<a name="ibc.core.client.v1.IdentifiedGenesisMetadata"></a>

### IdentifiedGenesisMetadata
IdentifiedGenesisMetadata has the client metadata with the corresponding
client id.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  |  |
| `client_metadata` | [GenesisMetadata](#ibc.core.client.v1.GenesisMetadata) | repeated |  |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/core/client/v1/query.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/client/v1/query.proto



<a name="ibc.core.client.v1.QueryClientParamsRequest"></a>

### QueryClientParamsRequest
QueryClientParamsRequest is the request type for the Query/ClientParams RPC
method.






<a name="ibc.core.client.v1.QueryClientParamsResponse"></a>

### QueryClientParamsResponse
QueryClientParamsResponse is the response type for the Query/ClientParams RPC
method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `params` | [Params](#ibc.core.client.v1.Params) |  | params defines the parameters of the module. |






<a name="ibc.core.client.v1.QueryClientStateRequest"></a>

### QueryClientStateRequest
QueryClientStateRequest is the request type for the Query/ClientState RPC
method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client state unique identifier |






<a name="ibc.core.client.v1.QueryClientStateResponse"></a>

### QueryClientStateResponse
QueryClientStateResponse is the response type for the Query/ClientState RPC
method. Besides the client state, it includes a proof and the height from
which the proof was retrieved.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_state` | [google.protobuf.Any](#google.protobuf.Any) |  | client state associated with the request identifier |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.client.v1.QueryClientStatesRequest"></a>

### QueryClientStatesRequest
QueryClientStatesRequest is the request type for the Query/ClientStates RPC
method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `pagination` | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination request |






<a name="ibc.core.client.v1.QueryClientStatesResponse"></a>

### QueryClientStatesResponse
QueryClientStatesResponse is the response type for the Query/ClientStates RPC
method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_states` | [IdentifiedClientState](#ibc.core.client.v1.IdentifiedClientState) | repeated | list of stored ClientStates of the chain. |
| `pagination` | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination response |






<a name="ibc.core.client.v1.QueryClientStatusRequest"></a>

### QueryClientStatusRequest
QueryClientStatusRequest is the request type for the Query/ClientStatus RPC
method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client unique identifier |






<a name="ibc.core.client.v1.QueryClientStatusResponse"></a>

### QueryClientStatusResponse
QueryClientStatusResponse is the response type for the Query/ClientStatus RPC
method. It returns the current status of the IBC client.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `status` | [string](#string) |  |  |






<a name="ibc.core.client.v1.QueryConsensusStateHeightsRequest"></a>

### QueryConsensusStateHeightsRequest
QueryConsensusStateHeightsRequest is the request type for Query/ConsensusStateHeights
RPC method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client identifier |
| `pagination` | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination request |






<a name="ibc.core.client.v1.QueryConsensusStateHeightsResponse"></a>

### QueryConsensusStateHeightsResponse
QueryConsensusStateHeightsResponse is the response type for the
Query/ConsensusStateHeights RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `consensus_state_heights` | [Height](#ibc.core.client.v1.Height) | repeated | consensus state heights |
| `pagination` | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination response |






<a name="ibc.core.client.v1.QueryConsensusStateRequest"></a>

### QueryConsensusStateRequest
QueryConsensusStateRequest is the request type for the Query/ConsensusState
RPC method. Besides the consensus state, it includes a proof and the height
from which the proof was retrieved.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client identifier |
| `revision_number` | [uint64](#uint64) |  | consensus state revision number |
| `revision_height` | [uint64](#uint64) |  | consensus state revision height |
| `latest_height` | [bool](#bool) |  | latest_height overrrides the height field and queries the latest stored ConsensusState |






<a name="ibc.core.client.v1.QueryConsensusStateResponse"></a>

### QueryConsensusStateResponse
QueryConsensusStateResponse is the response type for the Query/ConsensusState
RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `consensus_state` | [google.protobuf.Any](#google.protobuf.Any) |  | consensus state associated with the client identifier at the given height |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.client.v1.QueryConsensusStatesRequest"></a>

### QueryConsensusStatesRequest
QueryConsensusStatesRequest is the request type for the Query/ConsensusStates
RPC method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client identifier |
| `pagination` | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination request |






<a name="ibc.core.client.v1.QueryConsensusStatesResponse"></a>

### QueryConsensusStatesResponse
QueryConsensusStatesResponse is the response type for the
Query/ConsensusStates RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `consensus_states` | [ConsensusStateWithHeight](#ibc.core.client.v1.ConsensusStateWithHeight) | repeated | consensus states associated with the identifier |
| `pagination` | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination response |






<a name="ibc.core.client.v1.QueryUpgradedClientStateRequest"></a>

### QueryUpgradedClientStateRequest
QueryUpgradedClientStateRequest is the request type for the
Query/UpgradedClientState RPC method






<a name="ibc.core.client.v1.QueryUpgradedClientStateResponse"></a>

### QueryUpgradedClientStateResponse
QueryUpgradedClientStateResponse is the response type for the
Query/UpgradedClientState RPC method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `upgraded_client_state` | [google.protobuf.Any](#google.protobuf.Any) |  | client state associated with the request identifier |






<a name="ibc.core.client.v1.QueryUpgradedConsensusStateRequest"></a>

### QueryUpgradedConsensusStateRequest
QueryUpgradedConsensusStateRequest is the request type for the
Query/UpgradedConsensusState RPC method






<a name="ibc.core.client.v1.QueryUpgradedConsensusStateResponse"></a>

### QueryUpgradedConsensusStateResponse
QueryUpgradedConsensusStateResponse is the response type for the
Query/UpgradedConsensusState RPC method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `upgraded_consensus_state` | [google.protobuf.Any](#google.protobuf.Any) |  | Consensus state associated with the request identifier |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->


<a name="ibc.core.client.v1.Query"></a>

### Query
Query provides defines the gRPC querier service

| Method Name | Request Type | Response Type | Description | HTTP Verb | Endpoint |
| ----------- | ------------ | ------------- | ------------| ------- | -------- |
| `ClientState` | [QueryClientStateRequest](#ibc.core.client.v1.QueryClientStateRequest) | [QueryClientStateResponse](#ibc.core.client.v1.QueryClientStateResponse) | ClientState queries an IBC light client. | GET|/ibc/core/client/v1/client_states/{client_id}|
| `ClientStates` | [QueryClientStatesRequest](#ibc.core.client.v1.QueryClientStatesRequest) | [QueryClientStatesResponse](#ibc.core.client.v1.QueryClientStatesResponse) | ClientStates queries all the IBC light clients of a chain. | GET|/ibc/core/client/v1/client_states|
| `ConsensusState` | [QueryConsensusStateRequest](#ibc.core.client.v1.QueryConsensusStateRequest) | [QueryConsensusStateResponse](#ibc.core.client.v1.QueryConsensusStateResponse) | ConsensusState queries a consensus state associated with a client state at a given height. | GET|/ibc/core/client/v1/consensus_states/{client_id}/revision/{revision_number}/height/{revision_height}|
| `ConsensusStates` | [QueryConsensusStatesRequest](#ibc.core.client.v1.QueryConsensusStatesRequest) | [QueryConsensusStatesResponse](#ibc.core.client.v1.QueryConsensusStatesResponse) | ConsensusStates queries all the consensus state associated with a given client. | GET|/ibc/core/client/v1/consensus_states/{client_id}|
| `ConsensusStateHeights` | [QueryConsensusStateHeightsRequest](#ibc.core.client.v1.QueryConsensusStateHeightsRequest) | [QueryConsensusStateHeightsResponse](#ibc.core.client.v1.QueryConsensusStateHeightsResponse) | ConsensusStateHeights queries the height of every consensus states associated with a given client. | GET|/ibc/core/client/v1/consensus_states/{client_id}/heights|
| `ClientStatus` | [QueryClientStatusRequest](#ibc.core.client.v1.QueryClientStatusRequest) | [QueryClientStatusResponse](#ibc.core.client.v1.QueryClientStatusResponse) | Status queries the status of an IBC client. | GET|/ibc/core/client/v1/client_status/{client_id}|
| `ClientParams` | [QueryClientParamsRequest](#ibc.core.client.v1.QueryClientParamsRequest) | [QueryClientParamsResponse](#ibc.core.client.v1.QueryClientParamsResponse) | ClientParams queries all parameters of the ibc client. | GET|/ibc/client/v1/params|
| `UpgradedClientState` | [QueryUpgradedClientStateRequest](#ibc.core.client.v1.QueryUpgradedClientStateRequest) | [QueryUpgradedClientStateResponse](#ibc.core.client.v1.QueryUpgradedClientStateResponse) | UpgradedClientState queries an Upgraded IBC light client. | GET|/ibc/core/client/v1/upgraded_client_states|
| `UpgradedConsensusState` | [QueryUpgradedConsensusStateRequest](#ibc.core.client.v1.QueryUpgradedConsensusStateRequest) | [QueryUpgradedConsensusStateResponse](#ibc.core.client.v1.QueryUpgradedConsensusStateResponse) | UpgradedConsensusState queries an Upgraded IBC consensus state. | GET|/ibc/core/client/v1/upgraded_consensus_states|

 <!-- end services -->



<a name="ibc/core/client/v1/tx.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/client/v1/tx.proto



<a name="ibc.core.client.v1.MsgCreateClient"></a>

### MsgCreateClient
MsgCreateClient defines a message to create an IBC client


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_state` | [google.protobuf.Any](#google.protobuf.Any) |  | light client state |
| `consensus_state` | [google.protobuf.Any](#google.protobuf.Any) |  | consensus state associated with the client that corresponds to a given height. |
| `signer` | [string](#string) |  | signer address |






<a name="ibc.core.client.v1.MsgCreateClientResponse"></a>

### MsgCreateClientResponse
MsgCreateClientResponse defines the Msg/CreateClient response type.






<a name="ibc.core.client.v1.MsgSubmitMisbehaviour"></a>

### MsgSubmitMisbehaviour
MsgSubmitMisbehaviour defines an sdk.Msg type that submits Evidence for
light client misbehaviour.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client unique identifier |
| `misbehaviour` | [google.protobuf.Any](#google.protobuf.Any) |  | misbehaviour used for freezing the light client |
| `signer` | [string](#string) |  | signer address |






<a name="ibc.core.client.v1.MsgSubmitMisbehaviourResponse"></a>

### MsgSubmitMisbehaviourResponse
MsgSubmitMisbehaviourResponse defines the Msg/SubmitMisbehaviour response
type.






<a name="ibc.core.client.v1.MsgUpdateClient"></a>

### MsgUpdateClient
MsgUpdateClient defines an sdk.Msg to update a IBC client state using
the given header.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client unique identifier |
| `header` | [google.protobuf.Any](#google.protobuf.Any) |  | header to update the light client |
| `signer` | [string](#string) |  | signer address |






<a name="ibc.core.client.v1.MsgUpdateClientResponse"></a>

### MsgUpdateClientResponse
MsgUpdateClientResponse defines the Msg/UpdateClient response type.






<a name="ibc.core.client.v1.MsgUpgradeClient"></a>

### MsgUpgradeClient
MsgUpgradeClient defines an sdk.Msg to upgrade an IBC client to a new client
state


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client unique identifier |
| `client_state` | [google.protobuf.Any](#google.protobuf.Any) |  | upgraded client state |
| `consensus_state` | [google.protobuf.Any](#google.protobuf.Any) |  | upgraded consensus state, only contains enough information to serve as a basis of trust in update logic |
| `proof_upgrade_client` | [bytes](#bytes) |  | proof that old chain committed to new client |
| `proof_upgrade_consensus_state` | [bytes](#bytes) |  | proof that old chain committed to new consensus state |
| `signer` | [string](#string) |  | signer address |






<a name="ibc.core.client.v1.MsgUpgradeClientResponse"></a>

### MsgUpgradeClientResponse
MsgUpgradeClientResponse defines the Msg/UpgradeClient response type.





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->


<a name="ibc.core.client.v1.Msg"></a>

### Msg
Msg defines the ibc/client Msg service.

| Method Name | Request Type | Response Type | Description | HTTP Verb | Endpoint |
| ----------- | ------------ | ------------- | ------------| ------- | -------- |
| `CreateClient` | [MsgCreateClient](#ibc.core.client.v1.MsgCreateClient) | [MsgCreateClientResponse](#ibc.core.client.v1.MsgCreateClientResponse) | CreateClient defines a rpc handler method for MsgCreateClient. | |
| `UpdateClient` | [MsgUpdateClient](#ibc.core.client.v1.MsgUpdateClient) | [MsgUpdateClientResponse](#ibc.core.client.v1.MsgUpdateClientResponse) | UpdateClient defines a rpc handler method for MsgUpdateClient. | |
| `UpgradeClient` | [MsgUpgradeClient](#ibc.core.client.v1.MsgUpgradeClient) | [MsgUpgradeClientResponse](#ibc.core.client.v1.MsgUpgradeClientResponse) | UpgradeClient defines a rpc handler method for MsgUpgradeClient. | |
| `SubmitMisbehaviour` | [MsgSubmitMisbehaviour](#ibc.core.client.v1.MsgSubmitMisbehaviour) | [MsgSubmitMisbehaviourResponse](#ibc.core.client.v1.MsgSubmitMisbehaviourResponse) | SubmitMisbehaviour defines a rpc handler method for MsgSubmitMisbehaviour. | |

 <!-- end services -->



<a name="ibc/core/commitment/v1/commitment.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/commitment/v1/commitment.proto



<a name="ibc.core.commitment.v1.MerklePath"></a>

### MerklePath
MerklePath is the path used to verify commitment proofs, which can be an
arbitrary structured object (defined by a commitment type).
MerklePath is represented from root-to-leaf


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `key_path` | [string](#string) | repeated |  |






<a name="ibc.core.commitment.v1.MerklePrefix"></a>

### MerklePrefix
MerklePrefix is merkle path prefixed to the key.
The constructed key from the Path and the key will be append(Path.KeyPath,
append(Path.KeyPrefix, key...))


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `key_prefix` | [bytes](#bytes) |  |  |






<a name="ibc.core.commitment.v1.MerkleProof"></a>

### MerkleProof
MerkleProof is a wrapper type over a chain of CommitmentProofs.
It demonstrates membership or non-membership for an element or set of
elements, verifiable in conjunction with a known commitment root. Proofs
should be succinct.
MerkleProofs are ordered from leaf-to-root


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `proofs` | [ics23.CommitmentProof](#ics23.CommitmentProof) | repeated |  |






<a name="ibc.core.commitment.v1.MerkleRoot"></a>

### MerkleRoot
MerkleRoot defines a merkle root hash.
In the Cosmos SDK, the AppHash of a block header becomes the root.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `hash` | [bytes](#bytes) |  |  |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/core/connection/v1/connection.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/connection/v1/connection.proto



<a name="ibc.core.connection.v1.ClientPaths"></a>

### ClientPaths
ClientPaths define all the connection paths for a client state.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `paths` | [string](#string) | repeated | list of connection paths |






<a name="ibc.core.connection.v1.ConnectionEnd"></a>

### ConnectionEnd
ConnectionEnd defines a stateful object on a chain connected to another
separate one.
NOTE: there must only be 2 defined ConnectionEnds to establish
a connection between two chains.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client associated with this connection. |
| `versions` | [Version](#ibc.core.connection.v1.Version) | repeated | IBC version which can be utilised to determine encodings or protocols for channels or packets utilising this connection. |
| `state` | [State](#ibc.core.connection.v1.State) |  | current state of the connection end. |
| `counterparty` | [Counterparty](#ibc.core.connection.v1.Counterparty) |  | counterparty chain associated with this connection. |
| `delay_period` | [uint64](#uint64) |  | delay period that must pass before a consensus state can be used for packet-verification NOTE: delay period logic is only implemented by some clients. |






<a name="ibc.core.connection.v1.ConnectionPaths"></a>

### ConnectionPaths
ConnectionPaths define all the connection paths for a given client state.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client state unique identifier |
| `paths` | [string](#string) | repeated | list of connection paths |






<a name="ibc.core.connection.v1.Counterparty"></a>

### Counterparty
Counterparty defines the counterparty chain associated with a connection end.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | identifies the client on the counterparty chain associated with a given connection. |
| `connection_id` | [string](#string) |  | identifies the connection end on the counterparty chain associated with a given connection. |
| `prefix` | [ibc.core.commitment.v1.MerklePrefix](#ibc.core.commitment.v1.MerklePrefix) |  | commitment merkle prefix of the counterparty chain. |






<a name="ibc.core.connection.v1.IdentifiedConnection"></a>

### IdentifiedConnection
IdentifiedConnection defines a connection with additional connection
identifier field.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `id` | [string](#string) |  | connection identifier. |
| `client_id` | [string](#string) |  | client associated with this connection. |
| `versions` | [Version](#ibc.core.connection.v1.Version) | repeated | IBC version which can be utilised to determine encodings or protocols for channels or packets utilising this connection |
| `state` | [State](#ibc.core.connection.v1.State) |  | current state of the connection end. |
| `counterparty` | [Counterparty](#ibc.core.connection.v1.Counterparty) |  | counterparty chain associated with this connection. |
| `delay_period` | [uint64](#uint64) |  | delay period associated with this connection. |






<a name="ibc.core.connection.v1.Params"></a>

### Params
Params defines the set of Connection parameters.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `max_expected_time_per_block` | [uint64](#uint64) |  | maximum expected time per block (in nanoseconds), used to enforce block delay. This parameter should reflect the largest amount of time that the chain might reasonably take to produce the next block under normal operating conditions. A safe choice is 3-5x the expected time per block. |






<a name="ibc.core.connection.v1.Version"></a>

### Version
Version defines the versioning scheme used to negotiate the IBC verison in
the connection handshake.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `identifier` | [string](#string) |  | unique version identifier |
| `features` | [string](#string) | repeated | list of features compatible with the specified identifier |





 <!-- end messages -->


<a name="ibc.core.connection.v1.State"></a>

### State
State defines if a connection is in one of the following states:
INIT, TRYOPEN, OPEN or UNINITIALIZED.

| Name | Number | Description |
| ---- | ------ | ----------- |
| STATE_UNINITIALIZED_UNSPECIFIED | 0 | Default State |
| STATE_INIT | 1 | A connection end has just started the opening handshake. |
| STATE_TRYOPEN | 2 | A connection end has acknowledged the handshake step on the counterparty chain. |
| STATE_OPEN | 3 | A connection end has completed the handshake. |


 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/core/connection/v1/genesis.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/connection/v1/genesis.proto



<a name="ibc.core.connection.v1.GenesisState"></a>

### GenesisState
GenesisState defines the ibc connection submodule's genesis state.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connections` | [IdentifiedConnection](#ibc.core.connection.v1.IdentifiedConnection) | repeated |  |
| `client_connection_paths` | [ConnectionPaths](#ibc.core.connection.v1.ConnectionPaths) | repeated |  |
| `next_connection_sequence` | [uint64](#uint64) |  | the sequence for the next generated connection identifier |
| `params` | [Params](#ibc.core.connection.v1.Params) |  |  |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/core/connection/v1/query.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/connection/v1/query.proto



<a name="ibc.core.connection.v1.QueryClientConnectionsRequest"></a>

### QueryClientConnectionsRequest
QueryClientConnectionsRequest is the request type for the
Query/ClientConnections RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  | client identifier associated with a connection |






<a name="ibc.core.connection.v1.QueryClientConnectionsResponse"></a>

### QueryClientConnectionsResponse
QueryClientConnectionsResponse is the response type for the
Query/ClientConnections RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connection_paths` | [string](#string) | repeated | slice of all the connection paths associated with a client. |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | height at which the proof was generated |






<a name="ibc.core.connection.v1.QueryConnectionClientStateRequest"></a>

### QueryConnectionClientStateRequest
QueryConnectionClientStateRequest is the request type for the
Query/ConnectionClientState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connection_id` | [string](#string) |  | connection identifier |






<a name="ibc.core.connection.v1.QueryConnectionClientStateResponse"></a>

### QueryConnectionClientStateResponse
QueryConnectionClientStateResponse is the response type for the
Query/ConnectionClientState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `identified_client_state` | [ibc.core.client.v1.IdentifiedClientState](#ibc.core.client.v1.IdentifiedClientState) |  | client state associated with the channel |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.connection.v1.QueryConnectionConsensusStateRequest"></a>

### QueryConnectionConsensusStateRequest
QueryConnectionConsensusStateRequest is the request type for the
Query/ConnectionConsensusState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connection_id` | [string](#string) |  | connection identifier |
| `revision_number` | [uint64](#uint64) |  |  |
| `revision_height` | [uint64](#uint64) |  |  |






<a name="ibc.core.connection.v1.QueryConnectionConsensusStateResponse"></a>

### QueryConnectionConsensusStateResponse
QueryConnectionConsensusStateResponse is the response type for the
Query/ConnectionConsensusState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `consensus_state` | [google.protobuf.Any](#google.protobuf.Any) |  | consensus state associated with the channel |
| `client_id` | [string](#string) |  | client ID associated with the consensus state |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.connection.v1.QueryConnectionRequest"></a>

### QueryConnectionRequest
QueryConnectionRequest is the request type for the Query/Connection RPC
method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connection_id` | [string](#string) |  | connection unique identifier |






<a name="ibc.core.connection.v1.QueryConnectionResponse"></a>

### QueryConnectionResponse
QueryConnectionResponse is the response type for the Query/Connection RPC
method. Besides the connection end, it includes a proof and the height from
which the proof was retrieved.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connection` | [ConnectionEnd](#ibc.core.connection.v1.ConnectionEnd) |  | connection associated with the request identifier |
| `proof` | [bytes](#bytes) |  | merkle proof of existence |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | height at which the proof was retrieved |






<a name="ibc.core.connection.v1.QueryConnectionsRequest"></a>

### QueryConnectionsRequest
QueryConnectionsRequest is the request type for the Query/Connections RPC
method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `pagination` | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  |  |






<a name="ibc.core.connection.v1.QueryConnectionsResponse"></a>

### QueryConnectionsResponse
QueryConnectionsResponse is the response type for the Query/Connections RPC
method.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connections` | [IdentifiedConnection](#ibc.core.connection.v1.IdentifiedConnection) | repeated | list of stored connections of the chain. |
| `pagination` | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination response |
| `height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | query block height |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->


<a name="ibc.core.connection.v1.Query"></a>

### Query
Query provides defines the gRPC querier service

| Method Name | Request Type | Response Type | Description | HTTP Verb | Endpoint |
| ----------- | ------------ | ------------- | ------------| ------- | -------- |
| `Connection` | [QueryConnectionRequest](#ibc.core.connection.v1.QueryConnectionRequest) | [QueryConnectionResponse](#ibc.core.connection.v1.QueryConnectionResponse) | Connection queries an IBC connection end. | GET|/ibc/core/connection/v1/connections/{connection_id}|
| `Connections` | [QueryConnectionsRequest](#ibc.core.connection.v1.QueryConnectionsRequest) | [QueryConnectionsResponse](#ibc.core.connection.v1.QueryConnectionsResponse) | Connections queries all the IBC connections of a chain. | GET|/ibc/core/connection/v1/connections|
| `ClientConnections` | [QueryClientConnectionsRequest](#ibc.core.connection.v1.QueryClientConnectionsRequest) | [QueryClientConnectionsResponse](#ibc.core.connection.v1.QueryClientConnectionsResponse) | ClientConnections queries the connection paths associated with a client state. | GET|/ibc/core/connection/v1/client_connections/{client_id}|
| `ConnectionClientState` | [QueryConnectionClientStateRequest](#ibc.core.connection.v1.QueryConnectionClientStateRequest) | [QueryConnectionClientStateResponse](#ibc.core.connection.v1.QueryConnectionClientStateResponse) | ConnectionClientState queries the client state associated with the connection. | GET|/ibc/core/connection/v1/connections/{connection_id}/client_state|
| `ConnectionConsensusState` | [QueryConnectionConsensusStateRequest](#ibc.core.connection.v1.QueryConnectionConsensusStateRequest) | [QueryConnectionConsensusStateResponse](#ibc.core.connection.v1.QueryConnectionConsensusStateResponse) | ConnectionConsensusState queries the consensus state associated with the connection. | GET|/ibc/core/connection/v1/connections/{connection_id}/consensus_state/revision/{revision_number}/height/{revision_height}|

 <!-- end services -->



<a name="ibc/core/connection/v1/tx.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/connection/v1/tx.proto



<a name="ibc.core.connection.v1.MsgConnectionOpenAck"></a>

### MsgConnectionOpenAck
MsgConnectionOpenAck defines a msg sent by a Relayer to Chain A to
acknowledge the change of connection state to TRYOPEN on Chain B.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connection_id` | [string](#string) |  |  |
| `counterparty_connection_id` | [string](#string) |  |  |
| `version` | [Version](#ibc.core.connection.v1.Version) |  |  |
| `client_state` | [google.protobuf.Any](#google.protobuf.Any) |  |  |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `proof_try` | [bytes](#bytes) |  | proof of the initialization the connection on Chain B: `UNITIALIZED -> TRYOPEN` |
| `proof_client` | [bytes](#bytes) |  | proof of client state included in message |
| `proof_consensus` | [bytes](#bytes) |  | proof of client consensus state |
| `consensus_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.connection.v1.MsgConnectionOpenAckResponse"></a>

### MsgConnectionOpenAckResponse
MsgConnectionOpenAckResponse defines the Msg/ConnectionOpenAck response type.






<a name="ibc.core.connection.v1.MsgConnectionOpenConfirm"></a>

### MsgConnectionOpenConfirm
MsgConnectionOpenConfirm defines a msg sent by a Relayer to Chain B to
acknowledge the change of connection state to OPEN on Chain A.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `connection_id` | [string](#string) |  |  |
| `proof_ack` | [bytes](#bytes) |  | proof for the change of the connection state on Chain A: `INIT -> OPEN` |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.connection.v1.MsgConnectionOpenConfirmResponse"></a>

### MsgConnectionOpenConfirmResponse
MsgConnectionOpenConfirmResponse defines the Msg/ConnectionOpenConfirm
response type.






<a name="ibc.core.connection.v1.MsgConnectionOpenInit"></a>

### MsgConnectionOpenInit
MsgConnectionOpenInit defines the msg sent by an account on Chain A to
initialize a connection with Chain B.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  |  |
| `counterparty` | [Counterparty](#ibc.core.connection.v1.Counterparty) |  |  |
| `version` | [Version](#ibc.core.connection.v1.Version) |  |  |
| `delay_period` | [uint64](#uint64) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.connection.v1.MsgConnectionOpenInitResponse"></a>

### MsgConnectionOpenInitResponse
MsgConnectionOpenInitResponse defines the Msg/ConnectionOpenInit response
type.






<a name="ibc.core.connection.v1.MsgConnectionOpenTry"></a>

### MsgConnectionOpenTry
MsgConnectionOpenTry defines a msg sent by a Relayer to try to open a
connection on Chain B.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  |  |
| `previous_connection_id` | [string](#string) |  | in the case of crossing hello's, when both chains call OpenInit, we need the connection identifier of the previous connection in state INIT |
| `client_state` | [google.protobuf.Any](#google.protobuf.Any) |  |  |
| `counterparty` | [Counterparty](#ibc.core.connection.v1.Counterparty) |  |  |
| `delay_period` | [uint64](#uint64) |  |  |
| `counterparty_versions` | [Version](#ibc.core.connection.v1.Version) | repeated |  |
| `proof_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `proof_init` | [bytes](#bytes) |  | proof of the initialization the connection on Chain A: `UNITIALIZED -> INIT` |
| `proof_client` | [bytes](#bytes) |  | proof of client state included in message |
| `proof_consensus` | [bytes](#bytes) |  | proof of client consensus state |
| `consensus_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `signer` | [string](#string) |  |  |






<a name="ibc.core.connection.v1.MsgConnectionOpenTryResponse"></a>

### MsgConnectionOpenTryResponse
MsgConnectionOpenTryResponse defines the Msg/ConnectionOpenTry response type.





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->


<a name="ibc.core.connection.v1.Msg"></a>

### Msg
Msg defines the ibc/connection Msg service.

| Method Name | Request Type | Response Type | Description | HTTP Verb | Endpoint |
| ----------- | ------------ | ------------- | ------------| ------- | -------- |
| `ConnectionOpenInit` | [MsgConnectionOpenInit](#ibc.core.connection.v1.MsgConnectionOpenInit) | [MsgConnectionOpenInitResponse](#ibc.core.connection.v1.MsgConnectionOpenInitResponse) | ConnectionOpenInit defines a rpc handler method for MsgConnectionOpenInit. | |
| `ConnectionOpenTry` | [MsgConnectionOpenTry](#ibc.core.connection.v1.MsgConnectionOpenTry) | [MsgConnectionOpenTryResponse](#ibc.core.connection.v1.MsgConnectionOpenTryResponse) | ConnectionOpenTry defines a rpc handler method for MsgConnectionOpenTry. | |
| `ConnectionOpenAck` | [MsgConnectionOpenAck](#ibc.core.connection.v1.MsgConnectionOpenAck) | [MsgConnectionOpenAckResponse](#ibc.core.connection.v1.MsgConnectionOpenAckResponse) | ConnectionOpenAck defines a rpc handler method for MsgConnectionOpenAck. | |
| `ConnectionOpenConfirm` | [MsgConnectionOpenConfirm](#ibc.core.connection.v1.MsgConnectionOpenConfirm) | [MsgConnectionOpenConfirmResponse](#ibc.core.connection.v1.MsgConnectionOpenConfirmResponse) | ConnectionOpenConfirm defines a rpc handler method for MsgConnectionOpenConfirm. | |

 <!-- end services -->



<a name="ibc/core/types/v1/genesis.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/core/types/v1/genesis.proto



<a name="ibc.core.types.v1.GenesisState"></a>

### GenesisState
GenesisState defines the ibc module's genesis state.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_genesis` | [ibc.core.client.v1.GenesisState](#ibc.core.client.v1.GenesisState) |  | ICS002 - Clients genesis state |
| `connection_genesis` | [ibc.core.connection.v1.GenesisState](#ibc.core.connection.v1.GenesisState) |  | ICS003 - Connections genesis state |
| `channel_genesis` | [ibc.core.channel.v1.GenesisState](#ibc.core.channel.v1.GenesisState) |  | ICS004 - Channel genesis state |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/lightclients/localhost/v1/localhost.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/lightclients/localhost/v1/localhost.proto



<a name="ibc.lightclients.localhost.v1.ClientState"></a>

### ClientState
ClientState defines a loopback (localhost) client. It requires (read-only)
access to keys outside the client prefix.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `chain_id` | [string](#string) |  | self chain ID |
| `height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | self latest block height |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/lightclients/solomachine/v1/solomachine.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/lightclients/solomachine/v1/solomachine.proto



<a name="ibc.lightclients.solomachine.v1.ChannelStateData"></a>

### ChannelStateData
ChannelStateData returns the SignBytes data for channel state
verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `channel` | [ibc.core.channel.v1.Channel](#ibc.core.channel.v1.Channel) |  |  |






<a name="ibc.lightclients.solomachine.v1.ClientState"></a>

### ClientState
ClientState defines a solo machine client that tracks the current consensus
state and if the client is frozen.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `sequence` | [uint64](#uint64) |  | latest sequence of the client state |
| `frozen_sequence` | [uint64](#uint64) |  | frozen sequence of the solo machine |
| `consensus_state` | [ConsensusState](#ibc.lightclients.solomachine.v1.ConsensusState) |  |  |
| `allow_update_after_proposal` | [bool](#bool) |  | when set to true, will allow governance to update a solo machine client. The client will be unfrozen if it is frozen. |






<a name="ibc.lightclients.solomachine.v1.ClientStateData"></a>

### ClientStateData
ClientStateData returns the SignBytes data for client state verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `client_state` | [google.protobuf.Any](#google.protobuf.Any) |  |  |






<a name="ibc.lightclients.solomachine.v1.ConnectionStateData"></a>

### ConnectionStateData
ConnectionStateData returns the SignBytes data for connection state
verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `connection` | [ibc.core.connection.v1.ConnectionEnd](#ibc.core.connection.v1.ConnectionEnd) |  |  |






<a name="ibc.lightclients.solomachine.v1.ConsensusState"></a>

### ConsensusState
ConsensusState defines a solo machine consensus state. The sequence of a
consensus state is contained in the "height" key used in storing the
consensus state.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `public_key` | [google.protobuf.Any](#google.protobuf.Any) |  | public key of the solo machine |
| `diversifier` | [string](#string) |  | diversifier allows the same public key to be re-used across different solo machine clients (potentially on different chains) without being considered misbehaviour. |
| `timestamp` | [uint64](#uint64) |  |  |






<a name="ibc.lightclients.solomachine.v1.ConsensusStateData"></a>

### ConsensusStateData
ConsensusStateData returns the SignBytes data for consensus state
verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `consensus_state` | [google.protobuf.Any](#google.protobuf.Any) |  |  |






<a name="ibc.lightclients.solomachine.v1.Header"></a>

### Header
Header defines a solo machine consensus header


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `sequence` | [uint64](#uint64) |  | sequence to update solo machine public key at |
| `timestamp` | [uint64](#uint64) |  |  |
| `signature` | [bytes](#bytes) |  |  |
| `new_public_key` | [google.protobuf.Any](#google.protobuf.Any) |  |  |
| `new_diversifier` | [string](#string) |  |  |






<a name="ibc.lightclients.solomachine.v1.HeaderData"></a>

### HeaderData
HeaderData returns the SignBytes data for update verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `new_pub_key` | [google.protobuf.Any](#google.protobuf.Any) |  | header public key |
| `new_diversifier` | [string](#string) |  | header diversifier |






<a name="ibc.lightclients.solomachine.v1.Misbehaviour"></a>

### Misbehaviour
Misbehaviour defines misbehaviour for a solo machine which consists
of a sequence and two signatures over different messages at that sequence.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  |  |
| `sequence` | [uint64](#uint64) |  |  |
| `signature_one` | [SignatureAndData](#ibc.lightclients.solomachine.v1.SignatureAndData) |  |  |
| `signature_two` | [SignatureAndData](#ibc.lightclients.solomachine.v1.SignatureAndData) |  |  |






<a name="ibc.lightclients.solomachine.v1.NextSequenceRecvData"></a>

### NextSequenceRecvData
NextSequenceRecvData returns the SignBytes data for verification of the next
sequence to be received.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `next_seq_recv` | [uint64](#uint64) |  |  |






<a name="ibc.lightclients.solomachine.v1.PacketAcknowledgementData"></a>

### PacketAcknowledgementData
PacketAcknowledgementData returns the SignBytes data for acknowledgement
verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `acknowledgement` | [bytes](#bytes) |  |  |






<a name="ibc.lightclients.solomachine.v1.PacketCommitmentData"></a>

### PacketCommitmentData
PacketCommitmentData returns the SignBytes data for packet commitment
verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `commitment` | [bytes](#bytes) |  |  |






<a name="ibc.lightclients.solomachine.v1.PacketReceiptAbsenceData"></a>

### PacketReceiptAbsenceData
PacketReceiptAbsenceData returns the SignBytes data for
packet receipt absence verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |






<a name="ibc.lightclients.solomachine.v1.SignBytes"></a>

### SignBytes
SignBytes defines the signed bytes used for signature verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `sequence` | [uint64](#uint64) |  |  |
| `timestamp` | [uint64](#uint64) |  |  |
| `diversifier` | [string](#string) |  |  |
| `data_type` | [DataType](#ibc.lightclients.solomachine.v1.DataType) |  | type of the data used |
| `data` | [bytes](#bytes) |  | marshaled data |






<a name="ibc.lightclients.solomachine.v1.SignatureAndData"></a>

### SignatureAndData
SignatureAndData contains a signature and the data signed over to create that
signature.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `signature` | [bytes](#bytes) |  |  |
| `data_type` | [DataType](#ibc.lightclients.solomachine.v1.DataType) |  |  |
| `data` | [bytes](#bytes) |  |  |
| `timestamp` | [uint64](#uint64) |  |  |






<a name="ibc.lightclients.solomachine.v1.TimestampedSignatureData"></a>

### TimestampedSignatureData
TimestampedSignatureData contains the signature data and the timestamp of the
signature.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `signature_data` | [bytes](#bytes) |  |  |
| `timestamp` | [uint64](#uint64) |  |  |





 <!-- end messages -->


<a name="ibc.lightclients.solomachine.v1.DataType"></a>

### DataType
DataType defines the type of solo machine proof being created. This is done
to preserve uniqueness of different data sign byte encodings.

| Name | Number | Description |
| ---- | ------ | ----------- |
| DATA_TYPE_UNINITIALIZED_UNSPECIFIED | 0 | Default State |
| DATA_TYPE_CLIENT_STATE | 1 | Data type for client state verification |
| DATA_TYPE_CONSENSUS_STATE | 2 | Data type for consensus state verification |
| DATA_TYPE_CONNECTION_STATE | 3 | Data type for connection state verification |
| DATA_TYPE_CHANNEL_STATE | 4 | Data type for channel state verification |
| DATA_TYPE_PACKET_COMMITMENT | 5 | Data type for packet commitment verification |
| DATA_TYPE_PACKET_ACKNOWLEDGEMENT | 6 | Data type for packet acknowledgement verification |
| DATA_TYPE_PACKET_RECEIPT_ABSENCE | 7 | Data type for packet receipt absence verification |
| DATA_TYPE_NEXT_SEQUENCE_RECV | 8 | Data type for next sequence recv verification |
| DATA_TYPE_HEADER | 9 | Data type for header verification |


 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/lightclients/solomachine/v2/solomachine.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/lightclients/solomachine/v2/solomachine.proto



<a name="ibc.lightclients.solomachine.v2.ChannelStateData"></a>

### ChannelStateData
ChannelStateData returns the SignBytes data for channel state
verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `channel` | [ibc.core.channel.v1.Channel](#ibc.core.channel.v1.Channel) |  |  |






<a name="ibc.lightclients.solomachine.v2.ClientState"></a>

### ClientState
ClientState defines a solo machine client that tracks the current consensus
state and if the client is frozen.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `sequence` | [uint64](#uint64) |  | latest sequence of the client state |
| `is_frozen` | [bool](#bool) |  | frozen sequence of the solo machine |
| `consensus_state` | [ConsensusState](#ibc.lightclients.solomachine.v2.ConsensusState) |  |  |
| `allow_update_after_proposal` | [bool](#bool) |  | when set to true, will allow governance to update a solo machine client. The client will be unfrozen if it is frozen. |






<a name="ibc.lightclients.solomachine.v2.ClientStateData"></a>

### ClientStateData
ClientStateData returns the SignBytes data for client state verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `client_state` | [google.protobuf.Any](#google.protobuf.Any) |  |  |






<a name="ibc.lightclients.solomachine.v2.ConnectionStateData"></a>

### ConnectionStateData
ConnectionStateData returns the SignBytes data for connection state
verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `connection` | [ibc.core.connection.v1.ConnectionEnd](#ibc.core.connection.v1.ConnectionEnd) |  |  |






<a name="ibc.lightclients.solomachine.v2.ConsensusState"></a>

### ConsensusState
ConsensusState defines a solo machine consensus state. The sequence of a
consensus state is contained in the "height" key used in storing the
consensus state.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `public_key` | [google.protobuf.Any](#google.protobuf.Any) |  | public key of the solo machine |
| `diversifier` | [string](#string) |  | diversifier allows the same public key to be re-used across different solo machine clients (potentially on different chains) without being considered misbehaviour. |
| `timestamp` | [uint64](#uint64) |  |  |






<a name="ibc.lightclients.solomachine.v2.ConsensusStateData"></a>

### ConsensusStateData
ConsensusStateData returns the SignBytes data for consensus state
verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `consensus_state` | [google.protobuf.Any](#google.protobuf.Any) |  |  |






<a name="ibc.lightclients.solomachine.v2.Header"></a>

### Header
Header defines a solo machine consensus header


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `sequence` | [uint64](#uint64) |  | sequence to update solo machine public key at |
| `timestamp` | [uint64](#uint64) |  |  |
| `signature` | [bytes](#bytes) |  |  |
| `new_public_key` | [google.protobuf.Any](#google.protobuf.Any) |  |  |
| `new_diversifier` | [string](#string) |  |  |






<a name="ibc.lightclients.solomachine.v2.HeaderData"></a>

### HeaderData
HeaderData returns the SignBytes data for update verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `new_pub_key` | [google.protobuf.Any](#google.protobuf.Any) |  | header public key |
| `new_diversifier` | [string](#string) |  | header diversifier |






<a name="ibc.lightclients.solomachine.v2.Misbehaviour"></a>

### Misbehaviour
Misbehaviour defines misbehaviour for a solo machine which consists
of a sequence and two signatures over different messages at that sequence.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  |  |
| `sequence` | [uint64](#uint64) |  |  |
| `signature_one` | [SignatureAndData](#ibc.lightclients.solomachine.v2.SignatureAndData) |  |  |
| `signature_two` | [SignatureAndData](#ibc.lightclients.solomachine.v2.SignatureAndData) |  |  |






<a name="ibc.lightclients.solomachine.v2.NextSequenceRecvData"></a>

### NextSequenceRecvData
NextSequenceRecvData returns the SignBytes data for verification of the next
sequence to be received.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `next_seq_recv` | [uint64](#uint64) |  |  |






<a name="ibc.lightclients.solomachine.v2.PacketAcknowledgementData"></a>

### PacketAcknowledgementData
PacketAcknowledgementData returns the SignBytes data for acknowledgement
verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `acknowledgement` | [bytes](#bytes) |  |  |






<a name="ibc.lightclients.solomachine.v2.PacketCommitmentData"></a>

### PacketCommitmentData
PacketCommitmentData returns the SignBytes data for packet commitment
verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |
| `commitment` | [bytes](#bytes) |  |  |






<a name="ibc.lightclients.solomachine.v2.PacketReceiptAbsenceData"></a>

### PacketReceiptAbsenceData
PacketReceiptAbsenceData returns the SignBytes data for
packet receipt absence verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `path` | [bytes](#bytes) |  |  |






<a name="ibc.lightclients.solomachine.v2.SignBytes"></a>

### SignBytes
SignBytes defines the signed bytes used for signature verification.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `sequence` | [uint64](#uint64) |  |  |
| `timestamp` | [uint64](#uint64) |  |  |
| `diversifier` | [string](#string) |  |  |
| `data_type` | [DataType](#ibc.lightclients.solomachine.v2.DataType) |  | type of the data used |
| `data` | [bytes](#bytes) |  | marshaled data |






<a name="ibc.lightclients.solomachine.v2.SignatureAndData"></a>

### SignatureAndData
SignatureAndData contains a signature and the data signed over to create that
signature.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `signature` | [bytes](#bytes) |  |  |
| `data_type` | [DataType](#ibc.lightclients.solomachine.v2.DataType) |  |  |
| `data` | [bytes](#bytes) |  |  |
| `timestamp` | [uint64](#uint64) |  |  |






<a name="ibc.lightclients.solomachine.v2.TimestampedSignatureData"></a>

### TimestampedSignatureData
TimestampedSignatureData contains the signature data and the timestamp of the
signature.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `signature_data` | [bytes](#bytes) |  |  |
| `timestamp` | [uint64](#uint64) |  |  |





 <!-- end messages -->


<a name="ibc.lightclients.solomachine.v2.DataType"></a>

### DataType
DataType defines the type of solo machine proof being created. This is done
to preserve uniqueness of different data sign byte encodings.

| Name | Number | Description |
| ---- | ------ | ----------- |
| DATA_TYPE_UNINITIALIZED_UNSPECIFIED | 0 | Default State |
| DATA_TYPE_CLIENT_STATE | 1 | Data type for client state verification |
| DATA_TYPE_CONSENSUS_STATE | 2 | Data type for consensus state verification |
| DATA_TYPE_CONNECTION_STATE | 3 | Data type for connection state verification |
| DATA_TYPE_CHANNEL_STATE | 4 | Data type for channel state verification |
| DATA_TYPE_PACKET_COMMITMENT | 5 | Data type for packet commitment verification |
| DATA_TYPE_PACKET_ACKNOWLEDGEMENT | 6 | Data type for packet acknowledgement verification |
| DATA_TYPE_PACKET_RECEIPT_ABSENCE | 7 | Data type for packet receipt absence verification |
| DATA_TYPE_NEXT_SEQUENCE_RECV | 8 | Data type for next sequence recv verification |
| DATA_TYPE_HEADER | 9 | Data type for header verification |


 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



<a name="ibc/lightclients/tendermint/v1/tendermint.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## ibc/lightclients/tendermint/v1/tendermint.proto



<a name="ibc.lightclients.tendermint.v1.ClientState"></a>

### ClientState
ClientState from Tendermint tracks the current validator set, latest height,
and a possible frozen height.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `chain_id` | [string](#string) |  |  |
| `trust_level` | [Fraction](#ibc.lightclients.tendermint.v1.Fraction) |  |  |
| `trusting_period` | [google.protobuf.Duration](#google.protobuf.Duration) |  | duration of the period since the LastestTimestamp during which the submitted headers are valid for upgrade |
| `unbonding_period` | [google.protobuf.Duration](#google.protobuf.Duration) |  | duration of the staking unbonding period |
| `max_clock_drift` | [google.protobuf.Duration](#google.protobuf.Duration) |  | defines how much new (untrusted) header's Time can drift into the future. |
| `frozen_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | Block height when the client was frozen due to a misbehaviour |
| `latest_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  | Latest height the client was updated to |
| `proof_specs` | [ics23.ProofSpec](#ics23.ProofSpec) | repeated | Proof specifications used in verifying counterparty state |
| `upgrade_path` | [string](#string) | repeated | Path at which next upgraded client will be committed. Each element corresponds to the key for a single CommitmentProof in the chained proof. NOTE: ClientState must stored under `{upgradePath}/{upgradeHeight}/clientState` ConsensusState must be stored under `{upgradepath}/{upgradeHeight}/consensusState` For SDK chains using the default upgrade module, upgrade_path should be []string{"upgrade", "upgradedIBCState"}` |
| `allow_update_after_expiry` | [bool](#bool) |  | **Deprecated.** allow_update_after_expiry is deprecated |
| `allow_update_after_misbehaviour` | [bool](#bool) |  | **Deprecated.** allow_update_after_misbehaviour is deprecated |






<a name="ibc.lightclients.tendermint.v1.ConsensusState"></a>

### ConsensusState
ConsensusState defines the consensus state from Tendermint.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `timestamp` | [google.protobuf.Timestamp](#google.protobuf.Timestamp) |  | timestamp that corresponds to the block height in which the ConsensusState was stored. |
| `root` | [ibc.core.commitment.v1.MerkleRoot](#ibc.core.commitment.v1.MerkleRoot) |  | commitment root (i.e app hash) |
| `next_validators_hash` | [bytes](#bytes) |  |  |






<a name="ibc.lightclients.tendermint.v1.Fraction"></a>

### Fraction
Fraction defines the protobuf message type for tmmath.Fraction that only
supports positive values.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `numerator` | [uint64](#uint64) |  |  |
| `denominator` | [uint64](#uint64) |  |  |






<a name="ibc.lightclients.tendermint.v1.Header"></a>

### Header
Header defines the Tendermint client consensus Header.
It encapsulates all the information necessary to update from a trusted
Tendermint ConsensusState. The inclusion of TrustedHeight and
TrustedValidators allows this update to process correctly, so long as the
ConsensusState for the TrustedHeight exists, this removes race conditions
among relayers The SignedHeader and ValidatorSet are the new untrusted update
fields for the client. The TrustedHeight is the height of a stored
ConsensusState on the client that will be used to verify the new untrusted
header. The Trusted ConsensusState must be within the unbonding period of
current time in order to correctly verify, and the TrustedValidators must
hash to TrustedConsensusState.NextValidatorsHash since that is the last
trusted validator set at the TrustedHeight.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `signed_header` | [tendermint.types.SignedHeader](#tendermint.types.SignedHeader) |  |  |
| `validator_set` | [tendermint.types.ValidatorSet](#tendermint.types.ValidatorSet) |  |  |
| `trusted_height` | [ibc.core.client.v1.Height](#ibc.core.client.v1.Height) |  |  |
| `trusted_validators` | [tendermint.types.ValidatorSet](#tendermint.types.ValidatorSet) |  |  |






<a name="ibc.lightclients.tendermint.v1.Misbehaviour"></a>

### Misbehaviour
Misbehaviour is a wrapper over two conflicting Headers
that implements Misbehaviour interface expected by ICS-02


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| `client_id` | [string](#string) |  |  |
| `header_1` | [Header](#ibc.lightclients.tendermint.v1.Header) |  |  |
| `header_2` | [Header](#ibc.lightclients.tendermint.v1.Header) |  |  |





 <!-- end messages -->

 <!-- end enums -->

 <!-- end HasExtensions -->

 <!-- end services -->



## Scalar Value Types

| .proto Type | Notes | C++ | Java | Python | Go | C# | PHP | Ruby |
| ----------- | ----- | --- | ---- | ------ | -- | -- | --- | ---- |
| <a name="double" /> double |  | double | double | float | float64 | double | float | Float |
| <a name="float" /> float |  | float | float | float | float32 | float | float | Float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum or Fixnum (as required) |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="bool" /> bool |  | bool | boolean | boolean | bool | bool | boolean | TrueClass/FalseClass |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode | string | string | string | String (UTF-8) |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str | []byte | ByteString | string | String (ASCII-8BIT) |

