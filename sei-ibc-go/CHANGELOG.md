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

* (modules) [\#107](https://github.com/cosmos/ibc-go/pull/107) Modify OnRecvPacket callback to return an acknowledgement which indicates if it is successful or not. Callback state changes are discarded for unsuccessful acknowledgements only. 
* (modules) [\#108](https://github.com/cosmos/ibc-go/pull/108) All message constructors take the signer as a string to prevent upstream bugs. The `String()` function for an SDK Acc Address relies on external context.

### State Machine Breaking

* (modules/light-clients/07-tendermint) [\#99](https://github.com/cosmos/ibc-go/pull/99) Enforce maximum chain-id length for tendermint client. 
* (modules/core/02-client) [\#8405](https://github.com/cosmos/cosmos-sdk/pull/8405) Refactor IBC client update governance proposals to use a substitute client to update a frozen or expired client.
* (modules/core/02-client) [\#8673](https://github.com/cosmos/cosmos-sdk/pull/8673) IBC upgrade logic moved to 02-client and an IBC UpgradeProposal is added.

### Improvements

* (modules/core/04-channel) [\#7949](https://github.com/cosmos/cosmos-sdk/issues/7949) Standardized channel `Acknowledgement` moved to its own file. Codec registration redundancy removed.

## IBC in the Cosmos SDK Repository

The IBC module was originally released in [v0.40.0](https://github.com/cosmos/cosmos-sdk/releases/tag/v0.40.0) of the SDK.
Please see the [Release Notes](https://github.com/cosmos/cosmos-sdk/blob/v0.40.0/RELEASE_NOTES.md).

The IBC module is also contained in the releases for [v0.41.x](https://github.com/cosmos/cosmos-sdk/releases/tag/v0.41.0) and [v0.42.x](https://github.com/cosmos/cosmos-sdk/releases/tag/v0.42.0).
Please see the Release Notes for [v0.41.x](https://github.com/cosmos/cosmos-sdk/blob/release/v0.41.x/RELEASE_NOTES.md) and [v0.42.x](https://github.com/cosmos/cosmos-sdk/blob/release/v0.42.x/RELEASE_NOTES.md).
