<!--
Guiding Principles:

Changelogs are for humans, not machines.
There should be an entry for every single version.
The same types of changes should be grouped.
Versions and sections should be linkable.
The latest version comes first.
The release date of each version is displayed.
Mention whether you follow Semantic Versioning.

Usage:

Change log entries are to be added to the Unreleased section under the
appropriate stanza (see below). Each entry should ideally include a tag and
the Github issue reference in the following format:

* (<tag>) \#<issue-number> message

The issue numbers will later be link-ified during the release process so you do
not have to worry about including a link manually, but you can if you wish.

Types of changes (Stanzas):

"Features" for new features.
"Improvements" for changes in existing functionality.
"Deprecated" for soon-to-be removed features.
"Bug Fixes" for any bug fixes.
"Client Breaking" for breaking CLI commands and REST routes used by end-users.
"API Breaking" for breaking exported APIs used by developers building on SDK.
"State Machine Breaking" for any changes that result in a different AppState given same genesisState and txList.
Ref: https://keepachangelog.com/en/1.0.0/
-->

# Changelog

## [Unreleased]

### API Breaking

* (core) [\#227](https://github.com/cosmos/ibc-go/pull/227) Remove sdk.Result from application callbacks
* (transfer) [\#350](https://github.com/cosmos/ibc-go/pull/350) Change FungibleTokenPacketData to use a string for the Amount field. This enables token transfers with amounts previously restricted by uint64. Up to the maximum uint256 value is supported. 

### State Machine Breaking

### Improvements

* [\#383](https://github.com/cosmos/ibc-go/pull/383) Adds helper functions for merging and splitting middleware versions from the underlying app version.
* (modules/core/05-port) [\#288](https://github.com/cosmos/ibc-go/issues/288) Making the 05-port keeper function IsBound public. The IsBound function checks if the provided portID is already binded to a module.

### Features

* [\#384](https://github.com/cosmos/ibc-go/pull/384) Added `NegotiateAppVersion` method to `IBCModule` interface supported by a gRPC query service in `05-port`. This provides routing of requests to the desired application module callback, which in turn performs application version negotiation.

## [v1.2.0](https://github.com/cosmos/ibc-go/releases/tag/v1.2.0) - 2021-09-10

### State Machine Breaking

* (24-host) [\#344](https://github.com/cosmos/ibc-go/pull/344) Increase port identifier limit to 128 characters.

### Improvements

* [\#373](https://github.com/cosmos/ibc-go/pull/375) Added optional field `PacketCommitmentSequences` to `QueryPacketAcknowledgementsRequest` to provide filtering of packet acknowledgements.

### Features

* [\#372](https://github.com/cosmos/ibc-go/pull/372) New CLI command `query ibc client status <client id>` to get the current activity status of a client.

### Dependencies

* [\#386](https://github.com/cosmos/ibc-go/pull/386) Bump [tendermint](https://github.com/tendermint/tendermint) from 0.34.12 to 0.34.13.

## [v1.1.0](https://github.com/cosmos/ibc-go/releases/tag/v1.1.0) - 2021-09-03

### Dependencies

* [\#367](https://github.com/cosmos/ibc-go/pull/367) Bump [cosmos-sdk](https://github.com/cosmos/cosmos-sdk) from 0.43 to 0.44.

## [v1.0.1](https://github.com/cosmos/ibc-go/releases/tag/v1.0.1) - 2021-08-25

### Improvements

* [\#343](https://github.com/cosmos/ibc-go/pull/343) Create helper functions for publishing of packet sent and acknowledgement sent events.

## [v1.0.0](https://github.com/cosmos/ibc-go/releases/tag/v1.0.0) - 2021-08-10

### Bug Fixes

* (07-tendermint) [\#241](https://github.com/cosmos/ibc-go/pull/241) Ensure tendermint client state latest height revision number matches chain id revision number.
* (07-tendermint) [\#234](https://github.com/cosmos/ibc-go/pull/234) Use sentinel value for the consensus state root set during a client upgrade. This prevents genesis validation from failing.
* (modules) [\#223](https://github.com/cosmos/ibc-go/pull/223) Use correct Prometheus format for metric labels.
* (06-solomachine) [\#214](https://github.com/cosmos/ibc-go/pull/214) Disable defensive timestamp check in SendPacket for solo machine clients.
* (07-tendermint) [\#210](https://github.com/cosmos/ibc-go/pull/210) Export all consensus metadata on genesis restarts for tendermint clients.
* (core) [\#200](https://github.com/cosmos/ibc-go/pull/200) Fixes incorrect export of IBC identifier sequences. Previously, the next identifier sequence for clients/connections/channels was not set during genesis export. This resulted in the next identifiers being generated on the new chain to reuse old identifiers (the sequences began again from 0).
* (02-client) [\#192](https://github.com/cosmos/ibc-go/pull/192) Fix IBC `query ibc client header` cli command. Support historical queries for query header/node-state commands.
* (modules/light-clients/06-solomachine) [\#153](https://github.com/cosmos/ibc-go/pull/153) Fix solo machine proof height sequence mismatch bug.
* (modules/light-clients/06-solomachine) [\#122](https://github.com/cosmos/ibc-go/pull/122) Fix solo machine merkle prefix casting bug. 
* (modules/light-clients/06-solomachine) [\#120](https://github.com/cosmos/ibc-go/pull/120) Fix solo machine handshake verification bug. 
* (modules/light-clients/06-solomachine) [\#153](https://github.com/cosmos/ibc-go/pull/153) fix solo machine connection handshake failure at `ConnectionOpenAck`.

### API Breaking

* (04-channel) [\#220](https://github.com/cosmos/ibc-go/pull/220) Channel legacy handler functions were removed. Please use the MsgServer functions or directly call the channel keeper's handshake function.
* (modules) [\#206](https://github.com/cosmos/ibc-go/pull/206) Expose `relayer sdk.AccAddress` on `OnRecvPacket`, `OnAcknowledgementPacket`, `OnTimeoutPacket` module callbacks to enable incentivization.
* (02-client) [\#181](https://github.com/cosmos/ibc-go/pull/181) Remove 'InitialHeight' from UpdateClient Proposal. Only copy over latest consensus state from substitute client.
* (06-solomachine) [\#169](https://github.com/cosmos/ibc-go/pull/169) Change FrozenSequence to boolean in solomachine ClientState. The solo machine proto package has been bumped from `v1` to `v2`.
* (module/core/02-client) [\#165](https://github.com/cosmos/ibc-go/pull/165) Remove GetFrozenHeight from the ClientState interface. 
* (modules) [\#166](https://github.com/cosmos/ibc-go/pull/166) Remove GetHeight from the misbehaviour interface. The `consensus_height` attribute has been removed from Misbehaviour events.
* (modules) [\#162](https://github.com/cosmos/ibc-go/pull/162) Remove deprecated Handler types in core IBC and the ICS 20 transfer module. 
* (modules/core) [\#161](https://github.com/cosmos/ibc-go/pull/161) Remove Type(), Route(), GetSignBytes() from 02-client, 03-connection, and 04-channel messages.
* (modules) [\#140](https://github.com/cosmos/ibc-go/pull/140) IsFrozen() client state interface changed to Status(). gRPC `ClientStatus` route added.
* (modules/core) [\#109](https://github.com/cosmos/ibc-go/pull/109) Remove connection and channel handshake CLI commands.
* (modules) [\#107](https://github.com/cosmos/ibc-go/pull/107) Modify OnRecvPacket callback to return an acknowledgement which indicates if it is successful or not. Callback state changes are discarded for unsuccessful acknowledgements only. 
* (modules) [\#108](https://github.com/cosmos/ibc-go/pull/108) All message constructors take the signer as a string to prevent upstream bugs. The `String()` function for an SDK Acc Address relies on external context.
* (transfer) [\#275](https://github.com/cosmos/ibc-go/pull/275) Remove 'ChanCloseInit' function from transfer keeper. ICS20 does not close channels.

### State Machine Breaking

* (modules/light-clients/07-tendermint) [\#99](https://github.com/cosmos/ibc-go/pull/99) Enforce maximum chain-id length for tendermint client. 
* (modules/light-clients/07-tendermint) [\#141](https://github.com/cosmos/ibc-go/pull/141) Allow a new form of misbehaviour that proves counterparty chain breaks time monotonicity, automatically enforce monotonicity in UpdateClient and freeze client if monotonicity is broken.
* (modules/light-clients/07-tendermint) [\#141](https://github.com/cosmos/ibc-go/pull/141) Freeze the client if there's a conflicting header submitted for an existing consensus state.
* (modules/core/02-client) [\#8405](https://github.com/cosmos/cosmos-sdk/pull/8405) Refactor IBC client update governance proposals to use a substitute client to update a frozen or expired client.
* (modules/core/02-client) [\#8673](https://github.com/cosmos/cosmos-sdk/pull/8673) IBC upgrade logic moved to 02-client and an IBC UpgradeProposal is added.
* (modules/core/03-connection) [\#171](https://github.com/cosmos/ibc-go/pull/171) Introduces a new parameter `MaxExpectedTimePerBlock` to allow connections to calculate and enforce a block delay that is proportional to time delay set by connection.
* (core) [\#268](https://github.com/cosmos/ibc-go/pull/268) Perform a no-op on redundant relay messages. Previous behaviour returned an error. Now no state change will occur and no error will be returned.

### Improvements

* (04-channel) [\#220](https://github.com/cosmos/ibc-go/pull/220) Channel handshake events are now emitted with the channel keeper.
* (core/02-client) [\#205](https://github.com/cosmos/ibc-go/pull/205) Add in-place and genesis migrations from SDK v0.42.0 to ibc-go v1.0.0. Solo machine protobuf defintions are migrated from v1 to v2. All solo machine consensus states are pruned. All expired tendermint consensus states are pruned.
* (modules/core) [\#184](https://github.com/cosmos/ibc-go/pull/184) Improve error messages. Uses unique error codes to indicate already relayed packets.
* (07-tendermint) [\#182](https://github.com/cosmos/ibc-go/pull/182) Remove duplicate checks in upgrade logic.
* (modules/core/04-channel) [\#7949](https://github.com/cosmos/cosmos-sdk/issues/7949) Standardized channel `Acknowledgement` moved to its own file. Codec registration redundancy removed.
* (modules/core/04-channel) [\#144](https://github.com/cosmos/ibc-go/pull/144) Introduced a `packet_data_hex` attribute to emit the hex-encoded packet data in events. This allows for raw binary (proto-encoded message) to be sent over events and decoded correctly on relayer. Original `packet_data` is DEPRECATED. All relayers and IBC event consumers are encouraged to switch to `packet_data_hex` as soon as possible.
* (core/04-channel) [\#197](https://github.com/cosmos/ibc-go/pull/197) Introduced a `packet_ack_hex` attribute to emit the hex-encoded acknowledgement in events. This allows for raw binary (proto-encoded message) to be sent over events and decoded correctly on relayer. Original `packet_ack` is DEPRECATED. All relayers and IBC event consumers are encouraged to switch to `packet_ack_hex` as soon as possible.
* (modules/light-clients/07-tendermint) [\#125](https://github.com/cosmos/ibc-go/pull/125) Implement efficient iteration of consensus states and pruning of earliest expired consensus state on UpdateClient.
* (modules/light-clients/07-tendermint) [\#141](https://github.com/cosmos/ibc-go/pull/141) Return early in case there's a duplicate update call to save Gas.
* (modules/core/ante) [\#235](https://github.com/cosmos/ibc-go/pull/235) Introduces a new IBC Antedecorator that will reject transactions that only contain redundant packet messages (and accompany UpdateClient msgs). This will prevent relayers from wasting fees by submitting messages for packets that have already been processed by previous relayer(s). The Antedecorator is only applied on CheckTx and RecheckTx and is therefore optional for each node.

### Features

* [\#198](https://github.com/cosmos/ibc-go/pull/198) New CLI command `query ibc-transfer escrow-address <port> <channel id>` to get the escrow address for a channel; can be used to then query balance of escrowed tokens

### Client Breaking Changes

* (02-client/cli) [\#196](https://github.com/cosmos/ibc-go/pull/196) Rename `node-state` cli command to `self-consensus-state`.

## IBC in the Cosmos SDK Repository

The IBC module was originally released in [v0.40.0](https://github.com/cosmos/cosmos-sdk/releases/tag/v0.40.0) of the SDK.
Please see the [Release Notes](https://github.com/cosmos/cosmos-sdk/blob/v0.40.0/RELEASE_NOTES.md).

The IBC module is also contained in the releases for [v0.41.x](https://github.com/cosmos/cosmos-sdk/releases/tag/v0.41.0) and [v0.42.x](https://github.com/cosmos/cosmos-sdk/releases/tag/v0.42.0).
Please see the Release Notes for [v0.41.x](https://github.com/cosmos/cosmos-sdk/blob/v0.41.0/RELEASE_NOTES.md) and [v0.42.x](https://github.com/cosmos/cosmos-sdk/blob/v0.42.0/RELEASE_NOTES.md).

The IBC module was removed in the commit hash [da064e13d56add466548135739c5860a9f7ed842](https://github.com/cosmos/cosmos-sdk/commit/da064e13d56add466548135739c5860a9f7ed842) on the SDK. The release for SDK v0.43.0 will be the first release without the IBC module.

Backports should be made to the [release/v0.42.x](https://github.com/cosmos/cosmos-sdk/tree/release/v0.42.x) branch on the SDK.
