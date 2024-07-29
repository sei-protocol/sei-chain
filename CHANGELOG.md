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
## v5.7.1 & v5.7.2
sei-chain
* [#1779](https://github.com/sei-protocol/sei-chain/pull/1779) Fix subscribe logs empty params crash
* [#1783](https://github.com/sei-protocol/sei-chain/pull/1783) Add meaningful message for eth_call balance override overflow
* [#1783](https://github.com/sei-protocol/sei-chain/pull/1784) Fix log index on synthetic receipt
* [#1775](https://github.com/sei-protocol/sei-chain/pull/1775) Disallow sending to direct cast addr after association

sei-wasmd
* [60](https://github.com/sei-protocol/sei-wasmd/pull/60) Query penalty fixes

sei-tendermint
* [#237](https://github.com/sei-protocol/sei-tendermint/pull/237) Add metrics for total txs bytes in mempool

## v5.7.0
sei-chain
* [#1731](https://github.com/sei-protocol/sei-chain/pull/1731) Remove 1-hop limit
* [#1663](https://github.com/sei-protocol/sei-chain/pull/1663) Retain pointer address on upgrade

## v5.6.0
sei-chain
* [#1690](https://github.com/sei-protocol/sei-chain/pull/1690) Use transient store for EVM deferred info
* [#1742](https://github.com/sei-protocol/sei-chain/pull/1742) \[EVM\] Add transient receipts with eventual flush to store
* [#1744](https://github.com/sei-protocol/sei-chain/pull/1744) Only emit cosmos events if no error in precompiles
* [#1737](https://github.com/sei-protocol/sei-chain/pull/1737) Only send unlocked tokens upon address association
* [#1740](https://github.com/sei-protocol/sei-chain/pull/1740) Update Random to Hash of Block Timestamp
* [#1734](https://github.com/sei-protocol/sei-chain/pull/1734) Add migration to unwind dex state
* [#1736](https://github.com/sei-protocol/sei-chain/pull/1736) Create account for sendNative receiver
* [#1738](https://github.com/sei-protocol/sei-chain/pull/1738) Reduce Default TTL configs
* [#1733](https://github.com/sei-protocol/sei-chain/pull/1733) Update getBlockReceipts to accept block hash
* [#1732](https://github.com/sei-protocol/sei-chain/pull/1732) Show empty trace on insufficient funds error
* [#1727](https://github.com/sei-protocol/sei-chain/pull/1727) \[EVM\] Add association error metric
* [#1728](https://github.com/sei-protocol/sei-chain/pull/1728) Make occ caused evm panics less noisy
* [#1719](https://github.com/sei-protocol/sei-chain/pull/1719) Fixes local network in /scripts/run-node.py


sei-cosmos
* [#521](https://github.com/sei-protocol/sei-cosmos/pull/521) add DeliverTx hook
* [#520](https://github.com/sei-protocol/sei-cosmos/pull/520) Add callback for receipt storage
* [#517](https://github.com/sei-protocol/sei-cosmos/pull/517) Fix metric name for chain state size
* [#516](https://github.com/sei-protocol/sei-cosmos/pull/516) add EVM event manager to context


sei-wasmd
* [#54](https://github.com/sei-protocol/sei-wasmd/pull/54) Update wasm query behavior upon error


sei-tendermint
* [238](https://github.com/sei-protocol/sei-tendermint/pull/238) Make RPC timeout configurable
* [219](https://github.com/sei-protocol/sei-tendermint/pull/219) Add metrics for mempool change


## v5.5.5
sei-chain
* [#1726](https://github.com/sei-protocol/sei-chain/pull/1726) Handle VM error code properly
* [#1713](https://github.com/sei-protocol/sei-chain/pull/1713) RPC Get Evm Hash
* [#1711](https://github.com/sei-protocol/sei-chain/pull/1711) Add gov proposal v2 for native pointer
* [#1694](https://github.com/sei-protocol/sei-chain/pull/1694) Add native associate tx type


sei-cosmos
* [#511](https://github.com/sei-protocol/sei-cosmos/pull/511) Add error for evm revert


## v5.5.2
sei-chain
* [#1685](https://github.com/sei-protocol/sei-chain/pull/1685) Add EVM support to v5.5.2

## v5.4.0
sei-chain
* [#1671](https://github.com/sei-protocol/sei-chain/pull/1671) Update and fixes to ERC721 contract
* [#1672](https://github.com/sei-protocol/sei-chain/pull/1672) Add sei_getCosmosTx endpoint
* [#1669](https://github.com/sei-protocol/sei-chain/pull/1669) Add ERC/CW 2981 in pointe
* [#1668](https://github.com/sei-protocol/sei-chain/pull/1673) Bring CW721 pointer contract up to spec
* [#1662](https://github.com/sei-protocol/sei-chain/pull/1662) Add memo support to ibc compiles
* [#1661](https://github.com/sei-protocol/sei-chain/pull/1661) Do not modify original value passed in executeBatch call

sei-cosmos
*[#505](https://github.com/sei-protocol/sei-cosmos/pull/505) Fix export genesis for historical height
*[#506](https://github.com/sei-protocol/sei-cosmos/pull/506) Allow reading pairs in changeset before flush

sei-wasmd
*[#50](https://github.com/sei-protocol/sei-wasmd/pull/50) Changes to fix runtime gas and add paramsKeeper to wasmKeeper for query gas multiplier

## v5.2.0
sei-chain
* [#1621](https://github.com/sei-protocol/sei-chain/pull/1621) Add websocket metrics
* [#1619](https://github.com/sei-protocol/sei-chain/pull/1619) Limit number of subscriptions
* [#1618](https://github.com/sei-protocol/sei-chain/pull/1618) Fix contract deploy receipts
* [#1615](https://github.com/sei-protocol/sei-chain/pull/1615) Optimize websocket newHead by reusing tendermint subscription
* [#1609](https://github.com/sei-protocol/sei-chain/pull/1609) Add association logic to simulate endpoints
* [#1605](https://github.com/sei-protocol/sei-chain/pull/1605) Disallow sr25519 addresses for evm functions
* [#1606](https://github.com/sei-protocol/sei-chain/pull/1606) SKip evm antehandler on sr25519 signatures

sei-cosmos:
* [#495](https://github.com/sei-protocol/sei-cosmos/pull/495) Fix seid keys list by ignoring evm-addr for sr25519
* [#493](https://github.com/sei-protocol/sei-cosmos/pull/493) Remove non-multiplier gas meter

sei-tendermint:
* [#235](https://github.com/sei-protocol/sei-tendermint/pull/235) Check removed including wrapped tx state

sei-db:
* [#63](https://github.com/sei-protocol/sei-db/pull/63) Fix edge case for iterating over tombstoned value

## v5.0.1
sei-chain
[#1577](https://github.com/sei-protocol/sei-chain/pull/1577) Re-enable Cancun

## v5.0.0
sei-chain:
[Compare v3.9.0...v5.0.0](https://github.com/sei-protocol/sei-chain/compare/v3.9.0...008ff68)

sei-cosmos:
[Compare v0.2.84...v0.3.1](https://github.com/sei-protocol/sei-cosmos/compare/v0.2.83...v0.3.1)

sei-tendermint:
[Compare v0.2.40...v0.3.0](https://github.com/sei-protocol/sei-tendermint/compare/v0.2.40...v0.3.0)


## v3.9.0
sei-chain:
* [#1565](https://github.com/sei-protocol/sei-chain/pull/1565) Cosmos Gas Multiplier Params
* [#1444](https://github.com/sei-protocol/sei-chain/pull/1444) Adding tokenfactory denom metadata endpoint

sei-cosmos:
* [#489](https://github.com/sei-protocol/sei-cosmos/pull/489) Cosmos Gas Multiplier Params
* [#477](https://github.com/sei-protocol/sei-cosmos/pull/477) [OCC] if synchronous, reset non-pending

sei-tendermint:
* [211](https://github.com/sei-protocol/sei-tendermint/pull/211) Replay events during restart to avoid tx missing

sei-db:
* [#62](https://github.com/sei-protocol/sei-db/pull/62) Set CreateIfMissing to false when copyExisting

sei-wasmd:
* [45](https://github.com/sei-protocol/sei-wasmd/pull/45) Update LimitSimulationGasDecorator with custom Gas Meter Setter
* [44](https://github.com/sei-protocol/sei-wasmd/pull/44) Bump wasmvm to v1.5.2

## v3.8.0
sei-tendermint:
* [209](https://github.com/sei-protocol/sei-tendermint/pull/209) Use write-lock in (*TxPriorityQueue).ReapMax funcs

sei-db:
* [#61](https://github.com/sei-protocol/sei-db/pull/61) LoadVersion should open DB with read only

sei-wasmd:
* [41](https://github.com/sei-protocol/sei-wasmd/pull/42) Bump wasmd version

## v3.7.0
sei-chain:
* [#1283](https://github.com/sei-protocol/sei-chain/pull/1283) Update synchronous execution to set tx indices properly
* [#1325](https://github.com/sei-protocol/sei-chain/pull/1325) Oracle price feeder ignore error for vote already exist

sei-cosmos:
* [#401](https://github.com/sei-protocol/sei-cosmos/pull/401) Ensure Panic Recovery in Prepare & Process Handlers
* [#404](https://github.com/sei-protocol/sei-cosmos/pull/404) No longer disable dynamic dep generation
* [#411](https://github.com/sei-protocol/sei-cosmos/pull/411) Fix concurrent map access for seidb
* [#424](https://github.com/sei-protocol/sei-cosmos/pull/424) Fix SS apply changeset version off by 1

## v3.6.1
sei-chain:
* [#1204](https://github.com/sei-protocol/sei-chain/pull/1204) Cleanup removed oracle feeds
* [#1196](https://github.com/sei-protocol/sei-chain/pull/1196) Add panic handler in dex endblock
* [#1170](https://github.com/sei-protocol/sei-chain/pull/1170) Integrate SeiDB into Sei Chain

sei-cosmos:
* [#391](https://github.com/sei-protocol/sei-cosmos/pull/391) Fix potential memory leak due to emitting events
* [#388](https://github.com/sei-protocol/sei-cosmos/pull/388) Improve cachekv write performance
* [#385](https://github.com/sei-protocol/sei-cosmos/pull/385) Add params to disable seqno
* [#373](https://github.com/sei-protocol/sei-cosmos/pull/373) Add root multistore v2 for SeiDB

sei-tendermint:
* [#175](https://github.com/sei-protocol/sei-tendermint/pull/175) Fix self remediation bug for block sync

## v3.5.0
sei-chain:
* [#1164](https://github.com/sei-protocol/sei-chain/pull/1164) Bump wasmd
* [#1163](https://github.com/sei-protocol/sei-chain/pull/1163) Update antehandler
* [#1160](https://github.com/sei-protocol/sei-chain/pull/1160) Allow metrics script to query remote
* [#1156](https://github.com/sei-protocol/sei-chain/pull/1156) Bump ledger version to support nano s
* [#1155](https://github.com/sei-protocol/sei-chain/pull/1155) Allow loadtest client to take a list of grpc endpoints

sei-cosmos:
* [#383](https://github.com/sei-protocol/sei-cosmos/pull/383) Refactor wasm dependency behavior
* [#353](https://github.com/sei-protocol/sei-cosmos/pull/353) Perf: Relax locking contention for cache and cachekv
* [#331](https://github.com/sei-protocol/sei-cosmos/pull/331) Fast reject invalid consensus params

sei-tendermint:
* [#170](https://github.com/sei-protocol/sei-tendermint/pull/170) P2P: Optimize block pool requester retry and peer pick up logic
* [#167](https://github.com/sei-protocol/sei-tendermint/pull/167) Perf: Increase buffer size for pubsub server to boost performance
* [#164](https://github.com/sei-protocol/sei-tendermint/pull/164) Add regex support to query syntax
* [#163](https://github.com/sei-protocol/sei-tendermint/pull/163) Reduce noisy tendermint logs
* [#162](https://github.com/sei-protocol/sei-tendermint/pull/162) Use peermanager scores for blocksync peers and don't error out on block mismatch

## v3.3.0
sei-ibc-go:
* [#35](https://github.com/sei-protocol/sei-ibc-go/pull/35) Upgrade to Ibc v3.4.0

## v3.2.1
sei-chain:
* [#1073](https://github.com/sei-protocol/sei-chain/pull/1073) Add timestamp to oracle exchange rates

sei-cosmos:
* [#320](https://github.com/sei-protocol/sei-cosmos/pull/320) Allow minor relase upgrades prior to upgrade height

sei-tendermint:
* [#158](https://github.com/sei-protocol/sei-tendermint/pull/158) Add metrics for peermanager scores
* [#157](https://github.com/sei-protocol/sei-tendermint/pull/157) Fix findNewPrimary never timing out upon encountering poor witnesses
* [#156](https://github.com/sei-protocol/sei-tendermint/pull/156) Remove bad witness and don't block on all witnesses for ConsensusParams

## v3.1.1
sei-ibc-go:
* [#34](https://github.com/sei-protocol/sei-ibc-go/pull/34) Upgrade to Ibc v3.2.0

## v3.0.9
* [#154](https://github.com/sei-protocol/sei-tendermint/pull/154) Fix empty prevote latency metrics

## 3.0.8
sei-chain:
* [#1018](https://github.com/sei-protocol/sei-chain/pull/1018) Reorder tx results into absolute order
## 3.0.7
sei-chain:
* [#1002](https://github.com/sei-protocol/sei-chain/pull/1002) Tokenfactory Query Wasmbindings
* [#989](https://github.com/sei-protocol/sei-chain/pull/989) Add CLI/wasmbinding to set tokenfactory metadata
* [#963](https://github.com/sei-protocol/sei-chain/pull/963) Add SetMetadata to tokenfactory

sei-cosmos:
* [#308](https://github.com/sei-protocol/sei-cosmos/pull/308) Add NoConsumptionInfiniteGasMeter

## 3.0.6
sei-chain:
* [#944](https://github.com/sei-protocol/sei-chain/pull/944) Add new configuration for snapshot directory
* [#940](https://github.com/sei-protocol/sei-chain/pull/940) Use ImmutableAppend for v16 to v17 dex migration

sei-cosmos:
* [#306](https://github.com/sei-protocol/sei-cosmos/pull/306) Fix dryRun for seid tx

## 3.0.5
sei-chain:
* [#878](https://github.com/sei-protocol/sei-chain/pull/878) Fix denom key collision

sei-tendermint:
* [#149](https://github.com/sei-protocol/sei-tendermint/pull/149) Fix condition for tx key dissemination

sei-iavl:
* [#32](https://github.com/sei-protocol/sei-iavl/pull/32) Separate orphan storage

## 3.0.4
sei-chain:
* [#874](https://github.com/sei-protocol/sei-chain/pull/874) Charge rent after failed Sudo call
* [#869](https://github.com/sei-protocol/sei-chain/pull/869) Require fee per byte in order data
* [#861](https://github.com/sei-protocol/sei-chain/pull/861) Fix tokenfactory metadata

sei-cosmos:
* [#287](https://github.com/sei-protocol/sei-cosmos/pull/287) Refactor deferred balance to use memkv
* [#286](https://github.com/sei-protocol/sei-cosmos/pull/286) Prevent multisig sign with wrong key
* [#284](https://github.com/sei-protocol/sei-cosmos/pull/284) Fix allowed_msg uncapped spend limit
* [#280](https://github.com/sei-protocol/sei-cosmos/pull/280) Barberry patch

sei-tendermint:
* [#148](https://github.com/sei-protocol/sei-tendermint/pull/148) Add sleep to avoid consensus reactor retrying too quickly
*
## 3.0.3
sei-chain:
* [#816](https://github.com/sei-protocol/sei-chain/pull/816) Reenable tx concurrency for non oracle/priority txs

sei-cosmos:
* [#254](https://github.com/sei-protocol/sei-cosmos/pull/254) Use sequential searching instead of binary search for coins

sei-tendermint:
* [#143](https://github.com/sei-protocol/sei-tendermint/pull/143) Fix cpu leak for simple pq but stopping timer
* [#140](https://github.com/sei-protocol/sei-tendermint/pull/140) Add raw logs to tx output

## 3.0.2
sei-chain:
* [#810](https://github.com/sei-protocol/sei-chain/pull/810) Disable FOK orders
* [#809](https://github.com/sei-protocol/sei-chain/pull/809) Huckleberry patch
* [#808](https://github.com/sei-protocol/sei-chain/pull/808) Add global min fees as a param

## 3.0.1
sei-chain:
* [#797](https://github.com/sei-protocol/sei-chain/pull/797) Don't charge gas for loading contract dependencies
* [#792](https://github.com/sei-protocol/sei-chain/pull/792) Reset block gas meter if concurrent processing fails
* [#791](https://github.com/sei-protocol/sei-chain/pull/791) Disable skipFastStorageUpgrade to make iavl dump faster
* [#790](https://github.com/sei-protocol/sei-chain/pull/790) Disable non-prioritized tx concurrency
* [#789](https://github.com/sei-protocol/sei-chain/pull/789) Adds appropriate READ access for dex contract in antehandler
* [#788](https://github.com/sei-protocol/sei-chain/pull/788) Clear dex memstate cache when falling back to sequential processing
* [#786](https://github.com/sei-protocol/sei-chain/pull/786) Add NoVersioning to seid command
* [#781](https://github.com/sei-protocol/sei-chain/pull/781) Add order limit for price level and pair limit for contracts

tm-db:
* [#2](https://github.com/sei-protocol/tm-db/pull/2) Load items eagerly to memdb_iterator to avoid deadlock

sei-tendermint:
* [#137](https://github.com/sei-protocol/sei-tendermint/pull/137) New endpoint to expose lag

## 3.0.0
sei-chain:
* [#777](https://github.com/sei-protocol/sei-chain/pull/777) Parallelize Sudo Deposit
* [#771](https://github.com/sei-protocol/sei-chain/pull/771) Parallelize BeginBlock for x/dex
* [#768](https://github.com/sei-protocol/sei-chain/pull/768) Add FOK back to order match result
* [#763](https://github.com/sei-protocol/sei-chain/pull/763) Refactor dex EndBlock to optimize store access

sei-cosmos
* [#240](https://github.com/sei-protocol/sei-cosmos/pull/239) Add dex contract ACL type
* [#237](https://github.com/sei-protocol/sei-cosmos/pull/237) Add next-account-numnber cli

sei-tendermint
* [#136](https://github.com/sei-protocol/sei-tendermint/pull/136) Revert block.Evidence to nested block.Evidence.Evidence
* [#135](https://github.com/sei-protocol/sei-tendermint/pull/135) Auto switch to blocksync should only start in consensus mode

## 2.0.48beta
sei-chain:
* [#743](https://github.com/sei-protocol/sei-chain/pull/743) Do not unregister contract if out of rent
* [#742](https://github.com/sei-protocol/sei-chain/pull/742) Add more metrics to dex module
* [#733](https://github.com/sei-protocol/sei-chain/pull/733) Remove liquidation logic from dex

sei-cosmos
* [#235](https://github.com/sei-protocol/sei-cosmos/pull/235) Fix x/simulation fee check
* [#234](https://github.com/sei-protocol/sei-cosmos/pull/234) Add more metrics for Begin/Mid/End Block

sei-tendermint
* [#134](https://github.com/sei-protocol/sei-tendermint/pull/134) Fix nil peer address map
## 2.0.47beta
sei-chain:
* [#726](https://github.com/sei-protocol/sei-chain/pull/726) Fix of dex rent transfer issue
* [#723](https://github.com/sei-protocol/sei-chain/pull/723) Security CW Patch Cherry
* [#699](https://github.com/sei-protocol/sei-chain/pull/699) Loadtest update
* [#716](https://github.com/sei-protocol/sei-chain/pull/716) Sei cluster init script update
* [#725](https://github.com/sei-protocol/sei-chain/pull/725) DBSync config update
* [#718](https://github.com/sei-protocol/sei-chain/pull/718) Update mint distriution to be daily
* [#729](https://github.com/sei-protocol/sei-chain/pull/729) Add gov prop handler for updating current minter
* [#730](https://github.com/sei-protocol/sei-chain/pull/730) Add README.md for epoch module
* [#727](https://github.com/sei-protocol/sei-chain/pull/727) Bump max wasm file size to 2MB
* [#731](https://github.com/sei-protocol/sei-chain/pull/731) Bump for module to module debug logs
* [#732](https://github.com/sei-protocol/sei-chain/pull/732) Remove x/nitro from genesis version

sei-cosmos:
* [#231](https://github.com/sei-protocol/sei-cosmos/pull/231) Typo for m2m debug message
* [#230](https://github.com/sei-protocol/sei-cosmos/pull/230) Add debug message for module to module transactions
* [#228](https://github.com/sei-protocol/sei-cosmos/pull/228) Deprecate LoadLatest flag
* [#229](https://github.com/sei-protocol/sei-cosmos/pull/229) Replace snapshot manager multistore with new one after DBSync

sei-tendermint:
* [#130](https://github.com/sei-protocol/sei-tendermint/pull/130) Do not run DBSync if there is already a readable app version

## 2.0.46beta
sei-chain:
* [#694](https://github.com/sei-protocol/sei-chain/pull/694) Register prune command
* [#702](https://github.com/sei-protocol/sei-chain/pull/702) Change tick failure log to warning

sei-cosmos:
* [#227](https://github.com/sei-protocol/sei-cosmos/pull/227) Add checkTxResponse log to RPCResponse
* [#224](https://github.com/sei-protocol/sei-cosmos/pull/224) Default to secp256k1
* [#220](https://github.com/sei-protocol/sei-cosmos/pull/220) Add admin field to base vesting account
* [#218](https://github.com/sei-protocol/sei-cosmos/pull/218) Restart node instead of panicking
* [#216](https://github.com/sei-protocol/sei-cosmos/pull/216) Fix pruning command

sei-tendermint:
* [#118](https://github.com/sei-protocol/sei-tendermint/pull/118) Add DBSync module

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
* [#658](https://github.com/sei-protocol/sei-chain/pull/658) Revert EventAttribute fields to byte array

sei-cosmos: https://github.com/sei-protocol/sei-cosmos/compare/sei-cosmos-2.0.42beta...v2.0.43beta-release
* [#204](https://github.com/sei-protocol/sei-cosmos/pull/204) IBC Compatibility Fix

sei-tendermint: https://github.com/sei-protocol/sei-tendermint/compare/2.0.42beta-release...2.0.43beta-release
* IBC Compatibility Fix
* Bump default max gas limit
- Add metrics & visibility for high block time

## 2.0.42beta

sei-chain:
* [#670](https://github.com/sei-protocol/sei-chain/pull/670) Add add-wasm-genesis-message to seid
* [#654](https://github.com/sei-protocol/sei-chain/pull/654) Improve endblock performance and fix trace

sei-cosmos: https://github.com/sei-protocol/sei-cosmos/compare/v0.2.8...v0.2.12
* improvements around monitoring for sei-cosmos
* dont enforce gas limit on deliverTx
* refactor slashing module


sei-tendermint:
* [#95](https://github.com/sei-protocol/sei-tendermint/pull/95) Patch forging empty merkle tree attack vector
* set default max gas param to 6mil
* log tunning for p2p

## 2.0.40beta - 2023-03-10
* [#646](https://github.com/sei-protocol/sei-chain/pull/646) Optimizations for FinalizeBlock
* [#644](https://github.com/sei-protocol/sei-chain/pull/644) [Oak Audit] Add check for non-existent transaction
* [#647](https://github.com/sei-protocol/sei-chain/pull/647) Fixes to race conditions
* [#638](https://github.com/sei-protocol/sei-chain/pull/638) Emit Version Related Metrics
* [#636](https://github.com/sei-protocol/sei-chain/pull/636) Fix deadlock with upgrades
* [#635](https://github.com/sei-protocol/sei-chain/pull/635) Add event to dex messages

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
