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
"Client Breaking" for breaking Protobuf, gRPC and REST routes used by end-users.
"CLI Breaking" for breaking CLI commands.
"API Breaking" for breaking exported APIs used by developers building on SDK.
"State Machine Breaking" for any changes that result in a different AppState given same genesisState and txList.
Ref: https://keepachangelog.com/en/1.0.0/
-->

# Changelog
### 3.0.6
### 3.0.6
sei-chain:
*[#944] (https://github.com/sei-protocol/sei-chain/pull/944) Add new configuration for snapshot directory
*[#940] (https://github.com/sei-protocol/sei-chain/pull/940) Use ImmutableAppend for v16 to v17 dex migration

sei-cosmos:
*[#306] (https://github.com/sei-protocol/sei-cosmos/pull/306) Fix dryRun for seid tx

### 3.0.5
sei-chain: 
* [#878] (https://github.com/sei-protocol/sei-chain/pull/878) Fix denom key collision

sei-tendermint: 
* [#149] (https://github.com/sei-protocol/sei-tendermint/pull/149) Fix condition for tx key dissemination

sei-iavl:
* [#32] (https://github.com/sei-protocol/sei-iavl/pull/32) Separate orphan storage

## 3.0.4
sei-chain:
* [#874] (https://github.com/sei-protocol/sei-chain/pull/874) Charge rent after failed Sudo call
* [#869] (https://github.com/sei-protocol/sei-chain/pull/869) Require fee per byte in order data
* [#861] (https://github.com/sei-protocol/sei-chain/pull/861) Fix tokenfactory metadata 

sei-cosmos: 
* [#287] (https://github.com/sei-protocol/sei-cosmos/pull/287) Refactor deferred balance to use memkv
* [#286] (https://github.com/sei-protocol/sei-cosmos/pull/286) Prevent multisig sign with wrong key
* [#284] (https://github.com/sei-protocol/sei-cosmos/pull/284) Fix allowed_msg uncapped spend limit
* [#280] (https://github.com/sei-protocol/sei-cosmos/pull/280) Barberry patch

sei-tendermint:
* [#148] (https://github.com/sei-protocol/sei-tendermint/pull/148) Add sleep to avoid consensus reactor retrying too quickly
* 
## 3.0.3
sei-chain: 
* [#816] (https://github.com/sei-protocol/sei-chain/pull/816) Reenable tx concurrency for non oracle/priority txs

sei-cosmos:
* [#254] (https://github.com/sei-protocol/sei-cosmos/pull/254) Use sequential searching instead of binary search for coins

sei-tendermint:
* [#143] (https://github.com/sei-protocol/sei-tendermint/pull/143) Fix cpu leak for simple pq but stopping timer
* [#140] (https://github.com/sei-protocol/sei-tendermint/pull/140) Add raw logs to tx output

## 3.0.2
sei-chain:
* [#810] (https://github.com/sei-protocol/sei-chain/pull/810) Disable FOK orders
* [#809] (https://github.com/sei-protocol/sei-chain/pull/809) Huckleberry patch
* [#808] (https://github.com/sei-protocol/sei-chain/pull/808) Add global min fees as a param
 
## 3.0.1
sei-chain:
* [#797] (https://github.com/sei-protocol/sei-chain/pull/797) Don't charge gas for loading contract dependencies
* [#792] (https://github.com/sei-protocol/sei-chain/pull/792) Reset block gas meter if concurrent processing fails
* [#791] (https://github.com/sei-protocol/sei-chain/pull/791) Disable skipFastStorageUpgrade to make iavl dump faster
* [#790] (https://github.com/sei-protocol/sei-chain/pull/790) Disable non-prioritized tx concurrency
* [#789] (https://github.com/sei-protocol/sei-chain/pull/789) Adds appropriate READ access for dex contract in antehandler
* [#788] (https://github.com/sei-protocol/sei-chain/pull/788) Clear dex memstate cache when falling back to sequential processing
* [#786] (https://github.com/sei-protocol/sei-chain/pull/786) Add NoVersioning to seid command
* [#781] (https://github.com/sei-protocol/sei-chain/pull/781) Add order limit for price level and pair limit for contracts

tm-db:
* [#2] (https://github.com/sei-protocol/tm-db/pull/2) Load items eagerly to memdb_iterator to avoid deadlock

sei-tendermint:
* [#137] (https://github.com/sei-protocol/sei-tendermint/pull/137) New endpoint to expose lag 

## 3.0.0
sei-chain:
* [#777] (https://github.com/sei-protocol/sei-chain/pull/777) Parallelize Sudo Deposit
* [#771] (https://github.com/sei-protocol/sei-chain/pull/771) Parallelize BeginBlock for x/dex
* [#768] (https://github.com/sei-protocol/sei-chain/pull/768) Add FOK back to order match result
* [#763] (https://github.com/sei-protocol/sei-chain/pull/763) Refactor dex EndBlock to optimize store access

sei-cosmos
* [#240] (https://github.com/sei-protocol/sei-cosmos/pull/239) Add dex contract ACL type
* [#237] (https://github.com/sei-protocol/sei-cosmos/pull/237) Add next-account-numnber cli

sei-tendermint
* [#136] (https://github.com/sei-protocol/sei-tendermint/pull/136) Revert block.Evidence to nested block.Evidence.Evidence
* [#135] (https://github.com/sei-protocol/sei-tendermint/pull/135) Auto switch to blocksync should only start in consensus mode
 
## 2.0.48beta
sei-chain:
* [#743] (https://github.com/sei-protocol/sei-chain/pull/743) Do not unregister contract if out of rent
* [#742] (https://github.com/sei-protocol/sei-chain/pull/742) Add more metrics to dex module
* [#733] (https://github.com/sei-protocol/sei-chain/pull/733) Remove liquidation logic from dex

sei-cosmos
* [#235] (https://github.com/sei-protocol/sei-cosmos/pull/235) Fix x/simulation fee check
* [#234] (https://github.com/sei-protocol/sei-cosmos/pull/234) Add more metrics for Begin/Mid/End Block

sei-tendermint
* [#134] (https://github.com/sei-protocol/sei-tendermint/pull/134) Fix nil peer address map
## 2.0.47beta
sei-chain:
* [#726] (https://github.com/sei-protocol/sei-chain/pull/726) Fix of dex rent transfer issue
* [#723] (https://github.com/sei-protocol/sei-chain/pull/723) Security CW Patch Cherry 
* [#699] (https://github.com/sei-protocol/sei-chain/pull/699) Loadtest update
* [#716] (https://github.com/sei-protocol/sei-chain/pull/716) Sei cluster init script update
* [#725] (https://github.com/sei-protocol/sei-chain/pull/725) DBSync config update
* [#718] (https://github.com/sei-protocol/sei-chain/pull/718) Update mint distriution to be daily
* [#729] (https://github.com/sei-protocol/sei-chain/pull/729) Add gov prop handler for updating current minter 
* [#730] (https://github.com/sei-protocol/sei-chain/pull/730) Add README.md for epoch module
* [#727] (https://github.com/sei-protocol/sei-chain/pull/727) Bump max wasm file size to 2MB
* [#731] (https://github.com/sei-protocol/sei-chain/pull/731) Bump for module to module debug logs
* [#732] (https://github.com/sei-protocol/sei-chain/pull/732) Remove x/nitro from genesis version

sei-cosmos:
* [#231] (https://github.com/sei-protocol/sei-cosmos/pull/231) Typo for m2m debug message
* [#230] (https://github.com/sei-protocol/sei-cosmos/pull/230) Add debug message for module to module transactions
* [#228] (https://github.com/sei-protocol/sei-cosmos/pull/228) Deprecate LoadLatest flag
* [#229] (https://github.com/sei-protocol/sei-cosmos/pull/229) Replace snapshot manager multistore with new one after DBSync

sei-tendermint:
* [#130] (https://github.com/sei-protocol/sei-tendermint/pull/130) Do not run DBSync if there is already a readable app version

## 2.0.46beta
sei-chain:
* [#694] (https://github.com/sei-protocol/sei-chain/pull/694) Register prune command
* [#702] (https://github.com/sei-protocol/sei-chain/pull/702) Change tick failure log to warning

sei-cosmos:
* [#227] (https://github.com/sei-protocol/sei-cosmos/pull/227) Add checkTxResponse log to RPCResponse
* [#224] (https://github.com/sei-protocol/sei-cosmos/pull/224) Default to secp256k1
* [#220] (https://github.com/sei-protocol/sei-cosmos/pull/220) Add admin field to base vesting account
* [#218] (https://github.com/sei-protocol/sei-cosmos/pull/218) Restart node instead of panicking
* [#216] (https://github.com/sei-protocol/sei-cosmos/pull/216) Fix pruning command

sei-tendermint:
* [#118] (https://github.com/sei-protocol/sei-tendermint/pull/118) Add DBSync module

## 2.0.45beta

sei-chain: https://github.com/sei-protocol/sei-chain/compare/2.0.44beta...2.0.45beta-release
* [#666](https://github.com/sei-protocol/sei-chain/pull/666) [DEX] remove BeginBlock/FinalizeBlock sudo hooks
* [#674](https://github.com/sei-protocol/sei-chain/pull/674) Longterm fix for max gas enforcement

sei-cosmos: https://github.com/sei-protocol/sei-cosmos/releases/tag/v0.2.14
* [#210](https://github.com/sei-protocol/sei-cosmos/pull/210) Add levelDB compaction goroutine

sei-tendermint: https://github.com/sei-protocol/sei-tendermint/releases/tag/v0.2.4
* [#110](https://github.com/sei-protocol/sei-tendermint/pull/110) Add more granular buckets for block interval
* [#111](https://github.com/sei-protocol/sei-tendermint/pull/111) Add unused prival pubKey back to node info - fix for IBC on full nodes
* [#113](https://github.com/sei-protocol/sei-tendermint/pull/113) Add metrics label for missing val power

## 2.0.44beta

sei-chain:
* [#658] (https://github.com/sei-protocol/sei-chain/pull/658) Revert EventAttribute fields to byte array

sei-cosmos: https://github.com/sei-protocol/sei-cosmos/compare/sei-cosmos-2.0.42beta...v2.0.43beta-release
* [#204] (https://github.com/sei-protocol/sei-cosmos/pull/204) IBC Compatibility Fix

sei-tendermint: https://github.com/sei-protocol/sei-tendermint/compare/2.0.42beta-release...2.0.43beta-release
* IBC Compatibility Fix
* Bump default max gas limit
- Add metrics & visibility for high block time

## 2.0.42beta

sei-chain:
* [#670] (https://github.com/sei-protocol/sei-chain/pull/670) Add add-wasm-genesis-message to seid
* [#654] (https://github.com/sei-protocol/sei-chain/pull/654) Improve endblock performance and fix trace

sei-cosmos: https://github.com/sei-protocol/sei-cosmos/compare/v0.2.8...v0.2.12
* improvements around monitoring for sei-cosmos
* dont enforce gas limit on deliverTx
* refactor slashing module


sei-tendermint:
* [#95] (https://github.com/sei-protocol/sei-tendermint/pull/95) Patch forging empty merkle tree attack vector
* set default max gas param to 6mil
* log tunning for p2p

## 2.0.40beta - 2023-03-10
* [#646] (https://github.com/sei-protocol/sei-chain/pull/646) Optimizations for FinalizeBlock
* [#644] (https://github.com/sei-protocol/sei-chain/pull/644) [Oak Audit] Add check for non-existent transaction
* [#647] (https://github.com/sei-protocol/sei-chain/pull/647) Fixes to race conditions
* [#638] (https://github.com/sei-protocol/sei-chain/pull/638) Emit Version Related Metrics
* [#636] (https://github.com/sei-protocol/sei-chain/pull/636) Fix deadlock with upgrades
* [#635] (https://github.com/sei-protocol/sei-chain/pull/635) Add event to dex messages

## 2.0.39beta - 2023-03-06
* [#632](https://github.com/sei-protocol/sei-chain/pull/632) Bump Sei-tendermint to reduce log volume
* [#631](https://github.com/sei-protocol/sei-chain/pull/631) Nondeterminism deadlock fixes
* [#630](https://github.com/sei-protocol/sei-chain/pull/630) Mempool configs to avoid node slow down

## 2.0.38beta - 2023-03-04
* [#623](https://github.com/sei-protocol/sei-chain/pull/623) [epoch] Add new epoch events by @udpatil in #623
* [#624](https://github.com/sei-protocol/sei-chain/pull/624) [dex][mint] Add long messages for dex and mint by @udpatil in #624
* [#588](https://github.com/sei-protocol/sei-chain/pull/588) Send deposit funds in message server instead of EndBlock by @codchen in #588
* [#627](https://github.com/sei-protocol/sei-chain/pull/627) [oracle] Add slash window progress query by @udpatil in #627
[label](x/oracle/README.md)* [#625](https://github.com/sei-protocol/sei-chain/pull/625) Update contract rent deposit logic + add query endpoint by @LCyson in #625

## 2.0.37beta - 2023-02-27
### Features
* [#621](https://github.com/sei-protocol/sei-chain/pull/621) Add success count to the oracle query
* [#600](https://github.com/sei-protocol/sei-chain/pull/600) Add params to guard Nitro fraud challenge
* [sei-tendermint #73](https://github.com/sei-protocol/sei-tendermint/pull/73) reduce checktx log noise
### Bug Fixes
* [#617](https://github.com/sei-protocol/sei-chain/pull/617) gracefully handle nil response for new provider
* [#619](https://github.com/sei-protocol/sei-chain/pull/619) Move store operations outside of iterator

## 2.0.36beta - 2023-02-27
### Features
* [#603](https://github.com/sei-protocol/sei-chain/pull/603) Set mempool ttl
### Bug Fixes
* [#612](https://github.com/sei-protocol/sei-chain/pull/612) Optimistic Processing should finish before main goroutine
* [#613](https://github.com/sei-protocol/sei-chain/pull/613) Incorporate IAVL change that removes mutex locking
* Various audit fixes
