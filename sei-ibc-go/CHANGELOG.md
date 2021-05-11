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

### Bug Fixes

* (modules/light-clients/06-solomachine) [\153](https://github.com/cosmos/ibc-go/pull/153) Fix solo machine proof height sequence mismatch bug.
* (modules/light-clients/06-solomachine) [\#122](https://github.com/cosmos/ibc-go/pull/122) Fix solo machine merkle prefix casting bug. 
* (modules/light-clients/06-solomachine) [\#120](https://github.com/cosmos/ibc-go/pull/120) Fix solo machine handshake verification bug. 


### API Breaking

* (module/core/02-client) [\#165](https://github.com/cosmos/ibc-go/pull/165) Remove GetFrozenHeight from the ClientState interface. 
* (modules) [\#166](https://github.com/cosmos/ibc-go/pull/166) Remove GetHeight from the misbehaviour interface. The `consensus_height` attribute has been removed from Misbehaviour events.
* (modules) [\#162](https://github.com/cosmos/ibc-go/pull/162) Remove deprecated Handler types in core IBC and the ICS 20 transfer module. 
* (modules/core) [\#161](https://github.com/cosmos/ibc-go/pull/161) Remove Type(), Route(), GetSignBytes() from 02-client, 03-connection, and 04-channel messages.
* (modules) [\#140](https://github.com/cosmos/ibc-go/pull/140) IsFrozen() client state interface changed to Status(). gRPC `ClientStatus` route added.
* (modules/core) [\#109](https://github.com/cosmos/ibc-go/pull/109) Remove connection and channel handshake CLI commands.
* (modules) [\#107](https://github.com/cosmos/ibc-go/pull/107) Modify OnRecvPacket callback to return an acknowledgement which indicates if it is successful or not. Callback state changes are discarded for unsuccessful acknowledgements only. 
* (modules) [\#108](https://github.com/cosmos/ibc-go/pull/108) All message constructors take the signer as a string to prevent upstream bugs. The `String()` function for an SDK Acc Address relies on external context.

### State Machine Breaking

* (modules/light-clients/07-tendermint) [\#99](https://github.com/cosmos/ibc-go/pull/99) Enforce maximum chain-id length for tendermint client. 
* (modules/light-clients/07-tendermint) [\#141](https://github.com/cosmos/ibc-go/pull/141) Allow a new form of misbehaviour that proves counterparty chain breaks time monotonicity, automatically enforce monotonicity in UpdateClient and freeze client if monotonicity is broken.
* (modules/light-clients/07-tendermint) [\#141](https://github.com/cosmos/ibc-go/pull/141) Freeze the client if there's a conflicting header submitted for an existing consensus state.
* (modules/core/02-client) [\#8405](https://github.com/cosmos/cosmos-sdk/pull/8405) Refactor IBC client update governance proposals to use a substitute client to update a frozen or expired client.
* (modules/core/02-client) [\#8673](https://github.com/cosmos/cosmos-sdk/pull/8673) IBC upgrade logic moved to 02-client and an IBC UpgradeProposal is added.

### Improvements

* (modules/core/04-channel) [\#7949](https://github.com/cosmos/cosmos-sdk/issues/7949) Standardized channel `Acknowledgement` moved to its own file. Codec registration redundancy removed.
* (modules/core/04-channel) [\#144](https://github.com/cosmos/ibc-go/pull/144) Introduced a `packet_data_hex` attribute to emit the hex-encoded packet data in events. This allows for raw binary (proto-encoded message) to be sent over events and decoded correctly on relayer. Original `packet_data` is DEPRECATED. All relayers and IBC event consumers are encouraged to switch to `packet_data_hex` as soon as possible.
* (modules/light-clients/07-tendermint) [\#125](https://github.com/cosmos/ibc-go/pull/125) Implement efficient iteration of consensus states and pruning of earliest expired consensus state on UpdateClient.
* (modules/light-clients/07-tendermint) [\#141](https://github.com/cosmos/ibc-go/pull/141) Return early in case there's a duplicate update call to save Gas.


## IBC in the Cosmos SDK Repository

The IBC module was originally released in [v0.40.0](https://github.com/cosmos/cosmos-sdk/releases/tag/v0.40.0) of the SDK.
Please see the [Release Notes](https://github.com/cosmos/cosmos-sdk/blob/v0.40.0/RELEASE_NOTES.md).

The IBC module is also contained in the releases for [v0.41.x](https://github.com/cosmos/cosmos-sdk/releases/tag/v0.41.0) and [v0.42.x](https://github.com/cosmos/cosmos-sdk/releases/tag/v0.42.0).
Please see the Release Notes for [v0.41.x](https://github.com/cosmos/cosmos-sdk/blob/release/v0.41.x/RELEASE_NOTES.md) and [v0.42.x](https://github.com/cosmos/cosmos-sdk/blob/release/v0.42.x/RELEASE_NOTES.md).
