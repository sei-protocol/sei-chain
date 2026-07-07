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
## Unreleased
sei-chain
* [#3716](https://github.com/sei-protocol/sei-chain/pull/3716) fix(distribution): prevent chain halt in `AfterValidatorRemoved` when a validator's commission recipient cannot receive funds (route commission to the community pool)

## v6.6
sei-chain
* [#3679](https://github.com/sei-protocol/sei-chain/pull/3679) Backport `release/v6.6`: State Store: Compact pruned key range after each prune
* [#3673](https://github.com/sei-protocol/sei-chain/pull/3673) Backport `release/v6.6`: fix(metrics): Prometheus metrics output
* [#3672](https://github.com/sei-protocol/sei-chain/pull/3672) Backport `release/v6.6`: [codex] Harden multiversion iterator validation
* [#3669](https://github.com/sei-protocol/sei-chain/pull/3669) Backport `release/v6.6`: Require absolute path for evmone lib
* [#3662](https://github.com/sei-protocol/sei-chain/pull/3662) Backport `release/v6.6`: [codex] bump go-ethereum to v1.15.7-sei-17
* [#3661](https://github.com/sei-protocol/sei-chain/pull/3661) Update checkout GHA step across all workflows
* [#3635](https://github.com/sei-protocol/sei-chain/pull/3635) Backport `release/v6.6`: Generate v6.6 CHANGELOG
* [#3627](https://github.com/sei-protocol/sei-chain/pull/3627) Backport `release/v6.6`: Co-broadcast rich block CW20 transfer with EVM batch
* [#3625](https://github.com/sei-protocol/sei-chain/pull/3625) Generate v6.6 pre-compiles in prep to freeze feature releases
* [#3624](https://github.com/sei-protocol/sei-chain/pull/3624) Load evmone from a trusted absolute path and verify its SHA-256
* [#3622](https://github.com/sei-protocol/sei-chain/pull/3622) ci: add retry/backoff to GHCR docker pull in integration-test workflow (PLT-753)
* [#3618](https://github.com/sei-protocol/sei-chain/pull/3618) [codex] remove sei debug trace exclude endpoints
* [#3617](https://github.com/sei-protocol/sei-chain/pull/3617) Upgrade docker actions no longer supported by ubuntu latest
* [#3612](https://github.com/sei-protocol/sei-chain/pull/3612) Tests: migrate remaining YAML integration tests to Go runner (PLT-738)
* [#3611](https://github.com/sei-protocol/sei-chain/pull/3611) tool: add full-scan EVM logical digest
* [#3610](https://github.com/sei-protocol/sei-chain/pull/3610) Tests: Rpc Tests Phase 3
* [#3609](https://github.com/sei-protocol/sei-chain/pull/3609) additional wireguard bounds for autobahn
* [#3608](https://github.com/sei-protocol/sei-chain/pull/3608) [PLT-700] Add MaxTxSearchResults cap to tx_search and block_search
* [#3607](https://github.com/sei-protocol/sei-chain/pull/3607) fix(sei-tendermint): add ReadHeaderTimeout to CometBFT RPC HTTP server (PLT-440)
* [#3606](https://github.com/sei-protocol/sei-chain/pull/3606) feat(wireguard): per-instance MaxCount + extend to Autobahn fields
* [#3604](https://github.com/sei-protocol/sei-chain/pull/3604) move types out of internal package
* [#3601](https://github.com/sei-protocol/sei-chain/pull/3601) conditional deactivation of consensus timeout overrides in config
* [#3599](https://github.com/sei-protocol/sei-chain/pull/3599) Deflake FlatKV EVM migrate halt coordination
* [#3598](https://github.com/sei-protocol/sei-chain/pull/3598) fix(evmrpc): pass raw tx bytes to DeliverTx in BlockByNumber TraceRunnable (CON-326)
* [#3596](https://github.com/sei-protocol/sei-chain/pull/3596) Tests: evm rpc tests phase 2
* [#3594](https://github.com/sei-protocol/sei-chain/pull/3594) Performance Optimize Litt Keymap Pebble Options
* [#3593](https://github.com/sei-protocol/sei-chain/pull/3593) LittDB iteration
* [#3592](https://github.com/sei-protocol/sei-chain/pull/3592) Stabilize FlatKV EVM migration stop point
* [#3591](https://github.com/sei-protocol/sei-chain/pull/3591) Propogate on chain consensus params to tendermint params
* [#3590](https://github.com/sei-protocol/sei-chain/pull/3590) Move all existing CLAUDE agent files to AGENTS
* [#3589](https://github.com/sei-protocol/sei-chain/pull/3589) Unify root agent files into `AGENTS.md`
* [#3588](https://github.com/sei-protocol/sei-chain/pull/3588) Tests/evm rpc split tests pr:1
* [#3587](https://github.com/sei-protocol/sei-chain/pull/3587) Make FlatKV total-loss recovery stop deterministic
* [#3586](https://github.com/sei-protocol/sei-chain/pull/3586) Revise the repository security policy
* [#3585](https://github.com/sei-protocol/sei-chain/pull/3585) Remove very out of date load test scripts
* [#3584](https://github.com/sei-protocol/sei-chain/pull/3584) Wait for address association before balance assertions
* [#3582](https://github.com/sei-protocol/sei-chain/pull/3582) ci: fix flaky integration tests by distributing images via GHCR
* [#3580](https://github.com/sei-protocol/sei-chain/pull/3580) Remove unused parquet and duckdb receipt store
* [#3577](https://github.com/sei-protocol/sei-chain/pull/3577) feat(flatkv): make zero storage pruning during flatkv migration
* [#3576](https://github.com/sei-protocol/sei-chain/pull/3576) Composite Store unit tests
* [#3575](https://github.com/sei-protocol/sei-chain/pull/3575) weighted committee for autobahn
* [#3574](https://github.com/sei-protocol/sei-chain/pull/3574) made blocksync send StatusRequest after statesync completes (CON-350)
* [#3573](https://github.com/sei-protocol/sei-chain/pull/3573) fix(giga): correct synchronous executor mode
* [#3572](https://github.com/sei-protocol/sei-chain/pull/3572) LittDB Garbage Collection Fine Control
* [#3571](https://github.com/sei-protocol/sei-chain/pull/3571) Update changelog with latest 6.5 chnages
* [#3570](https://github.com/sei-protocol/sei-chain/pull/3570) ci: split slow integration test jobs to reduce CI wall-clock by ~17%
* [#3567](https://github.com/sei-protocol/sei-chain/pull/3567) mempool evm metadata cache (CON-342)
* [#3565](https://github.com/sei-protocol/sei-chain/pull/3565) refactor(consensus-policy): idiomatic fmt.Errorf %w wrapping (follow-up to #3429)
* [#3560](https://github.com/sei-protocol/sei-chain/pull/3560) fix(giga): robust handling of unsupported iterators
* [#3558](https://github.com/sei-protocol/sei-chain/pull/3558) fix(sei-tendermint): default HTTP write timeout to 30s on CometBFT RPC server (SEI-10199)
* [#3557](https://github.com/sei-protocol/sei-chain/pull/3557) ci: pin codecov-action to v7 (fix repo-wide Codecov GPG failure)
* [#3556](https://github.com/sei-protocol/sei-chain/pull/3556) fix(evmrpc): bound and validate storageKeys in eth_getProof
* [#3554](https://github.com/sei-protocol/sei-chain/pull/3554) fix(metrics): OTel exporter namespace sei-chain -> sei_chain so sei_chain_* metrics are queryable
* [#3549](https://github.com/sei-protocol/sei-chain/pull/3549) clean up littDB config
* [#3548](https://github.com/sei-protocol/sei-chain/pull/3548) sharding rpc fixes (CON-320)
* [#3547](https://github.com/sei-protocol/sei-chain/pull/3547) mempool cache fixes (CON-324)
* [#3546](https://github.com/sei-protocol/sei-chain/pull/3546) UnconfirmedTxs improvements (CON-322)
* [#3545](https://github.com/sei-protocol/sei-chain/pull/3545) fix(flatkv): implement flatkv_only mode state-sync int testings
* [#3544](https://github.com/sei-protocol/sei-chain/pull/3544) composite store iteration
* [#3543](https://github.com/sei-protocol/sei-chain/pull/3543) feat(ibc): migrate sei-ibc-go metrics to OpenTelemetry with dual emission
* [#3542](https://github.com/sei-protocol/sei-chain/pull/3542) Fixed parsing of autobahn proposal (CON-325)
* [#3541](https://github.com/sei-protocol/sei-chain/pull/3541) autobahn parsing fixes (CON-311)
* [#3540](https://github.com/sei-protocol/sei-chain/pull/3540) replaced monotoneMaxPeerHeight with maxPeerHeight (CON-328)
* [#3539](https://github.com/sei-protocol/sei-chain/pull/3539) ci: use Docker Buildx + ECR registry cache for integration-test prepare-cluster
* [#3537](https://github.com/sei-protocol/sei-chain/pull/3537) feat(integration): Go-native runner for YAML integration tests (POC)
* [#3536](https://github.com/sei-protocol/sei-chain/pull/3536) revert giga snapshot journal rollback changes
* [#3532](https://github.com/sei-protocol/sei-chain/pull/3532) feat(sei-cosmos): add string limits
* [#3531](https://github.com/sei-protocol/sei-chain/pull/3531) fix flaky LittDB test
* [#3530](https://github.com/sei-protocol/sei-chain/pull/3530) evmrpc: return null on above-watermark for spec-compliant endpoints
* [#3529](https://github.com/sei-protocol/sei-chain/pull/3529) [codex] evmrpc: guard hash trace lookups under timeout
* [#3528](https://github.com/sei-protocol/sei-chain/pull/3528) reject empty SetCode auth list
* [#3527](https://github.com/sei-protocol/sei-chain/pull/3527) fix(scripts): preserve node_key.json across state sync (PLT-415)
* [#3526](https://github.com/sei-protocol/sei-chain/pull/3526) test(evm): cap child_process.exec in lib.js to surface stalled commands
* [#3524](https://github.com/sei-protocol/sei-chain/pull/3524) fix(tokenfactory): add pagination to DenomsFromCreator query (PLT-410)
* [#3523](https://github.com/sei-protocol/sei-chain/pull/3523) flatkv iteration
* [#3522](https://github.com/sei-protocol/sei-chain/pull/3522) autobahn mempool
* [#3521](https://github.com/sei-protocol/sei-chain/pull/3521) feat(giga): use giga executor with OCC by default
* [#3519](https://github.com/sei-protocol/sei-chain/pull/3519) feat(mempool): observability for the TxMempool rewrite invariants
* [#3518](https://github.com/sei-protocol/sei-chain/pull/3518) fix: memiavl-nil-deref-during-state-sync
* [#3517](https://github.com/sei-protocol/sei-chain/pull/3517) fix(storev2): hold read lock in GetCommitKVStore during concurrent map access
* [#3516](https://github.com/sei-protocol/sei-chain/pull/3516) test(flatkv): wait for catchup before partial-loss digest compare
* [#3515](https://github.com/sei-protocol/sei-chain/pull/3515) evmrpc: gate historical debug trace calls
* [#3514](https://github.com/sei-protocol/sei-chain/pull/3514) test(evm): wrap TransientStorageTest contracts with nonce-retry signer
* [#3513](https://github.com/sei-protocol/sei-chain/pull/3513) Iterator utilities and simplfications
* [#3512](https://github.com/sei-protocol/sei-chain/pull/3512) feat(metrics): migrate sei-cosmos module packages to OpenTelemetry (PLT-414)
* [#3511](https://github.com/sei-protocol/sei-chain/pull/3511) split blocksync reactor into 2 modes.
* [#3510](https://github.com/sei-protocol/sei-chain/pull/3510) Remove crisis module wiring and code
* [#3507](https://github.com/sei-protocol/sei-chain/pull/3507) feat(ratelimiter): add RateLimiterRegistry + evmrpc config fields
* [#3506](https://github.com/sei-protocol/sei-chain/pull/3506) Hash Vault Primitive
* [#3505](https://github.com/sei-protocol/sei-chain/pull/3505) feat(ci): nightly ECR publish for harbor nightly job freshness
* [#3504](https://github.com/sei-protocol/sei-chain/pull/3504) ci(PLT-368): build Docker images and seid once, reuse across integration test matrix
* [#3501](https://github.com/sei-protocol/sei-chain/pull/3501) evmrpc: return null from eth_getTransactionReceipt for blocks above safe latest
* [#3497](https://github.com/sei-protocol/sei-chain/pull/3497) Migrate Keeper tests to simple testing with improved coverage
* [#3494](https://github.com/sei-protocol/sei-chain/pull/3494) fix(sei-cosmos): harden paginated RPC queries against DoS via limit, offset, and count_total caps (PLT-361)
* [#3491](https://github.com/sei-protocol/sei-chain/pull/3491) LittDB: secondary keys
* [#3490](https://github.com/sei-protocol/sei-chain/pull/3490) test: migrate Gov & Oracle & Authz yamls off -b block, enable on Autobahn (CON-256)
* [#3489](https://github.com/sei-protocol/sei-chain/pull/3489) test(evm): retry transient query failures in findProposalByTitle (CON-256)
* [#3487](https://github.com/sei-protocol/sei-chain/pull/3487) add new storage test panel
* [#3486](https://github.com/sei-protocol/sei-chain/pull/3486) test(evm): migrate evm_rpc_tests.sh off -b block (CON-256)
* [#3485](https://github.com/sei-protocol/sei-chain/pull/3485) Deflake sequential proxy swap GIGA test
* [#3483](https://github.com/sei-protocol/sei-chain/pull/3483) autobahn: feed persistent_state_dir into data WAL (CON-256)
* [#3482](https://github.com/sei-protocol/sei-chain/pull/3482) test(evm): retry signer.sendTransaction on incorrect-account-sequence (CON-256)
* [#3480](https://github.com/sei-protocol/sei-chain/pull/3480) test: migrate Mint & Staking & Bank tests off -b block, enable on Autobahn (CON-256)
* [#3479](https://github.com/sei-protocol/sei-chain/pull/3479) metrics(app): add app_block_gas_wanted and app_block_gas_wanted_ratio histograms (PLT-354)
* [#3477](https://github.com/sei-protocol/sei-chain/pull/3477) evmrpc: remove blockRes from EncodeTmBlock (CON-256)
* [#3476](https://github.com/sei-protocol/sei-chain/pull/3476) TxMempool rewrite (CON-305)
* [#3473](https://github.com/sei-protocol/sei-chain/pull/3473) testing(flatkv): evm migration int testings
* [#3472](https://github.com/sei-protocol/sei-chain/pull/3472) test(evm): handle transient unsafe-block error in receipt polling (CON-256)
* [#3471](https://github.com/sei-protocol/sei-chain/pull/3471) ci: add Autobahn EVM Interoperability (CON-256)
* [#3470](https://github.com/sei-protocol/sei-chain/pull/3470) Fix pre-v6.5 debug trace tx decoding
* [#3469](https://github.com/sei-protocol/sei-chain/pull/3469) test: enable Autobahn Upgrade and SeiDB CI, migrate cosmos bash off -b block (CON-256)
* [#3468](https://github.com/sei-protocol/sei-chain/pull/3468) ci: add Autobahn EVM Module; drop sei_getEvmTx test (CON-256)
* [#3467](https://github.com/sei-protocol/sei-chain/pull/3467) feat(metrics): migrate sei-cosmos to OpenTelemetry (PLT-353)
* [#3466](https://github.com/sei-protocol/sei-chain/pull/3466) STO-533: Convert littdb to use otel
* [#3465](https://github.com/sei-protocol/sei-chain/pull/3465) Stabilize EVM throughput integration test
* [#3464](https://github.com/sei-protocol/sei-chain/pull/3464) Tidy app gas accounting loop and tests
* [#3463](https://github.com/sei-protocol/sei-chain/pull/3463) Harden distribution withdraw address handling
* [#3461](https://github.com/sei-protocol/sei-chain/pull/3461) fix: capture startTime before defer and fix goroutine error propagation in EVM RPC metrics
* [#3460](https://github.com/sei-protocol/sei-chain/pull/3460) Align LightBlockFromProto with its doc comment
* [#3459](https://github.com/sei-protocol/sei-chain/pull/3459) evmrpc: filter ante stubs from *ExcludeTraceFail (CON-296)
* [#3458](https://github.com/sei-protocol/sei-chain/pull/3458) wireguard: protoc extension and cap remaining inbound paths (CON-298)
* [#3457](https://github.com/sei-protocol/sei-chain/pull/3457) Stabilize FlatKV crash recovery CI test
* [#3455](https://github.com/sei-protocol/sei-chain/pull/3455) Fix lint issues and clean up simulation gas accounting
* [#3453](https://github.com/sei-protocol/sei-chain/pull/3453) ci: add Autobahn EVM GIGA Module integration test (CON-256)
* [#3452](https://github.com/sei-protocol/sei-chain/pull/3452) evmrpc: remove /block_results dependencies from block trace, sei_getCosmosTx, getHeader (CON-256)
* [#3450](https://github.com/sei-protocol/sei-chain/pull/3450) evmrpc: include reverts in *ExcludeTraceFail discriminator (CON-296)
* [#3447](https://github.com/sei-protocol/sei-chain/pull/3447) fix(flatkv): correct catchup metrics
* [#3446](https://github.com/sei-protocol/sei-chain/pull/3446) feat(metrics): migrate app/legacyabci, loadtest, utils/logging, wasmbinding to OpenTelemetry
* [#3443](https://github.com/sei-protocol/sei-chain/pull/3443) feat(giga): don't fail if evmone can't load
* [#3441](https://github.com/sei-protocol/sei-chain/pull/3441) evmrpc: stop caching panic-tx decision for missing receipts (CON-296)
* [#3440](https://github.com/sei-protocol/sei-chain/pull/3440) blocksync: fix sendError in block sync (CON-276)
* [#3439](https://github.com/sei-protocol/sei-chain/pull/3439) feat(metrics): migrate x/oracle, x/epoch, x/mint to OpenTelemetry
* [#3438](https://github.com/sei-protocol/sei-chain/pull/3438) rpc sharding by tx sender for autobahn
* [#3436](https://github.com/sei-protocol/sei-chain/pull/3436) feat(docker): add Prometheus + Grafana monitoring compose overlay
* [#3435](https://github.com/sei-protocol/sei-chain/pull/3435) Cjl/litt integration
* [#3433](https://github.com/sei-protocol/sei-chain/pull/3433) feat(test): add receipt parity + EIP-1559 checks
* [#3432](https://github.com/sei-protocol/sei-chain/pull/3432) blocksync: pre-decode CommitSig caps on inbound Block (CON-277)
* [#3431](https://github.com/sei-protocol/sei-chain/pull/3431) test(evm): wipe stale .openzeppelin manifest before upgradeability test
* [#3430](https://github.com/sei-protocol/sei-chain/pull/3430) add passthrough router
* [#3429](https://github.com/sei-protocol/sei-chain/pull/3429) feat(consensus-policy): mock_chain_validation build tag — HandleError + replay-path fix (PLT-338) + CI image
* [#3428](https://github.com/sei-protocol/sei-chain/pull/3428) ci: add Autobahn RPC .io/.iox spec fixtures to integration tests (CON-256)
* [#3426](https://github.com/sei-protocol/sei-chain/pull/3426) Fix seid static-link paths for missing .a files
* [#3423](https://github.com/sei-protocol/sei-chain/pull/3423) feat(x/evm): migrate x/evm metrics to OpenTelemetry Meter API
* [#3420](https://github.com/sei-protocol/sei-chain/pull/3420) composite.Store uses routers
* [#3419](https://github.com/sei-protocol/sei-chain/pull/3419) Wire eth_subscribe(newHeads) for Autobahn (CON-257)
* [#3417](https://github.com/sei-protocol/sei-chain/pull/3417) test(flatkv): Expands docker-level FlatKV coverage
* [#3415](https://github.com/sei-protocol/sei-chain/pull/3415) Wait for blocksync goroutines on Stop to fix leveldb shutdown panic
* [#3414](https://github.com/sei-protocol/sei-chain/pull/3414) Fix xevm snapshot journal reverts
* [#3411](https://github.com/sei-protocol/sei-chain/pull/3411) chore(giga): remove mentions of experimental and evmone
* [#3410](https://github.com/sei-protocol/sei-chain/pull/3410) Removed callbacks from TxMempool
* [#3407](https://github.com/sei-protocol/sei-chain/pull/3407) migration code cleanup
* [#3406](https://github.com/sei-protocol/sei-chain/pull/3406) test(evm): migrate response-reading lib.js helpers off -b block (CON-256)
* [#3405](https://github.com/sei-protocol/sei-chain/pull/3405) Optimize conflict-heavy EVM transfer execution
* [#3404](https://github.com/sei-protocol/sei-chain/pull/3404) Add EVM stress workload tooling
* [#3403](https://github.com/sei-protocol/sei-chain/pull/3403) make: pass AUTOBAHN/GIGA_STORAGE/etc through giga-mixed and parquet targets (CON-256)
* [#3402](https://github.com/sei-protocol/sei-chain/pull/3402) evmrpc: derive *ExcludeTraceFail from receipt store, not block trace (CON-257)
* [#3401](https://github.com/sei-protocol/sei-chain/pull/3401) feat: gate block-validation bypasses behind //go:build mock_block_validation
* [#3398](https://github.com/sei-protocol/sei-chain/pull/3398) Constants for migration code
* [#3397](https://github.com/sei-protocol/sei-chain/pull/3397) Add replacement APIs for methods we intend to deprecate.
* [#3396](https://github.com/sei-protocol/sei-chain/pull/3396) feat(app): migrate app and ante telemetry to OpenTelemetry Meter API
* [#3395](https://github.com/sei-protocol/sei-chain/pull/3395) litt sharding simple sharding
* [#3394](https://github.com/sei-protocol/sei-chain/pull/3394) test(evm): make 'insufficient gas' test actually reject on autobahn (CON-256)
* [#3391](https://github.com/sei-protocol/sei-chain/pull/3391) Update v6.4 changelog in prep to cut a release
* [#3388](https://github.com/sei-protocol/sei-chain/pull/3388) Regenerate v6.5 changelog in prep to cut rc1
* [#3386](https://github.com/sei-protocol/sei-chain/pull/3386) reverted TxMempool.CheckTx to previous behavior (CON-266)
* [#3384](https://github.com/sei-protocol/sei-chain/pull/3384) giga: report EIP-1559 effective gas price on receipt (CON-256)
* [#3383](https://github.com/sei-protocol/sei-chain/pull/3383) fix(giga): write receipt for state-transition errors that bump the nonce (CON-256)
* [#3382](https://github.com/sei-protocol/sei-chain/pull/3382) implement CommitKVStore using a migration router
* [#3381](https://github.com/sei-protocol/sei-chain/pull/3381) framework for migration tests
* [#3380](https://github.com/sei-protocol/sei-chain/pull/3380) perf(ProcessProposal): skip IsTxGasless in checkTotalBlockGas for non-gasless txs
* [#3377](https://github.com/sei-protocol/sei-chain/pull/3377) Update v6.5 changelog in prep to release rc1
* [#3376](https://github.com/sei-protocol/sei-chain/pull/3376) Add OCC conflict and block execution logs
* [#3374](https://github.com/sei-protocol/sei-chain/pull/3374) test(evm): bound eth_getLogs range in 'Should fetch past logs' (CON-257)
* [#3373](https://github.com/sei-protocol/sei-chain/pull/3373) bank: sdk.Int object pool to reduce allocations in balance reads
* [#3372](https://github.com/sei-protocol/sei-chain/pull/3372) mempool: pass recheck=false from Autobahn block-finalize (CON-256)
* [#3366](https://github.com/sei-protocol/sei-chain/pull/3366) metrics(flatkv): add FlatKV observability metrics and logs
* [#3363](https://github.com/sei-protocol/sei-chain/pull/3363) test(evm): migrate lib.js helpers to -b sync, wait for tx inclusion (CON-256)
* [#3362](https://github.com/sei-protocol/sei-chain/pull/3362) test(evm): drop waitForBaseFeeToBeGt in trace balance diff test (CON-256)
* [#3361](https://github.com/sei-protocol/sei-chain/pull/3361) LittDB: add pebbledb keymap, remove leveldb keymap
* [#3360](https://github.com/sei-protocol/sei-chain/pull/3360) TraceDB: Snapshot-backed state for the trace baker
* [#3359](https://github.com/sei-protocol/sei-chain/pull/3359) TraceDB: Add async debug_trace caching
* [#3358](https://github.com/sei-protocol/sei-chain/pull/3358) implement thread safe router
* [#3357](https://github.com/sei-protocol/sei-chain/pull/3357) README for migration workflows
* [#3355](https://github.com/sei-protocol/sei-chain/pull/3355) Backport v6.4.3 changelog to main
* [#3353](https://github.com/sei-protocol/sei-chain/pull/3353) Use exact v6.5 upgrade name
* [#3352](https://github.com/sei-protocol/sei-chain/pull/3352) config(state-sync): hide tendermint tuning knobs from template
* [#3351](https://github.com/sei-protocol/sei-chain/pull/3351) evmrpc: reject batch elements with object/array JSON-RPC IDs (PLT-319)
* [#3350](https://github.com/sei-protocol/sei-chain/pull/3350) evmrpc: use BlockID.Hash in GetTransaction (CON-257)
* [#3349](https://github.com/sei-protocol/sei-chain/pull/3349) rpc: emit non-nil LastCommit on Autobahn-routed blocks (CON-256)
* [#3347](https://github.com/sei-protocol/sei-chain/pull/3347) CheckTx cleanup
* [#3346](https://github.com/sei-protocol/sei-chain/pull/3346) set checktx flag on ctx
* [#3344](https://github.com/sei-protocol/sei-chain/pull/3344) fix(evmrpc): return null for out-of-range tx index and fix deferred error capture
* [#3341](https://github.com/sei-protocol/sei-chain/pull/3341) Producer MaxGas from genesis (autobahn); ConsensusParams on RPC ctx (CON-257)
* [#3337](https://github.com/sei-protocol/sei-chain/pull/3337) fix(seidb-tool): addresses two issues against the FlatKV seidb tooling
* [#3336](https://github.com/sei-protocol/sei-chain/pull/3336) migration routers
* [#3335](https://github.com/sei-protocol/sei-chain/pull/3335) Cjl/expanded route api
* [#3334](https://github.com/sei-protocol/sei-chain/pull/3334) made all Application.CheckTx calls recover panics (CON-259)
* [#3332](https://github.com/sei-protocol/sei-chain/pull/3332) fix(evmrpc): route receipt lookups through block cache for consistency
* [#3331](https://github.com/sei-protocol/sei-chain/pull/3331) fix(evmrpc): use synthetic IDs in slow-path batch to handle duplicate and null request IDs
* [#3330](https://github.com/sei-protocol/sei-chain/pull/3330) Wait for new-round events before injecting test proposals
* [#3329](https://github.com/sei-protocol/sei-chain/pull/3329) fixed ibc.lightclients.solomachine.v2.ClientState decoding (CON-259)
* [#3328](https://github.com/sei-protocol/sei-chain/pull/3328) fixed peerId check in blocksync (CON-248)
* [#3327](https://github.com/sei-protocol/sei-chain/pull/3327) testfix: delayed WAL closing
* [#3326](https://github.com/sei-protocol/sei-chain/pull/3326) replace eigen logger
* [#3322](https://github.com/sei-protocol/sei-chain/pull/3322) avoid early bail out in migration manager
* [#3321](https://github.com/sei-protocol/sei-chain/pull/3321) fix(evmrpc): include projected child base fee in eth_feeHistory response
* [#3320](https://github.com/sei-protocol/sei-chain/pull/3320) fix(evmrpc): return null for out-of-range index in eth_getTransactionByBlock*AndIndex
* [#3319](https://github.com/sei-protocol/sei-chain/pull/3319) Decommissioned stateful leader election
* [#3318](https://github.com/sei-protocol/sei-chain/pull/3318) StatelessLeaderElection defaults to true (port of #3291)
* [#3314](https://github.com/sei-protocol/sei-chain/pull/3314) optimizations of Tx Hash handling
* [#3313](https://github.com/sei-protocol/sei-chain/pull/3313) fix: flock to consensus WAL was not unlocked when shutting down the node
* [#3312](https://github.com/sei-protocol/sei-chain/pull/3312) feat(flatkv): add seidb tooling for  flatkv dump state and state analysis
* [#3310](https://github.com/sei-protocol/sei-chain/pull/3310) rpc: route /block + /block_results + /block_by_hash + /validators through Autobahn under GigaEnabled (CON-257)
* [#3309](https://github.com/sei-protocol/sei-chain/pull/3309) rpc: populate /status under Autobahn
* [#3307](https://github.com/sei-protocol/sei-chain/pull/3307) Cjl/litt db compiling
* [#3306](https://github.com/sei-protocol/sei-chain/pull/3306) create tooling for routing based on module name
* [#3305](https://github.com/sei-protocol/sei-chain/pull/3305) leader election algorithm (port of #3270)
* [#3303](https://github.com/sei-protocol/sei-chain/pull/3303) fix(evmrpc): treat id=null as a valid request, not a JSON-RPC notification
* [#3302](https://github.com/sei-protocol/sei-chain/pull/3302) fix(ci): align ECR mock_balances build-arg name with Dockerfile
* [#3300](https://github.com/sei-protocol/sei-chain/pull/3300) Support autobahn node restart by skipping CometBFT handshaker (CON-252)
* [#3299](https://github.com/sei-protocol/sei-chain/pull/3299) Copy LittDB in raw form
* [#3298](https://github.com/sei-protocol/sei-chain/pull/3298) integration tests: use parquet receiptdb
* [#3296](https://github.com/sei-protocol/sei-chain/pull/3296) remove iterator usage in PointerInfo
* [#3295](https://github.com/sei-protocol/sei-chain/pull/3295) Use journal instead of CacheKV for giga snapshot/rollback
* [#3294](https://github.com/sei-protocol/sei-chain/pull/3294) Parquet disable full range scan
* [#3293](https://github.com/sei-protocol/sei-chain/pull/3293) V6.5 upgrade handler
* [#3292](https://github.com/sei-protocol/sei-chain/pull/3292) fix(evmrpc): return empty array instead of null for eth_getFilterLogs and eth_getFilterChanges
* [#3281](https://github.com/sei-protocol/sei-chain/pull/3281) fixes to mempool handling
* [#3280](https://github.com/sei-protocol/sei-chain/pull/3280) Align parquet file rotation with cache chunk boundaries
* [#3279](https://github.com/sei-protocol/sei-chain/pull/3279) refactor: drop redundant filepath.Clean around filepath.Join
* [#3278](https://github.com/sei-protocol/sei-chain/pull/3278) Update SS write/read modes into single evm-ss-mode
* [#3277](https://github.com/sei-protocol/sei-chain/pull/3277) Export ProposerPriorityHash metric for divergence monitoring
* [#3276](https://github.com/sei-protocol/sei-chain/pull/3276) add block height modified to legacy data
* [#3275](https://github.com/sei-protocol/sei-chain/pull/3275) Utility for migrating from memiavl to flatKV
* [#3274](https://github.com/sei-protocol/sei-chain/pull/3274) evmrpc: refactor getHeader to always require tmBlock, fix CurrentHeader block fetch
* [#3272](https://github.com/sei-protocol/sei-chain/pull/3272) Replace bump-version.sh with pure-Go go:generate tool
* [#3268](https://github.com/sei-protocol/sei-chain/pull/3268) Giga storage integration test
* [#3267](https://github.com/sei-protocol/sei-chain/pull/3267) Add debug_traceTransactionProfile endpoint
* [#3266](https://github.com/sei-protocol/sei-chain/pull/3266) Speed up historical MVCC reads
* [#3265](https://github.com/sei-protocol/sei-chain/pull/3265) feat(evmrpc): migrate RPC telemetry to OpenTelemetry Meter API
* [#3264](https://github.com/sei-protocol/sei-chain/pull/3264) fix: add retry to apt-get update in Docker CI
* [#3262](https://github.com/sei-protocol/sei-chain/pull/3262) test(flatkv): add flatkv integration testings
* [#3261](https://github.com/sei-protocol/sei-chain/pull/3261) Fix of the proto conv testing
* [#3258](https://github.com/sei-protocol/sei-chain/pull/3258) backport CW/wasmd@76eaff4
* [#3257](https://github.com/sei-protocol/sei-chain/pull/3257) perf(app): reuse decoded transactions across ProcessProposalHandler hot path
* [#3256](https://github.com/sei-protocol/sei-chain/pull/3256) Validate block part index matches proof index (CON-20)
* [#3255](https://github.com/sei-protocol/sei-chain/pull/3255) Fix buffer offset in ProposerPriorityHash (CON-200)
* [#3254](https://github.com/sei-protocol/sei-chain/pull/3254) Handle error case in light client divergence detector
* [#3251](https://github.com/sei-protocol/sei-chain/pull/3251) Use cache in EncodeTmBlock for consistency with GetBlockReceipts
* [#3250](https://github.com/sei-protocol/sei-chain/pull/3250) FlatKV refactor for state sync import + export
* [#3249](https://github.com/sei-protocol/sei-chain/pull/3249) strongly typed clist
* [#3247](https://github.com/sei-protocol/sei-chain/pull/3247) Blocksim Benchmark
* [#3246](https://github.com/sei-protocol/sei-chain/pull/3246) fix(evmrpc): omit notifications from legacy JSON-RPC batch responses per spec
* [#3244](https://github.com/sei-protocol/sei-chain/pull/3244) fix: deduplicate block fetch in getTransactionReceipt
* [#3243](https://github.com/sei-protocol/sei-chain/pull/3243) fix: autobahn InitChain, GetValidators, and mempool TTL (CON-249)
* [#3240](https://github.com/sei-protocol/sei-chain/pull/3240) Regenerate changelog in prep to tag v6.4.2
* [#3237](https://github.com/sei-protocol/sei-chain/pull/3237) Fix receipt default retention
* [#3234](https://github.com/sei-protocol/sei-chain/pull/3234) feat: add autobahn integration tests - batch 1 (CON-249)
* [#3232](https://github.com/sei-protocol/sei-chain/pull/3232) added a ProposerAddress check to setProposal CON-250
* [#3230](https://github.com/sei-protocol/sei-chain/pull/3230) Skip signature event building during Cosmos CheckTx/ReCheckTx
* [#3229](https://github.com/sei-protocol/sei-chain/pull/3229) feat(flatkv): introduce module-prefix physical keys across all FlatKV DBs
* [#3227](https://github.com/sei-protocol/sei-chain/pull/3227) Giga store migration guide
* [#3226](https://github.com/sei-protocol/sei-chain/pull/3226) Assert that forked upstream dependencies are absent
* [#3225](https://github.com/sei-protocol/sei-chain/pull/3225) removed unused peer filtering
* [#3224](https://github.com/sei-protocol/sei-chain/pull/3224) Made autobahn producer use TxMempool
* [#3223](https://github.com/sei-protocol/sei-chain/pull/3223) fix reconcile order, pruning spin, and WAL lock retention
* [#3222](https://github.com/sei-protocol/sei-chain/pull/3222) Tx index for Parquet receipt store
* [#3221](https://github.com/sei-protocol/sei-chain/pull/3221) fix: return zero value for Int, Uint, and Dec when unmarshalling empty bytes
* [#3220](https://github.com/sei-protocol/sei-chain/pull/3220) feat: add AUTOBAHN option to local docker cluster (CON-247)
* [#3219](https://github.com/sei-protocol/sei-chain/pull/3219) added missing proto conversion test
* [#3218](https://github.com/sei-protocol/sei-chain/pull/3218) Fix ApplyChangeSet
* [#3216](https://github.com/sei-protocol/sei-chain/pull/3216) Include receipt store earliest height in watermark bounds to prevent silent undercount on pruned blocks
* [#3213](https://github.com/sei-protocol/sei-chain/pull/3213) Update Changelog in prep to cut v6.4.1
* [#3212](https://github.com/sei-protocol/sei-chain/pull/3212) simplified mempool API
* [#3210](https://github.com/sei-protocol/sei-chain/pull/3210) moved eviction from mempool to mempool reactor
* [#3209](https://github.com/sei-protocol/sei-chain/pull/3209) mux stream kind mismatch fix (CON-240)
* [#3208](https://github.com/sei-protocol/sei-chain/pull/3208) perf(evmrpc): eliminate redundant block fetches in simulate backend
* [#3207](https://github.com/sei-protocol/sei-chain/pull/3207) fix(sei-tendermint): resolve staticcheck warnings
* [#3206](https://github.com/sei-protocol/sei-chain/pull/3206) proto conversion defensive tests
* [#3205](https://github.com/sei-protocol/sei-chain/pull/3205) fix: don't report block-level gas for failed tx decodes (CON-244)
* [#3204](https://github.com/sei-protocol/sei-chain/pull/3204) tendermint mempool cleanup
* [#3203](https://github.com/sei-protocol/sei-chain/pull/3203) fix(staticcheck): resolve warnings in sei-db and sei-ibc-go
* [#3200](https://github.com/sei-protocol/sei-chain/pull/3200) feat(mempool): only allow one retry for failed txs
* [#3197](https://github.com/sei-protocol/sei-chain/pull/3197) Use clean context snapshot for ProcessProposal gas validation (CON-173)
* [#3194](https://github.com/sei-protocol/sei-chain/pull/3194) feat: wire autobahn config propagation from top-level to GigaRouter (CON-232)
* [#3192](https://github.com/sei-protocol/sei-chain/pull/3192) Fix changelog generator
* [#3191](https://github.com/sei-protocol/sei-chain/pull/3191) fix(sei-cosmos): resolve remaining staticcheck warnings
* [#3187](https://github.com/sei-protocol/sei-chain/pull/3187) Make profiled block tracing opt-in by default
* [#3185](https://github.com/sei-protocol/sei-chain/pull/3185) test(flatkv): comprehensive test coverage for the FlatKV state-commitment store
* [#3184](https://github.com/sei-protocol/sei-chain/pull/3184) Guard LastCommitID with rootmulti read lock
* [#3183](https://github.com/sei-protocol/sei-chain/pull/3183) Add historical state offload stream hook
* [#3182](https://github.com/sei-protocol/sei-chain/pull/3182) made secret unreachable via golang reflection
* [#3181](https://github.com/sei-protocol/sei-chain/pull/3181) Fix gzip corruption in EVM JSON-RPC responses via seiLegacyHTTPGate
* [#3180](https://github.com/sei-protocol/sei-chain/pull/3180) upgraded App.GetValidator
* [#3179](https://github.com/sei-protocol/sei-chain/pull/3179) Upgraded autobahn Proposal.CreatedAt to a monotone timestamp.
* [#3176](https://github.com/sei-protocol/sei-chain/pull/3176) Fix gzip double-close in EVM JSON-RPC responses
* [#3175](https://github.com/sei-protocol/sei-chain/pull/3175) feat(giga): do not create receipts for invisible txs
* [#3174](https://github.com/sei-protocol/sei-chain/pull/3174) Fix staticcheck warnings in precompiles and sei-cosmos packages
* [#3172](https://github.com/sei-protocol/sei-chain/pull/3172) Skip coverage job on merge_group events
* [#3171](https://github.com/sei-protocol/sei-chain/pull/3171) Fix gzip Content-Length parsing for flushed HTTP RPC responses
* [#3170](https://github.com/sei-protocol/sei-chain/pull/3170) Upgrade dependency to UCI and configure backport team
* [#3167](https://github.com/sei-protocol/sei-chain/pull/3167) Update v6.4 changelog in prep for cutting tag
* [#3165](https://github.com/sei-protocol/sei-chain/pull/3165) Build the Ethereum chain config once per fee check
* [#3164](https://github.com/sei-protocol/sei-chain/pull/3164) FlatKV Value Type Structs
* [#3163](https://github.com/sei-protocol/sei-chain/pull/3163) Add 9 bytes of metadata to FlatKV store
* [#3162](https://github.com/sei-protocol/sei-chain/pull/3162) plt-232 fixed static check in json_test
* [#3160](https://github.com/sei-protocol/sei-chain/pull/3160) fix: sei legacy http batch
* [#3159](https://github.com/sei-protocol/sei-chain/pull/3159) Add warning message for IAVL deprecation
* [#3157](https://github.com/sei-protocol/sei-chain/pull/3157) Change default min valid per window to zero
* [#3155](https://github.com/sei-protocol/sei-chain/pull/3155) Restructure sei data folder for Giga
* [#3154](https://github.com/sei-protocol/sei-chain/pull/3154) plt-228 fixed static check on app and evmrpc package
* [#3153](https://github.com/sei-protocol/sei-chain/pull/3153) Fix upgrade list comparison to respect semver
* [#3151](https://github.com/sei-protocol/sei-chain/pull/3151) App plugged into autobahn
* [#3149](https://github.com/sei-protocol/sei-chain/pull/3149) Remove redundant makefiles, unused code and adjust docs
* [#3147](https://github.com/sei-protocol/sei-chain/pull/3147) Fix minor logging issues
* [#3146](https://github.com/sei-protocol/sei-chain/pull/3146) Deprecate IAVL and fix all dependency and test
* [#3145](https://github.com/sei-protocol/sei-chain/pull/3145) Make cryptosim state store backend configurable + No Op Wrapper + Read Disable Config
* [#3144](https://github.com/sei-protocol/sei-chain/pull/3144) PLT-225 Use in place orbytes for bloom filter
* [#3143](https://github.com/sei-protocol/sei-chain/pull/3143) hit ledger cache first then duckdb
* [#3142](https://github.com/sei-protocol/sei-chain/pull/3142) State Store Wrapper ApplychangesetAsync
* [#3137](https://github.com/sei-protocol/sei-chain/pull/3137) Deflake empty-block in mempool test
* [#3136](https://github.com/sei-protocol/sei-chain/pull/3136) support for starting autobahn from non-zero global block
* [#3135](https://github.com/sei-protocol/sei-chain/pull/3135) test(evmrpc): assert getBlockTransactionCountByHash matches block list
* [#3134](https://github.com/sei-protocol/sei-chain/pull/3134) Unify EVM SS into single DB with store key prefixes
* [#3133](https://github.com/sei-protocol/sei-chain/pull/3133) Eliminate per-iteration allocation in SetEvmOnlyBlockBloom
* [#3132](https://github.com/sei-protocol/sei-chain/pull/3132) Make historical proof test use sync SC commits
* [#3129](https://github.com/sei-protocol/sei-chain/pull/3129) fix to ProposalPOLMessage poisoning (CON-222)
* [#3128](https://github.com/sei-protocol/sei-chain/pull/3128) Fix edge case for non atomic commit
* [#3127](https://github.com/sei-protocol/sei-chain/pull/3127) fix(flatkv): harden error handling for readonly store and crash cleanup
* [#3126](https://github.com/sei-protocol/sei-chain/pull/3126) persist blocks and FullCommitQCs in data layer via WAL (CON-231)
* [#3125](https://github.com/sei-protocol/sei-chain/pull/3125) fix: eth_getBlockTransactionCountByNumber returns inconsistent count vs eth_getBlockByNumber
* [#3124](https://github.com/sei-protocol/sei-chain/pull/3124) fix(wal): make TruncateAll early return work when AllowEmpty is true
* [#3122](https://github.com/sei-protocol/sei-chain/pull/3122) fix: make no-op deleteBefore advance cursor in commitQCState and simplify deleteBefore API
* [#3121](https://github.com/sei-protocol/sei-chain/pull/3121) InitChain cleanup
* [#3120](https://github.com/sei-protocol/sei-chain/pull/3120) Pass versioned changeset entries to wrappers + ApplyChangesetasync
* [#3119](https://github.com/sei-protocol/sei-chain/pull/3119) evm: RPC IO fixtures and blocking CI (targetting 100% pass rate)
* [#3118](https://github.com/sei-protocol/sei-chain/pull/3118) fix(evmrpc): align genesis block handling RPC methods
* [#3117](https://github.com/sei-protocol/sei-chain/pull/3117) Remove stale price snapshot creation from MidBlocker
* [#3116](https://github.com/sei-protocol/sei-chain/pull/3116) fix(evm-tests): align reverter_contract.hex with reverter.sol (solc 0.8.28)
* [#3114](https://github.com/sei-protocol/sei-chain/pull/3114) fixed ValidatorInfo json encoding
* [#3111](https://github.com/sei-protocol/sei-chain/pull/3111) fix: eth_call with eip-1559 call
* [#3110](https://github.com/sei-protocol/sei-chain/pull/3110) Update rpc_bench to support fixed block ranges and file output
* [#3109](https://github.com/sei-protocol/sei-chain/pull/3109) chore: sei legacy RPC deprecation
* [#3107](https://github.com/sei-protocol/sei-chain/pull/3107) Disable zero copy for SC
* [#3105](https://github.com/sei-protocol/sei-chain/pull/3105) persist: productionize A/B file persistence
* [#3103](https://github.com/sei-protocol/sei-chain/pull/3103) feat(giga): fallback to v2 for associations and migrations
* [#3098](https://github.com/sei-protocol/sei-chain/pull/3098) Fix SIGSEGV in historical RPC queries
* [#3097](https://github.com/sei-protocol/sei-chain/pull/3097) Fix last commit hash missmatch during replay
* [#3096](https://github.com/sei-protocol/sei-chain/pull/3096) removed PrepareProposal from Application
* [#3092](https://github.com/sei-protocol/sei-chain/pull/3092) Fix IBC Upgrade Params
* [#3091](https://github.com/sei-protocol/sei-chain/pull/3091) Fix composite snapshot import hang on EVM errors
* [#3090](https://github.com/sei-protocol/sei-chain/pull/3090) fix(flatkv): purge stale data before state sync import
* [#3088](https://github.com/sei-protocol/sei-chain/pull/3088) go version for mockery pinned
* [#3087](https://github.com/sei-protocol/sei-chain/pull/3087) removed proto encoding for most of the abci types
* [#3086](https://github.com/sei-protocol/sei-chain/pull/3086) chore(evmrpc): explicit unsupported rpcs
* [#3085](https://github.com/sei-protocol/sei-chain/pull/3085) cryptosim bugfixes
* [#3084](https://github.com/sei-protocol/sei-chain/pull/3084) feat(flatkv): implements Account Row GC for FlatKV `accountDB`
* [#3083](https://github.com/sei-protocol/sei-chain/pull/3083) Made block processing abci request contain a tendermint block header
* [#3082](https://github.com/sei-protocol/sei-chain/pull/3082) dropped orphaned mock
* [#3081](https://github.com/sei-protocol/sei-chain/pull/3081) Add receipt / log reads to cryptosim
* [#3080](https://github.com/sei-protocol/sei-chain/pull/3080) chore(evmrpc): expose eth_blobBaseFee on eth RPC surface
* [#3077](https://github.com/sei-protocol/sei-chain/pull/3077) Implement a standard cache.
* [#3075](https://github.com/sei-protocol/sei-chain/pull/3075) Fix commit and query contention
* [#3074](https://github.com/sei-protocol/sei-chain/pull/3074) feat(flatkv): adds per-DB LtHash tracking to the FlatKV commit store
* [#3073](https://github.com/sei-protocol/sei-chain/pull/3073) Harden EVM field-bloat validation
* [#3069](https://github.com/sei-protocol/sei-chain/pull/3069) chore (evmrpc): fix eth_getBlockTransactionCountByHash hash lookup consistency
* [#3067](https://github.com/sei-protocol/sei-chain/pull/3067) chore(evmrpc): return null for unknown/empty block hash
* [#3064](https://github.com/sei-protocol/sei-chain/pull/3064) Add exporter for FlatKV and CompositeSC
* [#3058](https://github.com/sei-protocol/sei-chain/pull/3058) Debug Trace Speedup
* [#3054](https://github.com/sei-protocol/sei-chain/pull/3054) Parquet docker integration tests
* [#3047](https://github.com/sei-protocol/sei-chain/pull/3047) add receipt generation to cryptosim
* [#3044](https://github.com/sei-protocol/sei-chain/pull/3044) persist: replace file-per-entry with WAL and refactor into generic indexedWAL
* [#3037](https://github.com/sei-protocol/sei-chain/pull/3037) rotation of outbound peers
* [#3027](https://github.com/sei-protocol/sei-chain/pull/3027) flatkv cache
* [#3004](https://github.com/sei-protocol/sei-chain/pull/3004) EVM RPC read benchmark script for state store load testing
* [#2752](https://github.com/sei-protocol/sei-chain/pull/2752) Fix seid version tagging logic for release branch
* [#2499](https://github.com/sei-protocol/sei-chain/pull/2499) Fix bogus signatures in light client attack evidence

## v6.5
sei-chain
* [#3563](https://github.com/sei-protocol/sei-chain/pull/3563) fix(giga): robust handling of unsupported iterators (#3560)
* [#3500](https://github.com/sei-protocol/sei-chain/pull/3500) Release v6.5.1
* [#3478](https://github.com/sei-protocol/sei-chain/pull/3478) Backport `release/v6.5`: Fix pre-v6.5 debug trace tx decoding
* [#3444](https://github.com/sei-protocol/sei-chain/pull/3444) Cut v6.5.0 in prep for release
* [#3389](https://github.com/sei-protocol/sei-chain/pull/3389) Backport `release/v6.5`: Regenerate v6.5 changelog in prep to cut rc1
* [#3387](https://github.com/sei-protocol/sei-chain/pull/3387) Backport `release/v6.5`: giga: report EIP-1559 effective gas price on receipt (CON-256)
* [#3385](https://github.com/sei-protocol/sei-chain/pull/3385) Bump version in prep to cut `v6.5` RC1
* [#3378](https://github.com/sei-protocol/sei-chain/pull/3378) Backport `release/v6.5`: Update v6.5 changelog in prep to release rc1
* [#3367](https://github.com/sei-protocol/sei-chain/pull/3367) Backport `release/v6.5`: fix(evmrpc): return null for out-of-range index in eth_getTransactionByBlock*AndIndex
* [#3364](https://github.com/sei-protocol/sei-chain/pull/3364) Backport `release/v6.5`: V6.5 upgrade handler
* [#3356](https://github.com/sei-protocol/sei-chain/pull/3356) Backport `release/v6.5`: Use exact v6.5 upgrade name
* [#3340](https://github.com/sei-protocol/sei-chain/pull/3340) Backport `release/v6.5`: Harden EVM field-bloat validation
* [#3338](https://github.com/sei-protocol/sei-chain/pull/3338) Backport `release/v6.5`: Update SS write/read modes into single evm-ss-mode
* [#3308](https://github.com/sei-protocol/sei-chain/pull/3308) Backport `release/v6.5`: remove iterator usage in PointerInfo
* [#3291](https://github.com/sei-protocol/sei-chain/pull/3291) StatelessLeaderElection defaults to true


## v6.4
sei-chain
* [#3390](https://github.com/sei-protocol/sei-chain/pull/3390) Backport `release/v6.4`: giga: report EIP-1559 effective gas price on receipt (CON-256)
* [#3343](https://github.com/sei-protocol/sei-chain/pull/3343) Bump version to v6.4.3 in prep for tag cut
* [#3342](https://github.com/sei-protocol/sei-chain/pull/3342) Update Changelog in prep for release
* [#3333](https://github.com/sei-protocol/sei-chain/pull/3333) Backport `release/v6.4`: fix(evmrpc): use synthetic IDs in slow-path batch to handle duplicate and null request IDs
* [#3324](https://github.com/sei-protocol/sei-chain/pull/3324) Backport `release/v6.4`: fix(evmrpc): return empty array instead of null for eth_getFilterLogs and eth_getFilterChanges
* [#3315](https://github.com/sei-protocol/sei-chain/pull/3315) Backport `release/v6.4`: fix: flock to consensus WAL was not unlocked when shutting down the node
* [#3286](https://github.com/sei-protocol/sei-chain/pull/3286) Backport `release/v6.4`: Export ProposerPriorityHash metric for divergence monitoring
* [#3283](https://github.com/sei-protocol/sei-chain/pull/3283) Backport `release/v6.4`: Fix buffer offset in ProposerPriorityHash (CON-200)
* [#3282](https://github.com/sei-protocol/sei-chain/pull/3282) Backport `release/v6.4`: Handle error case in light client divergence detector
* [#3273](https://github.com/sei-protocol/sei-chain/pull/3273) backported crypto library fixes
* [#3270](https://github.com/sei-protocol/sei-chain/pull/3270) new short term election algorithm
* [#3242](https://github.com/sei-protocol/sei-chain/pull/3242) Bump tag in prep to publish release v6.4.2
* [#3241](https://github.com/sei-protocol/sei-chain/pull/3241) Backport `release/v6.4`: Regenerate changelog in prep to tag v6.4.2
* [#3238](https://github.com/sei-protocol/sei-chain/pull/3238) Backport `release/v6.4`: Fix receipt default retention
* [#3233](https://github.com/sei-protocol/sei-chain/pull/3233) Backport `release/v6.4`: added a ProposerAddress check to setProposal CON-250
* [#3215](https://github.com/sei-protocol/sei-chain/pull/3215) Bump version file to `v6.4.1` in prep to cut tag
* [#3214](https://github.com/sei-protocol/sei-chain/pull/3214) Backport `release/v6.4`: Update Changelog in prep to cut v6.4.1
* [#3211](https://github.com/sei-protocol/sei-chain/pull/3211) Backport `release/v6.4`: Unify EVM SS into single DB with store key prefixes
* [#3201](https://github.com/sei-protocol/sei-chain/pull/3201) Backport `release/v6.4`: feat(mempool): only allow one retry for failed txs
* [#3199](https://github.com/sei-protocol/sei-chain/pull/3199) Add v154 wasmvm library compatibility documentation
* [#3196](https://github.com/sei-protocol/sei-chain/pull/3196) Backport `release/v6.4`: Fix changelog generator
* [#3193](https://github.com/sei-protocol/sei-chain/pull/3193) Backport `release/v6.4`: hit ledger cache first then duckdb
* [#3190](https://github.com/sei-protocol/sei-chain/pull/3190) Backport `release/v6.4`: Debug Trace Speedup
* [#3189](https://github.com/sei-protocol/sei-chain/pull/3189) Backport `release/v6.4`: Make profiled block tracing opt-in by default
* [#3188](https://github.com/sei-protocol/sei-chain/pull/3188) Backport `release/v6.4`: Fix gzip corruption in EVM JSON-RPC responses via seiLegacyHTTPGate
* [#3186](https://github.com/sei-protocol/sei-chain/pull/3186) Backport `release/v6.4`: Guard LastCommitID with rootmulti read lock
* [#3177](https://github.com/sei-protocol/sei-chain/pull/3177) Backport `release/v6.4`: Fix gzip double-close in EVM JSON-RPC responses
* [#3173](https://github.com/sei-protocol/sei-chain/pull/3173) Backport `release/v6.4`: Fix gzip Content-Length parsing for flushed HTTP RPC responses
* [#3169](https://github.com/sei-protocol/sei-chain/pull/3169) Cut `v6.4.0` tag atop head of `release/v6.4` branch
* [#3168](https://github.com/sei-protocol/sei-chain/pull/3168) Backport `release/v6.4`: Update v6.4 changelog in prep for cutting tag
* [#3166](https://github.com/sei-protocol/sei-chain/pull/3166) Backport `release/v6.4`: fix: sei legacy http batch
* [#3161](https://github.com/sei-protocol/sei-chain/pull/3161) Backport `release/v6.4`: Add warning message for IAVL deprecation
* [#3158](https://github.com/sei-protocol/sei-chain/pull/3158) Backport `release/v6.4`: Change default min valid per window to zero
* [#3156](https://github.com/sei-protocol/sei-chain/pull/3156) Backport `release/v6.4`: Fix upgrade list comparison to respect semver
* [#3152](https://github.com/sei-protocol/sei-chain/pull/3152) Backport `release/v6.4`: Remove redundant makefiles, unused code and adjust docs
* [#3148](https://github.com/sei-protocol/sei-chain/pull/3148) Backport `release/v6.4`: Fix minor logging issues
* [#3141](https://github.com/sei-protocol/sei-chain/pull/3141) Backport `release/v6.4`: chore: sei legacy RPC deprecation
* [#3140](https://github.com/sei-protocol/sei-chain/pull/3140) Backport `release/v6.4`: feat(giga): fallback to v2 for associations and migrations
* [#3139](https://github.com/sei-protocol/sei-chain/pull/3139) Backport `release/v6.4`: fix to ProposalPOLMessage poisoning (CON-222)
* [#3131](https://github.com/sei-protocol/sei-chain/pull/3131) Backport `release/v6.4`: Remove stale price snapshot creation from MidBlocker
* [#3115](https://github.com/sei-protocol/sei-chain/pull/3115) Backport `release/v6.4`: fixed ValidatorInfo json encoding
* [#3108](https://github.com/sei-protocol/sei-chain/pull/3108) Backport `release/v6.4`: Disable zero copy for SC
* [#3104](https://github.com/sei-protocol/sei-chain/pull/3104) Backport `release/v6.4`: Fix composite snapshot import hang on EVM errors
* [#3101](https://github.com/sei-protocol/sei-chain/pull/3101) Backport `release/v6.4`: Fix SIGSEGV in historical RPC queries
* [#3100](https://github.com/sei-protocol/sei-chain/pull/3100) Backport `release/v6.4`: Fix last commit hash missmatch during replay
* [#3094](https://github.com/sei-protocol/sei-chain/pull/3094) Backport `release/v6.4`: Fix seid version tagging logic for release branch
* [#3093](https://github.com/sei-protocol/sei-chain/pull/3093) Backport `release/v6.4`: Fix IBC Upgrade Params
* [#3079](https://github.com/sei-protocol/sei-chain/pull/3079) Add release 6.4 precompiles
* [#3076](https://github.com/sei-protocol/sei-chain/pull/3076) Added clone method to canned random
* [#3072](https://github.com/sei-protocol/sei-chain/pull/3072) Helper files for the flatKV cache implementation
* [#3071](https://github.com/sei-protocol/sei-chain/pull/3071) fix(giga): match v2 correctness checks
* [#3070](https://github.com/sei-protocol/sei-chain/pull/3070) fix: restore PRs inadvertently reverted by #3039 squash-merge
* [#3066](https://github.com/sei-protocol/sei-chain/pull/3066) Refine logging to avoid printing expensive objects on hot path
* [#3065](https://github.com/sei-protocol/sei-chain/pull/3065) Fix flaky tendermint syncer test
* [#3063](https://github.com/sei-protocol/sei-chain/pull/3063) Fix flaky test caused by async WAL writes
* [#3062](https://github.com/sei-protocol/sei-chain/pull/3062) Add runtime log level control via gRPC admin service
* [#3059](https://github.com/sei-protocol/sei-chain/pull/3059) Use semver comparator to compare upgrade names
* [#3057](https://github.com/sei-protocol/sei-chain/pull/3057) feat(flatkv): add comprehensive writing test coverage and centralize account-field semantics
* [#3055](https://github.com/sei-protocol/sei-chain/pull/3055) Utility methods for FlatKV cache
* [#3053](https://github.com/sei-protocol/sei-chain/pull/3053) Fix flaky test: TestParquetFilePruning
* [#3052](https://github.com/sei-protocol/sei-chain/pull/3052) Enable CI run for merge queue
* [#3051](https://github.com/sei-protocol/sei-chain/pull/3051) Remove old codeql analysis take 2
* [#3050](https://github.com/sei-protocol/sei-chain/pull/3050) Replace all loggers with package level `slog`
* [#3049](https://github.com/sei-protocol/sei-chain/pull/3049) feat(wal): expose AllowEmpty config and add TruncateAll method
* [#3048](https://github.com/sei-protocol/sei-chain/pull/3048) Remove codeQL from commit accept pipeline.
* [#3046](https://github.com/sei-protocol/sei-chain/pull/3046) Add console logger and fix memiavl config for benchmark
* [#3043](https://github.com/sei-protocol/sei-chain/pull/3043) Add config to enable lattice hash
* [#3041](https://github.com/sei-protocol/sei-chain/pull/3041) chore: dcoument run RPC suite on legacy vs giga
* [#3040](https://github.com/sei-protocol/sei-chain/pull/3040) Use latest fo setup action to reduce flakes
* [#3039](https://github.com/sei-protocol/sei-chain/pull/3039) feat(flatkv): add read-only LoadVersion for state sync
* [#3035](https://github.com/sei-protocol/sei-chain/pull/3035) Add receiptdb config option in app.toml
* [#3033](https://github.com/sei-protocol/sei-chain/pull/3033) chore: self-contained revert tests, contract reorg, and failure analysis
* [#3031](https://github.com/sei-protocol/sei-chain/pull/3031) Source Go toolchain from official image and harden foundry install
* [#3030](https://github.com/sei-protocol/sei-chain/pull/3030) fix: use createdAt parameter instead of time.Now() in NewProposal
* [#3029](https://github.com/sei-protocol/sei-chain/pull/3029) fix: carry PrepareQC lock across consecutive timeouts in voteTimeout
* [#3028](https://github.com/sei-protocol/sei-chain/pull/3028) Parquet crash testing unit testing hooks
* [#3026](https://github.com/sei-protocol/sei-chain/pull/3026) Update trace queue wait by timeout
* [#3024](https://github.com/sei-protocol/sei-chain/pull/3024) Remove dependency to zerolog in favour of slog
* [#3023](https://github.com/sei-protocol/sei-chain/pull/3023) Fix cryptosim metrics, QOL scripting upgrades
* [#3022](https://github.com/sei-protocol/sei-chain/pull/3022) Create parent dirs for RocksDB backend fully
* [#3021](https://github.com/sei-protocol/sei-chain/pull/3021) Background Transaction Generation
* [#3020](https://github.com/sei-protocol/sei-chain/pull/3020) perf(flatkv): parallelize per-DB batch commit
* [#3019](https://github.com/sei-protocol/sei-chain/pull/3019) Show time spent per block in benchmark
* [#3018](https://github.com/sei-protocol/sei-chain/pull/3018) Add more detailed phase metrics to flatKV
* [#3016](https://github.com/sei-protocol/sei-chain/pull/3016) Deflake TestNodeStartStop under CI coverage load
* [#3015](https://github.com/sei-protocol/sei-chain/pull/3015) Remove oracle price-feeder executable
* [#3014](https://github.com/sei-protocol/sei-chain/pull/3014) Remove go.work.sum accidentally added back
* [#3013](https://github.com/sei-protocol/sei-chain/pull/3013) Add cursor context for EVM
* [#3012](https://github.com/sei-protocol/sei-chain/pull/3012) Automatically add dashboard
* [#3011](https://github.com/sei-protocol/sei-chain/pull/3011) fix(flatkv): sync SNAPSHOT_BASE on WriteSnapshot to avoid unnecessary WAL catchup on restart
* [#3010](https://github.com/sei-protocol/sei-chain/pull/3010) Cody littley/db metrics
* [#3009](https://github.com/sei-protocol/sei-chain/pull/3009) Convert cryptosim metrics to otel
* [#3008](https://github.com/sei-protocol/sei-chain/pull/3008) Cryptosim: dormant account support
* [#3007](https://github.com/sei-protocol/sei-chain/pull/3007) Allow cryptosim benchmark to be suspended.
* [#3006](https://github.com/sei-protocol/sei-chain/pull/3006) WAL utility tears itself down after the first error
* [#3005](https://github.com/sei-protocol/sei-chain/pull/3005) fix: make GetCodeHash EVM-compliant
* [#3003](https://github.com/sei-protocol/sei-chain/pull/3003) fix(flatkv): fix state sync panic on nil DB handles during snapshot restore
* [#3002](https://github.com/sei-protocol/sei-chain/pull/3002) Add opts for scdual write
* [#3001](https://github.com/sei-protocol/sei-chain/pull/3001) Fix lock issue in cacheMultiStore
* [#3000](https://github.com/sei-protocol/sei-chain/pull/3000) fix(flatkv): prevent phantom MixOut for new accounts in LtHash
* [#2999](https://github.com/sei-protocol/sei-chain/pull/2999) Deflake async peer registration and harden PeerList.All
* [#2998](https://github.com/sei-protocol/sei-chain/pull/2998) Remove random address collisions in conn tracker tests
* [#2997](https://github.com/sei-protocol/sei-chain/pull/2997) do not create receipts for invisible txs
* [#2995](https://github.com/sei-protocol/sei-chain/pull/2995) Extend DB benchmark to support state store (SS) backends
* [#2994](https://github.com/sei-protocol/sei-chain/pull/2994) feat(giga): implement iterator for the cachekv
* [#2993](https://github.com/sei-protocol/sei-chain/pull/2993) Fix `make lint`
* [#2992](https://github.com/sei-protocol/sei-chain/pull/2992) Pex during handshake
* [#2991](https://github.com/sei-protocol/sei-chain/pull/2991) Deflake upgrade/downgrade tests by making restart deterministic
* [#2990](https://github.com/sei-protocol/sei-chain/pull/2990) Reduced number of addresses per NodeID in peermanager to 1
* [#2989](https://github.com/sei-protocol/sei-chain/pull/2989) Refine condecov config to avoid miss-leading drop on partial coverage
* [#2988](https://github.com/sei-protocol/sei-chain/pull/2988) Fix flaky `TestPeerManager_MaxOutboundConnectionsForDialing`
* [#2987](https://github.com/sei-protocol/sei-chain/pull/2987) Migrate tendermint logging to `slog` and remove go-kit/log dependency
* [#2986](https://github.com/sei-protocol/sei-chain/pull/2986) Cody littley/cryptosim metrics
* [#2985](https://github.com/sei-protocol/sei-chain/pull/2985) feat(evm): EVM RPC .io/.iox integration tests (spec fixtures)
* [#2984](https://github.com/sei-protocol/sei-chain/pull/2984) Refactor StateStore for better readability and Giga support
* [#2983](https://github.com/sei-protocol/sei-chain/pull/2983) Deflake mempool tests with Eventually-based block waits
* [#2982](https://github.com/sei-protocol/sei-chain/pull/2982) Demote noisy gasless classification log to debug level
* [#2981](https://github.com/sei-protocol/sei-chain/pull/2981) Parquet remove last file if corrupted
* [#2980](https://github.com/sei-protocol/sei-chain/pull/2980) Harden `TestStateLock_NoPOL` against proposal/timeout race
* [#2979](https://github.com/sei-protocol/sei-chain/pull/2979) ERC20 simulation benchmark
* [#2978](https://github.com/sei-protocol/sei-chain/pull/2978) feat(flatkv): include legacyDB in ApplyChangeSets, LtHash, and read path
* [#2977](https://github.com/sei-protocol/sei-chain/pull/2977) merged unconditional and persistent peers status
* [#2976](https://github.com/sei-protocol/sei-chain/pull/2976) Fix hanging upgrade tests by adding timeouts to wait_for_height
* [#2975](https://github.com/sei-protocol/sei-chain/pull/2975) Fix race between file pruning and in-flight parquet queries
* [#2974](https://github.com/sei-protocol/sei-chain/pull/2974) added a config parameter to limit outbound p2p connections.
* [#2972](https://github.com/sei-protocol/sei-chain/pull/2972) feat(flatkv): add snapshot, WAL catchup, and rollback support
* [#2971](https://github.com/sei-protocol/sei-chain/pull/2971) Fix Rocksdb MVCC read timestamp lifetime for iterators
* [#2970](https://github.com/sei-protocol/sei-chain/pull/2970) Add snapshot import for Giga Live State
* [#2969](https://github.com/sei-protocol/sei-chain/pull/2969) Deflake `TestStateLock_NoPOL` by widening propose timeout in test
* [#2968](https://github.com/sei-protocol/sei-chain/pull/2968) Reduce exposed tendermint RPC endpoint
* [#2967](https://github.com/sei-protocol/sei-chain/pull/2967) Harden staking precompile test against CI flakiness
* [#2966](https://github.com/sei-protocol/sei-chain/pull/2966) fix: use correct EVM storage key prefix in benchmark key generation
* [#2964](https://github.com/sei-protocol/sei-chain/pull/2964) Don't sync flatKV DBs when committing
* [#2963](https://github.com/sei-protocol/sei-chain/pull/2963) fix(giga): bail on wrong nonce as v2 does
* [#2962](https://github.com/sei-protocol/sei-chain/pull/2962) Fix flaky `TestStateLock_POLSafety1`
* [#2961](https://github.com/sei-protocol/sei-chain/pull/2961) fix(giga): don't migrate balance on failed txs
* [#2959](https://github.com/sei-protocol/sei-chain/pull/2959) feat(app): remove concept of prioritized txs
* [#2958](https://github.com/sei-protocol/sei-chain/pull/2958) Add metrics for historical proof success/failure rate
* [#2957](https://github.com/sei-protocol/sei-chain/pull/2957) [giga] remove Snapshot() call in Prepare()
* [#2954](https://github.com/sei-protocol/sei-chain/pull/2954) Reduce SS changelog retention to use the async buffer size
* [#2951](https://github.com/sei-protocol/sei-chain/pull/2951) Add Rate limit and concurrency control for RPC with proof queries
* [#2946](https://github.com/sei-protocol/sei-chain/pull/2946) Fix flaky `TestClientMethodCalls` by using eventual time assertion
* [#2945](https://github.com/sei-protocol/sei-chain/pull/2945) Fix flaky `TestRPC` JSON-RPC test due to hardcoded port
* [#2943](https://github.com/sei-protocol/sei-chain/pull/2943) Fix `TestTimelyProposal` flakiness
* [#2942](https://github.com/sei-protocol/sei-chain/pull/2942) Deflake test failures due to port already in use
* [#2941](https://github.com/sei-protocol/sei-chain/pull/2941) bugfix: make router load addresses from peerdb
* [#2940](https://github.com/sei-protocol/sei-chain/pull/2940) flaky test fix
* [#2939](https://github.com/sei-protocol/sei-chain/pull/2939) Use go version consistent with go mod in UCI lint
* [#2937](https://github.com/sei-protocol/sei-chain/pull/2937) Simplify CI test coverage and fix flaky tests
* [#2936](https://github.com/sei-protocol/sei-chain/pull/2936) Add FlatKV database to the benchmarking utility.
* [#2935](https://github.com/sei-protocol/sei-chain/pull/2935) replacing abci protobuf types with plain golang types (part 1)
* [#2934](https://github.com/sei-protocol/sei-chain/pull/2934) WAL wrapper improvements
* [#2930](https://github.com/sei-protocol/sei-chain/pull/2930) Use composite SC for RootMultistore
* [#2925](https://github.com/sei-protocol/sei-chain/pull/2925) Verify LaneProposal signer and parent hash chain in PushBlock
* [#2924](https://github.com/sei-protocol/sei-chain/pull/2924) Fix off-by-one in PushBlock that causes nil dereference panic
* [#2922](https://github.com/sei-protocol/sei-chain/pull/2922) Add go bench for WAL
* [#2920](https://github.com/sei-protocol/sei-chain/pull/2920) Removed unused voteInfos from App.
* [#2919](https://github.com/sei-protocol/sei-chain/pull/2919) feat(sei-db): add flatkv store implementation
* [#2918](https://github.com/sei-protocol/sei-chain/pull/2918) Revert "feat(sei-db): add flatkv store implementation (#2793)"
* [#2916](https://github.com/sei-protocol/sei-chain/pull/2916) chore: delete outdated run node.py script
* [#2915](https://github.com/sei-protocol/sei-chain/pull/2915) chore: update integration test doc to the correct paths
* [#2913](https://github.com/sei-protocol/sei-chain/pull/2913) chore: add init mode full default biding address
* [#2911](https://github.com/sei-protocol/sei-chain/pull/2911) Return proper error message when SS disabled
* [#2910](https://github.com/sei-protocol/sei-chain/pull/2910) fix: harden PushQC against stale QCs and unverified headers
* [#2909](https://github.com/sei-protocol/sei-chain/pull/2909) removed tendermint binary and abciclient.
* [#2908](https://github.com/sei-protocol/sei-chain/pull/2908) fix(consensus): harden FullProposal.Verify and NewProposal against malicious proposals
* [#2907](https://github.com/sei-protocol/sei-chain/pull/2907) Add --overwrite to benchmark script to satisfy new check
* [#2903](https://github.com/sei-protocol/sei-chain/pull/2903) fix: config issue since v6.3.0 upgrade
* [#2899](https://github.com/sei-protocol/sei-chain/pull/2899) perf: cache block-level constants in `executeEVMTxWithGigaExecutor`
* [#2896](https://github.com/sei-protocol/sei-chain/pull/2896) consensus: persist AppQC, blocks, and CommitQCs with async persistence
* [#2893](https://github.com/sei-protocol/sei-chain/pull/2893) Port sei-v3 PR #510: crash-safe consensus state persistence
* [#2891](https://github.com/sei-protocol/sei-chain/pull/2891) fix(ledger): upgrade ledger-cosmos-go to v1.0.0 for Cosmos app v2.34+ compatibility
* [#2889](https://github.com/sei-protocol/sei-chain/pull/2889) fix(giga): three fixes for v2-matching correctness
* [#2887](https://github.com/sei-protocol/sei-chain/pull/2887) fixed autorestart cooldown
* [#2885](https://github.com/sei-protocol/sei-chain/pull/2885) refactor: move benchmarks to benchmark/ and deduplicate init logic
* [#2883](https://github.com/sei-protocol/sei-chain/pull/2883) Fix inconsistent config for self remediation behind interval
* [#2880](https://github.com/sei-protocol/sei-chain/pull/2880) IBC Update
* [#2879](https://github.com/sei-protocol/sei-chain/pull/2879) Add more metrics for snapshot and state sync
* [#2878](https://github.com/sei-protocol/sei-chain/pull/2878) chore: guard config override in init with existing overwrite flag
* [#2876](https://github.com/sei-protocol/sei-chain/pull/2876) perf(cachemulti): lazy-init cachekv stores on first access
* [#2875](https://github.com/sei-protocol/sei-chain/pull/2875) chore: remove wasm dir on unsafe-reset
* [#2874](https://github.com/sei-protocol/sei-chain/pull/2874) Made the consensus reactor rebroadcast NewValidBlockMessage
* [#2873](https://github.com/sei-protocol/sei-chain/pull/2873) fix to halt due to reconstructing block from bad proposal (backported #2823)
* [#2872](https://github.com/sei-protocol/sei-chain/pull/2872) made the peer dialing less aggressive (backported #2799)
* [#2870](https://github.com/sei-protocol/sei-chain/pull/2870) Flatten `sei-cosmos` go module into `sei-chain`
* [#2868](https://github.com/sei-protocol/sei-chain/pull/2868) fix: respect existing genesis file
* [#2866](https://github.com/sei-protocol/sei-chain/pull/2866) skip LastResultsHash check if giga executor is on
* [#2865](https://github.com/sei-protocol/sei-chain/pull/2865) Fix: validate AppQC/CommitQC index alignment
* [#2864](https://github.com/sei-protocol/sei-chain/pull/2864) Docs: clarify autobahn consensus vs avail roles
* [#2861](https://github.com/sei-protocol/sei-chain/pull/2861) feat: add parquet receipt store with DuckDB range queries
* [#2857](https://github.com/sei-protocol/sei-chain/pull/2857) fix: use MADV_RANDOM during loadtree
* [#2852](https://github.com/sei-protocol/sei-chain/pull/2852) perf(store): optimize `UpdateReadSet` allocation pattern
* [#2849](https://github.com/sei-protocol/sei-chain/pull/2849) perf(store): remove dead `sortedStore` field from `VersionIndexedStore`
* [#2835](https://github.com/sei-protocol/sei-chain/pull/2835) feat: embed genesis for well-known chains
* [#2828](https://github.com/sei-protocol/sei-chain/pull/2828) emit rewards withdrawn events for delegate
* [#2827](https://github.com/sei-protocol/sei-chain/pull/2827) [giga] clear up cache after Write
* [#2818](https://github.com/sei-protocol/sei-chain/pull/2818) Cap pebble compaction concurrency in EVM storage
* [#2817](https://github.com/sei-protocol/sei-chain/pull/2817) Flatten `sei-tendermint` go mod into `sei-chain`
* [#2816](https://github.com/sei-protocol/sei-chain/pull/2816) feat: add configurable I/O rate limiting for snapshot writes
* [#2814](https://github.com/sei-protocol/sei-chain/pull/2814) feat: make snapshot prune async with CAS-based concurrency control
* [#2813](https://github.com/sei-protocol/sei-chain/pull/2813) Add tps calculation based on instant blocks
* [#2811](https://github.com/sei-protocol/sei-chain/pull/2811) chore(refactor): drop unused code
* [#2810](https://github.com/sei-protocol/sei-chain/pull/2810) fix(giga): check whether txs follow Giga ordering
* [#2807](https://github.com/sei-protocol/sei-chain/pull/2807) fix(test): force GC behaviour for determinism
* [#2804](https://github.com/sei-protocol/sei-chain/pull/2804) perf(store): lazy-init `sortedCache` in `cachekv.Store`
* [#2800](https://github.com/sei-protocol/sei-chain/pull/2800) fix: pruning goroutine lifecycle and prune failure snapshot
* [#2798](https://github.com/sei-protocol/sei-chain/pull/2798) Update ledger-go dependency
* [#2797](https://github.com/sei-protocol/sei-chain/pull/2797) [giga] avoid double signature decoding
* [#2796](https://github.com/sei-protocol/sei-chain/pull/2796) fix: suppress expected ErrAggregateVoteExist error logs in gasless metrics
* [#2795](https://github.com/sei-protocol/sei-chain/pull/2795) fix(store): preserve keys in nested CacheMultiStore
* [#2794](https://github.com/sei-protocol/sei-chain/pull/2794) go bench read + write receipts/logs for parquet vs pebble
* [#2793](https://github.com/sei-protocol/sei-chain/pull/2793) feat(sei-db): add flatkv store implementation
* [#2791](https://github.com/sei-protocol/sei-chain/pull/2791) Autobahn migrated from sei-v3
* [#2788](https://github.com/sei-protocol/sei-chain/pull/2788) feat: add ledger cache layer for receipt store
* [#2786](https://github.com/sei-protocol/sei-chain/pull/2786) Consolidate SC configurations and interface for Giga
* [#2784](https://github.com/sei-protocol/sei-chain/pull/2784) Fix typo in backport CI workflow name
* [#2783](https://github.com/sei-protocol/sei-chain/pull/2783) Upgrade to latest UCI workflows
* [#2781](https://github.com/sei-protocol/sei-chain/pull/2781) emit rewards withdrawn events for redelegate/undelegate
* [#2780](https://github.com/sei-protocol/sei-chain/pull/2780) add original cachekv as base layer
* [#2779](https://github.com/sei-protocol/sei-chain/pull/2779) Add scenario capability to benchmark script
* [#2775](https://github.com/sei-protocol/sei-chain/pull/2775) Added GigaRouter stub (CON-157)
* [#2774](https://github.com/sei-protocol/sei-chain/pull/2774) Fixed flaky p2p test
* [#2770](https://github.com/sei-protocol/sei-chain/pull/2770) Add features & knobs to benchmark.sh
* [#2768](https://github.com/sei-protocol/sei-chain/pull/2768) feat: Giga RPC node (OCC execution)
* [#2767](https://github.com/sei-protocol/sei-chain/pull/2767) fix: support iavl.* pruning config keys with legacy fallback
* [#2764](https://github.com/sei-protocol/sei-chain/pull/2764) Separate test coverage report from test wit race detector enabled
* [#2762](https://github.com/sei-protocol/sei-chain/pull/2762) Support query latest state when SS disabled
* [#2761](https://github.com/sei-protocol/sei-chain/pull/2761) Skip slow `sei-iavl` tests on PR if unchanged
* [#2760](https://github.com/sei-protocol/sei-chain/pull/2760) feat(giga): add UseRegularStore in xbank keeper fork
* [#2758](https://github.com/sei-protocol/sei-chain/pull/2758) default SSTORE value back to original 20k
* [#2756](https://github.com/sei-protocol/sei-chain/pull/2756) Composite State Store part 3: Read path implementation
* [#2755](https://github.com/sei-protocol/sei-chain/pull/2755) Composite State Store Part 2: EVM database implementation
* [#2754](https://github.com/sei-protocol/sei-chain/pull/2754) Composite State Store part 1: EVM config and type definitions
* [#2753](https://github.com/sei-protocol/sei-chain/pull/2753) [giga] honor configured sstore gas values
* [#2751](https://github.com/sei-protocol/sei-chain/pull/2751) Add changelog for 6.2 and 6.3
* [#2750](https://github.com/sei-protocol/sei-chain/pull/2750) feat: deflake TestMConnectionReadErrorUnknownChannel
* [#2745](https://github.com/sei-protocol/sei-chain/pull/2745) [giga] Add UseRegularStore flag to GigaEvmKeeper for testing fallback
* [#2744](https://github.com/sei-protocol/sei-chain/pull/2744) Add go generate script to download evmone libraries on the fly
* [#2743](https://github.com/sei-protocol/sei-chain/pull/2743) [giga] Add toEvmcError() for Go to EVMC error conversion
* [#2738](https://github.com/sei-protocol/sei-chain/pull/2738) Log into dockerhub with RO credentials to avoid pull rate limiting
* [#2737](https://github.com/sei-protocol/sei-chain/pull/2737) [giga] fork bank keeper
* [#2736](https://github.com/sei-protocol/sei-chain/pull/2736) Use `stable` foundry toolchain version for CI tests
* [#2731](https://github.com/sei-protocol/sei-chain/pull/2731) Add go bench for sc commit store
* [#2730](https://github.com/sei-protocol/sei-chain/pull/2730) [giga] Fix contract deploys and add integration test
* [#2729](https://github.com/sei-protocol/sei-chain/pull/2729) update: set MaxPacketMsgPayloadSize use MB unit
* [#2727](https://github.com/sei-protocol/sei-chain/pull/2727) Fix workflow dispatch for libwasmvm job
* [#2725](https://github.com/sei-protocol/sei-chain/pull/2725) fix: set max packet msg payload default to 1MB
* [#2724](https://github.com/sei-protocol/sei-chain/pull/2724) Add CI workflow to build libwasmvm dynamic libraries
* [#2723](https://github.com/sei-protocol/sei-chain/pull/2723) Update `cosmwasm` to reftype fix via forked `wasmparser`
* [#2719](https://github.com/sei-protocol/sei-chain/pull/2719) [STO-237] remove unused cosmos invariants
* [#2718](https://github.com/sei-protocol/sei-chain/pull/2718) Made tcp connection context-aware
* [#2717](https://github.com/sei-protocol/sei-chain/pull/2717) [giga] replace cosmos cachekv with Giga's impl
* [#2716](https://github.com/sei-protocol/sei-chain/pull/2716) refactor giga tests
* [#2715](https://github.com/sei-protocol/sei-chain/pull/2715) Configure self-hosted runners for Go tests
* [#2713](https://github.com/sei-protocol/sei-chain/pull/2713) Upgrade to Go `v1.25.6`
* [#2712](https://github.com/sei-protocol/sei-chain/pull/2712) fix and test
* [#2710](https://github.com/sei-protocol/sei-chain/pull/2710) feat(flatkv): introduce interface layer (Store/Iterator) and typed keys
* [#2709](https://github.com/sei-protocol/sei-chain/pull/2709) error handling for invalid curve25519 public keys
* [#2708](https://github.com/sei-protocol/sei-chain/pull/2708) feat: Giga RPC node (sequential execution)
* [#2707](https://github.com/sei-protocol/sei-chain/pull/2707) Add Ethereum state test runner for Giga executor validation
* [#2705](https://github.com/sei-protocol/sei-chain/pull/2705) Bootstrap `evmone` integration with build tags
* [#2702](https://github.com/sei-protocol/sei-chain/pull/2702) Fix mac local cluster
* [#2701](https://github.com/sei-protocol/sei-chain/pull/2701) [STO-308] New receiptDB receipt-specific interface
* [#2698](https://github.com/sei-protocol/sei-chain/pull/2698) fix: lthash worker loop break; remove unreachable digest.Read fallback
* [#2697](https://github.com/sei-protocol/sei-chain/pull/2697) [giga] fork x/evm
* [#2696](https://github.com/sei-protocol/sei-chain/pull/2696) Fix integration tests to run on release branch and clean up rules
* [#2695](https://github.com/sei-protocol/sei-chain/pull/2695) Upgrade to PebbleDB v2 + Add DefaultComparer Config Option
* [#2692](https://github.com/sei-protocol/sei-chain/pull/2692) fix double refund
* [#2688](https://github.com/sei-protocol/sei-chain/pull/2688) Update default `MaxGasWanted` in testnet to match mainnet
* [#2685](https://github.com/sei-protocol/sei-chain/pull/2685) [CON-102] fix: test: improve test failure conditions
* [#2684](https://github.com/sei-protocol/sei-chain/pull/2684) [CON-176] fix: test: don't run TestEventsTestSuite in parallel
* [#2683](https://github.com/sei-protocol/sei-chain/pull/2683) Remove Hash Range
* [#2682](https://github.com/sei-protocol/sei-chain/pull/2682) moved TCP buffering to SecretConnection.
* [#2680](https://github.com/sei-protocol/sei-chain/pull/2680) Fix rollback failure due to snapshot creation happened after app hash
* [#2679](https://github.com/sei-protocol/sei-chain/pull/2679) tcp multiplexer for sei giga
* [#2678](https://github.com/sei-protocol/sei-chain/pull/2678) Add CI workflow to publish containers to ECR
* [#2675](https://github.com/sei-protocol/sei-chain/pull/2675) Adjusted RPC http requests to use POST instead of GET
* [#2674](https://github.com/sei-protocol/sei-chain/pull/2674) fix flaky staking integration test
* [#2673](https://github.com/sei-protocol/sei-chain/pull/2673) Add `seictl` binary to `seid` container
* [#2671](https://github.com/sei-protocol/sei-chain/pull/2671) Refactor changelog to generic WAL
* [#2670](https://github.com/sei-protocol/sei-chain/pull/2670) Add failfast precompile to detect interop
* [#2669](https://github.com/sei-protocol/sei-chain/pull/2669) feat: mempool: return all EVM txs before others when reaping
* [#2667](https://github.com/sei-protocol/sei-chain/pull/2667) Make SSTORE chain param height-aware
* [#2666](https://github.com/sei-protocol/sei-chain/pull/2666) feat: add generic KV interfaces + Pebble adapter
* [#2661](https://github.com/sei-protocol/sei-chain/pull/2661) Add upgrade handler 6.2 6.3
* [#2660](https://github.com/sei-protocol/sei-chain/pull/2660) fix: cosmos: protect coin denom regexp with a lock
* [#2659](https://github.com/sei-protocol/sei-chain/pull/2659) Check storage is non-nil before attempting to close it
* [#2658](https://github.com/sei-protocol/sei-chain/pull/2658) Install CA certs on Ubuntu base image
* [#2657](https://github.com/sei-protocol/sei-chain/pull/2657) Remove redundant copy from dockerfile
* [#2655](https://github.com/sei-protocol/sei-chain/pull/2655) fix: correct TestAsyncComputeMissingRanges
* [#2654](https://github.com/sei-protocol/sei-chain/pull/2654) `evmc` VM and `giga` block processors (sequential and `OCC`)
* [#2653](https://github.com/sei-protocol/sei-chain/pull/2653) Seidb restructure
* [#2650](https://github.com/sei-protocol/sei-chain/pull/2650) Refactor of p2p secret connection
* [#2649](https://github.com/sei-protocol/sei-chain/pull/2649) IBC Toggle Inbound + Outbound
* [#2647](https://github.com/sei-protocol/sei-chain/pull/2647) feat: add live state LtHash library
* [#2642](https://github.com/sei-protocol/sei-chain/pull/2642) [CON-151] fix: test: replace flaky Sleep with more predictable wait
* [#2641](https://github.com/sei-protocol/sei-chain/pull/2641) feat: mempool: don't add pending txs to priority reservoir
* [#2636](https://github.com/sei-protocol/sei-chain/pull/2636) Use relative URLs in landing page of tendermint API
* [#2635](https://github.com/sei-protocol/sei-chain/pull/2635) [CON-154] fix: test: ensure own precommit before adding votes
* [#2634](https://github.com/sei-protocol/sei-chain/pull/2634) [CON-153] fix: lightclient: divergence detector should return upon sending error
* [#2632](https://github.com/sei-protocol/sei-chain/pull/2632) Log the panic callstack for debugging purposes
* [#2628](https://github.com/sei-protocol/sei-chain/pull/2628) fix: app: defensively check for nil tx
* [#2627](https://github.com/sei-protocol/sei-chain/pull/2627) fix: state: safely handle access list reverts
* [#2626](https://github.com/sei-protocol/sei-chain/pull/2626) [CON-152] fix: state: safely set per-account maps when handling reverts
* [#2625](https://github.com/sei-protocol/sei-chain/pull/2625) Change codecov patch target to `auto`
* [#2623](https://github.com/sei-protocol/sei-chain/pull/2623) Add staking queries and distr events to precompiles
* [#2622](https://github.com/sei-protocol/sei-chain/pull/2622) Flatten `sei-wasmd` into `sei-chain` module
* [#2621](https://github.com/sei-protocol/sei-chain/pull/2621) [CON-148] fix: tendermint: flaky state test
* [#2620](https://github.com/sei-protocol/sei-chain/pull/2620) fix: cosmos: correctly lock when getting/setting config
* [#2619](https://github.com/sei-protocol/sei-chain/pull/2619) [CON-146] fix: deflake address test
* [#2618](https://github.com/sei-protocol/sei-chain/pull/2618) migrated ed25519 primitives from sei-v3
* [#2612](https://github.com/sei-protocol/sei-chain/pull/2612) Remove out of date IBC docs
* [#2611](https://github.com/sei-protocol/sei-chain/pull/2611) Flatten `sei-ibc-go` module into `sei-chain`
* [#2609](https://github.com/sei-protocol/sei-chain/pull/2609) Add disable wasm test
* [#2608](https://github.com/sei-protocol/sei-chain/pull/2608) Flatten `sei-wasmvm` into `sei-chain` go module
* [#2607](https://github.com/sei-protocol/sei-chain/pull/2607) Integrate UCI automatic backporting
* [#2605](https://github.com/sei-protocol/sei-chain/pull/2605) [CON-134][CON-135] Bump cosmwasm-vm version to include fixes
* [#2604](https://github.com/sei-protocol/sei-chain/pull/2604) [CON-145] fix: deflake TestNewAnyWithCustomTypeURLWithErrorNoAllocation
* [#2603](https://github.com/sei-protocol/sei-chain/pull/2603) [CON-143] fix: deflake TestRouter_dialPeer_Reject
* [#2602](https://github.com/sei-protocol/sei-chain/pull/2602) Increase codecov change tolerance to 3%
* [#2601](https://github.com/sei-protocol/sei-chain/pull/2601) [giga] add executor interfaces for VM
* [#2600](https://github.com/sei-protocol/sei-chain/pull/2600) [CON-76] fix: sei-tendermint: include all fields in CommitHash
* [#2599](https://github.com/sei-protocol/sei-chain/pull/2599) [CON-140] feat: add benchmark test for tx execution
* [#2598](https://github.com/sei-protocol/sei-chain/pull/2598) Update dockerfile for caching efficiency and better libwasmvm handling
* [#2597](https://github.com/sei-protocol/sei-chain/pull/2597) Rebuild dynamic and static libwasmvm libs from code
* [#2596](https://github.com/sei-protocol/sei-chain/pull/2596) Fix flaky test for seidb
* [#2595](https://github.com/sei-protocol/sei-chain/pull/2595) Add lock to protect SetPrices in price feeder
* [#2594](https://github.com/sei-protocol/sei-chain/pull/2594) fixed flaky consensus state test
* [#2592](https://github.com/sei-protocol/sei-chain/pull/2592) canonical encoding for protobuf
* [#2591](https://github.com/sei-protocol/sei-chain/pull/2591) perf: improve eth_getLogs performance with early rejection and backpressure
* [#2590](https://github.com/sei-protocol/sei-chain/pull/2590) fix: mempool: enforce txBlacklisting for stupidly big txs
* [#2589](https://github.com/sei-protocol/sei-chain/pull/2589) refactor: mempool: remove unused totalCheckTxCount
* [#2587](https://github.com/sei-protocol/sei-chain/pull/2587) Flatten `sei-db` module
* [#2586](https://github.com/sei-protocol/sei-chain/pull/2586) Remove redundant mint protos left behind from Cosmos simulation logic
* [#2585](https://github.com/sei-protocol/sei-chain/pull/2585) removed support for secp256k1 and sr25519 as validator keys
* [#2584](https://github.com/sei-protocol/sei-chain/pull/2584) Fix incorrect ldflag for app name
* [#2583](https://github.com/sei-protocol/sei-chain/pull/2583) Always set gas meter for every transaction
* [#2582](https://github.com/sei-protocol/sei-chain/pull/2582) consensus WAL rewrite
* [#2577](https://github.com/sei-protocol/sei-chain/pull/2577) fix: race condition between Close() and background snapshot goroutine
* [#2574](https://github.com/sei-protocol/sei-chain/pull/2574) add Cosmos simulation back
* [#2569](https://github.com/sei-protocol/sei-chain/pull/2569) Refactor DeliverTx ante

## v6.3
sei-chain (Note: major repos have been merged into sei-chain)
* [#2580](https://github.com/sei-protocol/sei-chain/pull/2580) Fix: enforce EIP-6780 selfdestruct for prefunded addresses
* [#2572](https://github.com/sei-protocol/sei-chain/pull/2572) Extra checks in BitArray methods
* [#2570](https://github.com/sei-protocol/sei-chain/pull/2570) Strongly typed p2p channels
* [#2567](https://github.com/sei-protocol/sei-chain/pull/2567) Migrate sei-ibc-go into sei-chain as monorepo
* [#2563](https://github.com/sei-protocol/sei-chain/pull/2563) Do not return error string on precompile error
* [#2561](https://github.com/sei-protocol/sei-chain/pull/2561) Make seid rollback idempotent and remove --hard
* [#2560](https://github.com/sei-protocol/sei-chain/pull/2560) Fix: Resolve data race in parallel snapshot writing
* [#2558](https://github.com/sei-protocol/sei-chain/pull/2559) Remove custom json encoding of consensus internals and replay command
* [#2558](https://github.com/sei-protocol/sei-chain/pull/2558) Refactor of consensus reactor task management
* [#2553](https://github.com/sei-protocol/sei-chain/pull/2553) Refactor CheckTx
* [#2547](https://github.com/sei-protocol/sei-chain/pull/2547) Deprecate and clean up dbsync code reference
* [#2543](https://github.com/sei-protocol/sei-chain/pull/2543) Add a benchmark mode
* [#2542](https://github.com/sei-protocol/sei-chain/pull/2542) Config: Make worker pool configurable and increase default queue size
* [#2540](https://github.com/sei-protocol/sei-chain/pull/2540) Streamline EndBlock
* [#2539](https://github.com/sei-protocol/sei-chain/pull/2539) PeerManager rewrite
* [#2537](https://github.com/sei-protocol/sei-chain/pull/2537) Optimzation: Reduce snapshot creation time
* [#2534](https://github.com/sei-protocol/sei-chain/pull/2534) Remove ABCI socket/grpc functionality
* [#2533](https://github.com/sei-protocol/sei-chain/pull/2533) Migrate transaction embedding proto types to Go types
* [#2528](https://github.com/sei-protocol/sei-chain/pull/2528) Watermark fixes
* [#2527](https://github.com/sei-protocol/sei-chain/pull/2527) Darwin build fix
* [#2525](https://github.com/sei-protocol/sei-chain/pull/2525) Deprecate store streaming and listeners
* [#2522](https://github.com/sei-protocol/sei-chain/pull/2522) Flatten BeginBlock and remove nested logic
* [#2521](https://github.com/sei-protocol/sei-chain/pull/2521) Fix base field parsing for sei-cosmos toml
* [#2520](https://github.com/sei-protocol/sei-chain/pull/2520) Minor refactor to tracing
* [#2519](https://github.com/sei-protocol/sei-chain/pull/2519) Include price-feeder in seid container
* [#2517](https://github.com/sei-protocol/sei-chain/pull/2517) Remove vote extensions logic
* [#2516](https://github.com/sei-protocol/sei-chain/pull/2516) Use wire and wire-json to check for proto breaking changes
* [#2515](https://github.com/sei-protocol/sei-chain/pull/2515) Logging fixes
* [#2513](https://github.com/sei-protocol/sei-chain/pull/2513) Remove unused code pt 2
* [#2512](https://github.com/sei-protocol/sei-chain/pull/2512) Remove unused code
* [#2511](https://github.com/sei-protocol/sei-chain/pull/2511) Fix logging message for restore
* [#2510](https://github.com/sei-protocol/sei-chain/pull/2511) Get rid of god-cache janitor
* [#2509](https://github.com/sei-protocol/sei-chain/pull/2509) Address comments for tendermint p2p
* [#2507](https://github.com/sei-protocol/sei-chain/pull/2507) Remove SimApp and Cosmos simulation logic
* [#2506](https://github.com/sei-protocol/sei-chain/pull/2506) Fix: Set MinRetainBlocks=0 for archive node
* [#2504](https://github.com/sei-protocol/sei-chain/pull/2504) Remove aclaccesscontrol module and usages
* [#2503](https://github.com/sei-protocol/sei-chain/pull/2503) Fix sei-db race conditions
* [#2497](https://github.com/sei-protocol/sei-chain/pull/2497) Feat: optimize memIAVL cold-start with sequential snapshot prefetch
* [#2494](https://github.com/sei-protocol/sei-chain/pull/2494) Fix bloom fallback behavior
* [#2491](https://github.com/sei-protocol/sei-chain/pull/2491) Fix gap nonce inclusion
* [#2490](https://github.com/sei-protocol/sei-chain/pull/2490) Config: reorganize configuration files with auto-managed fields settings
* [#2487](https://github.com/sei-protocol/sei-chain/pull/2487) Made tendermint reactors open channels in constructor
* [#2485](https://github.com/sei-protocol/sei-chain/pull/2485) Disable HashRange by default
* [#2484](https://github.com/sei-protocol/sei-chain/pull/2484) Fix compile error in sei-wasmd
* [#2480](https://github.com/sei-protocol/sei-chain/pull/2480) Remove redundant codecov config in sei-db and fix coverage upload
* [#2479](https://github.com/sei-protocol/sei-chain/pull/2479) Config: set pruning=nothing for all nodes
* [#2476](https://github.com/sei-protocol/sei-chain/pull/2476) DNS resolution test for ResolveAddressString
* [#2475](https://github.com/sei-protocol/sei-chain/pull/2475) Fix pruning MVCC error
* [#2471](https://github.com/sei-protocol/sei-chain/pull/2471) Simplified p2p.Channel
* [#2470](https://github.com/sei-protocol/sei-chain/pull/2470) Reverted semantics of ParseAddressString
* [#2469](https://github.com/sei-protocol/sei-chain/pull/2469) Config: Keep rosetta.enable=false by default for all kidns of nodes
* [#2468](https://github.com/sei-protocol/sei-chain/pull/2468) Remove sqlite and make latest version update atomic in SS
* [#2467](https://github.com/sei-protocol/sei-chain/pull/2467) Simply tracer enabled checks throughout sei-chain/cosmos app
* [#2465](https://github.com/sei-protocol/sei-chain/pull/2465) Integrate watermark in evmrpc
* [#2463](https://github.com/sei-protocol/sei-chain/pull/2463) State store metrics PebbleDB
* [#2462](https://github.com/sei-protocol/sei-chain/pull/2462) Automate and fix ProtocolBuffer generation across all sub modules
* [#2460](https://github.com/sei-protocol/sei-chain/pull/2460) Cherry pick remaining seidb commits
* [#2458](https://github.com/sei-protocol/sei-chain/pull/2458) Port timeoutTicker fix
* [#2456](https://github.com/sei-protocol/sei-chain/pull/2456) Feat: Add mode-based configuration for seid init
* [#2454](https://github.com/sei-protocol/sei-chain/pull/2454) Fix RPC read race
* [#2452](https://github.com/sei-protocol/sei-chain/pull/2452) Cherrypick RPC CPU optimization changes
* [#2450](https://github.com/sei-protocol/sei-chain/pull/2450) Get sender in txpool with relevant signer
* [#2449](https://github.com/sei-protocol/sei-chain/pull/2449) Delete existing zeroed out EVM contract state
* [#2448](https://github.com/sei-protocol/sei-chain/pull/2448) Merged Router and Transport
* [#2446](https://github.com/sei-protocol/sei-chain/pull/2446) Delete future zeroed out state from chain state
* [#2443](https://github.com/sei-protocol/sei-chain/pull/2443) Add otel metric utils provider
* [#2442](https://github.com/sei-protocol/sei-chain/pull/2442) Fix to tcp conneciton leak
* [#2440](https://github.com/sei-protocol/sei-chain/pull/2440) Reverted SendRate/RecvRate=0 semantics
* [#2439](https://github.com/sei-protocol/sei-chain/pull/2439) Add metrics for nonce mismatch & pending nonce
* [#2435](https://github.com/sei-protocol/sei-chain/pull/2435) Bump SeiDB to include rocksdb
* [#2434](https://github.com/sei-protocol/sei-chain/pull/2434) Config: update sei-tendermint default configs
* [#2431](https://github.com/sei-protocol/sei-chain/pull/2431) Remove Transport mock
* [#2430](https://github.com/sei-protocol/sei-chain/pull/2422) Refactor of MConnection internals
* [#2428](https://github.com/sei-protocol/sei-chain/pull/2428) Increase tm event buffer to reduce critical path backpressure
* [#2423](https://github.com/sei-protocol/sei-chain/pull/2423) Config: update app config default values
* [#2422](https://github.com/sei-protocol/sei-chain/pull/2422) Fix sender discrepancy on RPC reads
* [#2421](https://github.com/sei-protocol/sei-chain/pull/2421) Fix: Add recovery on CreateProposalBlock
* [#2420](https://github.com/sei-protocol/sei-chain/pull/2420) Upgrade to go 1.24.5
* [#2419](https://github.com/sei-protocol/sei-chain/pull/2419) Remove duplicate panic recovery in process proposal
* [#2418](https://github.com/sei-protocol/sei-chain/pull/2418) Remove prefill estimates scheduler code path
* [#2414](https://github.com/sei-protocol/sei-chain/pull/2414) Do not resolve latest upon error
* [#2412](https://github.com/sei-protocol/sei-chain/pull/2412) Add logic to handle single NFT claim
* [#2399](https://github.com/sei-protocol/sei-chain/pull/2399) Fix cosmos priority and add unit test
* [#2397](https://github.com/sei-protocol/sei-chain/pull/2397) Update error msg for v2 upgrade
* [#2389](https://github.com/sei-protocol/sei-chain/pull/2389) Parameterize SSTORE
* [#2388](https://github.com/sei-protocol/sei-chain/pull/2388) Cherrypick RPC fixes from v6.1.11
* [#2377](https://github.com/sei-protocol/sei-chain/pull/2377) Fix block gas used
* [#2374](https://github.com/sei-protocol/sei-chain/pull/2374) Estimate gas fix
* [#2345](https://github.com/sei-protocol/sei-chain/pull/2345) Fix: Add panic recovery to ProcessProposalHandler goroutine
* [#2320](https://github.com/sei-protocol/sei-chain/pull/2320) Implement standalone transaction prioritizer

Other fixes included that were squashed by monorepo work
* [Add otel metrics for seidb](https://github.com/sei-protocol/sei-chain/commit/c0e868d45adc00c0e27c932546c678a069b3d544)
* [Upgrade to Go 1.24 and fix lint issues](https://github.com/sei-protocol/sei-chain/commit/fcf9de74d902db49ff364918d8ed9079d28f0312)
* [Rocksdb update interface](https://github.com/sei-protocol/sei-chain/commit/e314508ebf75775d0c20ec7473ba5741ebc63f08)
* [Removed MemoryTransport](https://github.com/sei-protocol/sei-chain/commit/e8d4e7b867b418881c920dd0b6efcac15d854858)
* [MemIAVL Create snapshot whenever height diff exceeds interval](https://github.com/sei-protocol/sei-chain/commit/123dd8f7d8b5f9d1cf5d549e325fd058d79b30d9)
* [Fix cosmos limit big integer range](https://github.com/sei-protocol/sei-chain/commit/ef0bb143bfac512f029e88a0cdce810c5e542f19)
* [Add more trace spans to execution critical path](https://github.com/sei-protocol/sei-chain/commit/854381055c7e7a6917eab50e216fb1ddec5f77a8)
* [Add GetTxPriorityHint and mempool backpressure via priority drops](https://github.com/sei-protocol/sei-chain/commit/94f51a514582889c8af929698850d0032d3e74c1)
* [MemIAVL should only keep 1 snapshot](https://github.com/sei-protocol/sei-chain/commit/62ed63a645cb50e9c1aaa032f906afd4597edd8a)
* [Fix: Add recovery on CreateProposalBlock](https://github.com/sei-protocol/sei-chain/commit/6c96c70d2b6c114697dbba3eeb331b7a7a3c9a4f)
* [Refactor of TCP connection lifecycle](https://github.com/sei-protocol/sei-chain/commit/3bfb0fc260d77810411eb6e6d909f399d351c21a)
* [Fix cache max size for duplicate txs](https://github.com/sei-protocol/sei-chain/commit/7f34114feebaa0bb110bf9840ac1002121737f09)
* [Fix for contention on heightIndex in mempool](https://github.com/sei-protocol/sei-chain/commit/06dc2f6607662428ae222a70a95b1f646bfda388)
* [Remove support for vote extensions](https://github.com/sei-protocol/sei-chain/commit/b3c3ea55524296be0625be28eba796cb260e05cd)
* [Tendermint Estimate Gas Fix](https://github.com/sei-protocol/sei-chain/commit/4209f85fd264b9efcc6523f7723e7bf06e20f276)
* [Hardcoded simple-priority queue as the only message queue](https://github.com/sei-protocol/sei-chain/commit/44dcb81e7ce3f385034513d196d2352bd4d8c5bb)
* [Commit to metadata table for state analysis](https://github.com/sei-protocol/sei-chain/commit/859c9e9abf1a7af64dad95bf3fe93764b2ef80c1)
* [Only allow 1 tx per envelope](https://github.com/sei-protocol/sei-chain/commit/2b3572d052bf86b61426812872c523f7c99138df)

## v6.2.0
sei-chain
* [#2444](https://github.com/sei-protocol/sei-chain/pull/2444) Optimize getLogs performance
* [#2437](https://github.com/sei-protocol/sei-chain/pull/2437) Fix sender discrepancy on RPC reads
* [#2371](https://github.com/sei-protocol/sei-chain/pull/2371) Always include synthetic logs in eth_ endpoints
* [#2364](https://github.com/sei-protocol/sei-chain/pull/2364) eth_gasPrice fixes
* [#2361](https://github.com/sei-protocol/sei-chain/pull/2361) Exclude synthetic logs from receipts returned by eth_
* [#2344](https://github.com/sei-protocol/sei-chain/pull/2344) Skip txs failing ante when counting tx index for receipts
* [#2343](https://github.com/sei-protocol/sei-chain/pull/2343) Fix ante failure check in RPC
* [#2272](https://github.com/sei-protocol/sei-chain/pull/2272) Add make target for mock balances
* [#2271](https://github.com/sei-protocol/sei-chain/pull/2271) Fix cumulativeGasUsed == 0
* [#2269](https://github.com/sei-protocol/sei-chain/pull/2269) Add compile flagged mock balance testing functionality
* [#2268](https://github.com/sei-protocol/sei-chain/pull/2268) Only synthetic logs for Sei endpoints
* [#2265](https://github.com/sei-protocol/sei-chain/pull/2265) Bump geth to allow for skipping nonce bump
* [#2263](https://github.com/sei-protocol/sei-chain/pull/2263) Do not take a new snapshot upon RevertToSnapshot
* [#2262](https://github.com/sei-protocol/sei-chain/pull/2262) Consistent Gas Limit across RPC and Opcode
* [#2261](https://github.com/sei-protocol/sei-chain/pull/2261) Bump Geth for request size limit to 10MB
* [#2258](https://github.com/sei-protocol/sei-chain/pull/2258) Fix static fee history gas used ratio
* [#2256](https://github.com/sei-protocol/sei-chain/pull/2256) Fix data race in price-feeder websocket controller
* [#2255](https://github.com/sei-protocol/sei-chain/pull/2255) Optimization: CreateAccount only clears state if code hash exists
* [#2251](https://github.com/sei-protocol/sei-chain/pull/2251) Update oracle MidBlock logic
* [#2250](https://github.com/sei-protocol/sei-chain/pull/2250) Make flushing receipt synchronous
* [#2239](https://github.com/sei-protocol/sei-chain/pull/2239) Remove writeset estimation to alleviate AccAddress mutex contention
* [#2238](https://github.com/sei-protocol/sei-chain/pull/2238) Bump btcec to v2.3.2, x/crypto to v0.31.0
* [#2236](https://github.com/sei-protocol/sei-chain/pull/2236) Harden solo precompile
* [#2235](https://github.com/sei-protocol/sei-chain/pull/2235) Rate limit eth call in Simulation API
* [#2234](https://github.com/sei-protocol/sei-chain/pull/2234) Use legacy transaction decoder for historical height
* [#2233](https://github.com/sei-protocol/sei-chain/pull/2233) Exclude transactions that failed ante from getTransaction
* [#2232](https://github.com/sei-protocol/sei-chain/pull/2232) Require MsgClaim sender to match signer
* [#2292](https://github.com/sei-protocol/sei-chain/pull/2292) Remove receipts from chain state
* [#2225](https://github.com/sei-protocol/sei-chain/pull/2225) Fix tx index in getTransactionByHash response
* [#2219](https://github.com/sei-protocol/sei-chain/pull/2219) Re-enable p256 precompile
* [#2218](https://github.com/sei-protocol/sei-chain/pull/2218) Add gov proposal for rechecktx
* [#2210](https://github.com/sei-protocol/sei-chain/pull/2210) Refactor versioned precompiles & add automation scripts
* [#2074](https://github.com/sei-protocol/sei-chain/pull/2074) Pectra upgrade

sei-tendermint
* [#331](https://github.com/sei-protocol/sei-tendermint/pull/331) Fixed timeoutTicker
* [#314](https://github.com/sei-protocol/sei-tendermint/pull/314) Estimate gas fix
* [#309](https://github.com/sei-protocol/sei-tendermint/pull/309) Remove tx cache memory footprint by half
* [#308](https://github.com/sei-protocol/sei-tendermint/pull/308) Hardcoded simple-priority queue as the only message queue
* [#307](https://github.com/sei-protocol/sei-tendermint/pull/307) Set default RemoveExpiredTxsFromQueue to be true
* [#305](https://github.com/sei-protocol/sei-tendermint/pull/305) Only allow 1 tx per envelope
* [#304](https://github.com/sei-protocol/sei-tendermint/pull/304) Validate peer block height in block sync
* [#300](https://github.com/sei-protocol/sei-tendermint/pull/300) BaseService refactor
* [#299](https://github.com/sei-protocol/sei-tendermint/pull/299) Add metrics to track duplicate txs
* [#298](https://github.com/sei-protocol/sei-tendermint/pull/298) Bump golang to 1.24.5
* [#296](https://github.com/sei-protocol/sei-tendermint/pull/296) More granular buckets for consensus histograms
* [#291](https://github.com/sei-protocol/sei-tendermint/pull/291) Verify proposer selection algo upon state sync
* [#290](https://github.com/sei-protocol/sei-tendermint/pull/290) Prevent excssive Total values
* [#289](https://github.com/sei-protocol/sei-tendermint/pull/289) Purge expired txs from mempool cleanly
* [#287](https://github.com/sei-protocol/sei-tendermint/pull/287) Bump btcec to v2.3.2, x/crypto to v0.31.0

## v6.2.0
sei-chain
* [#2271](https://github.com/sei-protocol/sei-chain/pull/2271) Fix cumulativeGasUsed == 0
* [#2262](https://github.com/sei-protocol/sei-chain/pull/2262) Consistent Gas Limit across RPC and Opcode
* [#2263](https://github.com/sei-protocol/sei-chain/pull/2263) Do not take a new snapshot upon RevertToSnapshot
* [#2272](https://github.com/sei-protocol/sei-chain/pull/2272) Add make target for mock balances
* [#2258](https://github.com/sei-protocol/sei-chain/pull/2258) Fix static fee history gas used ratio
* [#2269](https://github.com/sei-protocol/sei-chain/pull/2269) Add compile flagged mock balance testing functionality
* [#2265](https://github.com/sei-protocol/sei-chain/pull/2265) Bump geth to allow for skipping nonce bump
* [#2235](https://github.com/sei-protocol/sei-chain/pull/2235) Rate limit eth call in Simulation API
* [#2261](https://github.com/sei-protocol/sei-chain/pull/2261) Bump Geth for request size limit to 10MB
* [#2255](https://github.com/sei-protocol/sei-chain/pull/2255) Optimization: CreateAccount only clears state if code hash exists
* [#2238](https://github.com/sei-protocol/sei-chain/pull/2238) Bump btcec to v2.3.2, x/crypto to v0.31.0
* [#2234](https://github.com/sei-protocol/sei-chain/pull/2234) Use legacy transaction decoder for historical height
* [#2250](https://github.com/sei-protocol/sei-chain/pull/2250) Make flushing receipt synchronous
* [#2251](https://github.com/sei-protocol/sei-chain/pull/2251) Update oracle MidBlock logic
* [#2256](https://github.com/sei-protocol/sei-chain/pull/2256) Fix data race in price-feeder websocket controller
* [#2236](https://github.com/sei-protocol/sei-chain/pull/2236) Harden solo precompile
* [#2232](https://github.com/sei-protocol/sei-chain/pull/2232) Require MsgClaim sender to match signer
* [#2239](https://github.com/sei-protocol/sei-chain/pull/2239) Remove writeset estimation to alleviate AccAddress mutex contention
* [#2233](https://github.com/sei-protocol/sei-chain/pull/2233) Exclude transactions that failed ante from getTransaction
* [#2210](https://github.com/sei-protocol/sei-chain/pull/2210) Refactor versioned precompiles & add automation scripts
* [#2225](https://github.com/sei-protocol/sei-chain/pull/2225) Fix tx index in getTransactionByHash response
* [#2218](https://github.com/sei-protocol/sei-chain/pull/2218) Add gov proposal for rechecktx
* [#2219](https://github.com/sei-protocol/sei-chain/pull/2219) Re-enable p256 precompile
* [#2074](https://github.com/sei-protocol/sei-chain/pull/2074) Pectra upgrade

go-ethereum
* [#63](https://github.com/sei-protocol/go-ethereum/pull/63) Allow nonce bump to be skipped
* [#62](https://github.com/sei-protocol/go-ethereum/pull/62) Expose set read limits for websocket server to prevent OOM
* [#59](https://github.com/sei-protocol/go-ethereum/pull/59) Pectra upgrade

## v6.1.4
sei-chain
* [#2234](https://github.com/sei-protocol/sei-chain/pull/2234) Use legacy transaction decoder for historical height
* [#2223](https://github.com/sei-protocol/sei-chain/pull/2223) Update Pointer Cache
* [#2211](https://github.com/sei-protocol/sei-chain/pull/2211) Fix: use evm only index in eth_getLogs
* [#2220](https://github.com/sei-protocol/sei-chain/pull/2220) Exclude transactions that failed ante from fee history calculation
* [#2204](https://github.com/sei-protocol/sei-chain/pull/2204) Fix: blockhash issue in eth_getLog
* [#2203](https://github.com/sei-protocol/sei-chain/pull/2203) Make MaxFee and MaxPriorityFee optional for eth_call (NoBaseFee:true)
* [#2217](https://github.com/sei-protocol/sei-chain/pull/2217) Fix eth_feeHistory empty blocks
* [#2215](https://github.com/sei-protocol/sei-chain/pull/2215) Option for unlimited Debug Trace lookback
* [#2214](https://github.com/sei-protocol/sei-chain/pull/2214) Fix log index on tx receipt
* [#2195](https://github.com/sei-protocol/sei-chain/pull/2195) Feat: optimize eth_getLogs scalability

## v6.1.0
sei-chain
* [#2194](https://github.com/sei-protocol/sei-chain/pull/2194) Fix access list height check
* [#2187](https://github.com/sei-protocol/sei-chain/pull/2187) Add command to take state sync snapshot at specific height
* [#2186](https://github.com/sei-protocol/sei-chain/pull/2186) Disable CW -> ERC Register Pointer
* [#2183](https://github.com/sei-protocol/sei-chain/pull/2183) Add missing methods to distribution precompile
* [#2180](https://github.com/sei-protocol/sei-chain/pull/2180) Add missing methods to staking precompile
* [#2179](https://github.com/sei-protocol/sei-chain/pull/2179) Use H+1 oracle for state during tracing
* [#2176](https://github.com/sei-protocol/sei-chain/pull/2176) Use pointer addr for to address in synthetic tx
* [#2175](https://github.com/sei-protocol/sei-chain/pull/2175) Update docker with wasm v1.5.5
* [#2173](https://github.com/sei-protocol/sei-chain/pull/2173) Add missing methods to gov precompile
* [#2171](https://github.com/sei-protocol/sei-chain/pull/2171) debug_trace Add Timeout + Rate Limit + Lookback + Concurrent calls max
* [#2166](https://github.com/sei-protocol/sei-chain/pull/2166) Recover panics from unmanaged goroutines
* [#2163](https://github.com/sei-protocol/sei-chain/pull/2163) Fix gas consumption for historical block tracing
* [#2158](https://github.com/sei-protocol/sei-chain/pull/2158) Fix oracle extremely slow query
* [#2156](https://github.com/sei-protocol/sei-chain/pull/2156) Deprecate MinTxsPerBlock

sei-cosmos
* [#584](https://github.com/sei-protocol/sei-cosmos/pull/584) Add new config OnlyAllowExportOnSnapshotVersion for sc
* [#580](https://github.com/sei-protocol/sei-cosmos/pull/580) Add nextMs to context
* [#579](https://github.com/sei-protocol/sei-cosmos/pull/579) Add store tracer


sei-tendermint
* [#284](https://github.com/sei-protocol/sei-tendermint/pull/284) Add godeltapprof to sei-tendermint to serve additional profiling data

## v6.0.6
sei-chain
* [#2161](https://github.com/sei-protocol/sei-chain/pull/2161) Filter EVM Rpc default case
* [#2160](https://github.com/sei-protocol/sei-chain/pull/2160) Remove Evmrpc Filter Panic
* [#2157](https://github.com/sei-protocol/sei-chain/pull/2157) Fix getLog&getReceipt txIndex mismatch
* [#2151](https://github.com/sei-protocol/sei-chain/pull/2151) Fix EVM RPC denylist config
* [#2143](https://github.com/sei-protocol/sei-chain/pull/2143) Harden oracle tx spam prevention
* [#2139](https://github.com/sei-protocol/sei-chain/pull/2139) Call antehandlers for traceBlock
* [#2136](https://github.com/sei-protocol/sei-chain/pull/2136) Backfill from/to on receipts for failed txs
* [#2135](https://github.com/sei-protocol/sei-chain/pull/2135) Use geth create trace for pointer trace
* [#2134](https://github.com/sei-protocol/sei-chain/pull/2134) Add tracing to precompiles
* [#2133](https://github.com/sei-protocol/sei-chain/pull/2133) Fix receipt tx index confusion
* [#2127](https://github.com/sei-protocol/sei-chain/pull/2127) Fix getlogs deadlock
* [#2123](https://github.com/sei-protocol/sei-chain/pull/2123) Fix getBlock endpoints transactionIndex
* [#2122](https://github.com/sei-protocol/sei-chain/pull/2122) Use versioned precompiles in tracing
* [#2118](https://github.com/sei-protocol/sei-chain/pull/2118) Add back legacy precompile versions
* [#2117](https://github.com/sei-protocol/sei-chain/pull/2117) Overwrite block hash in tracer response with tendermint block hash
* [#2112](https://github.com/sei-protocol/sei-chain/pull/2112) Return error when log requested with too wide ranges
* [#2110](https://github.com/sei-protocol/sei-chain/pull/2110) Disallow future block number to be passed to balance queries

sei-tendermint
* [#260](https://github.com/sei-protocol/sei-tendermint/pull/260) Add logs/metrics for block proposal
* [#274](https://github.com/sei-protocol/sei-tendermint/pull/274) Fix ToReqBeginBlock
* [#277](https://github.com/sei-protocol/sei-tendermint/pull/277) Fix goroutine leak during block sync
* [#275](https://github.com/sei-protocol/sei-tendermint/pull/275) Unsafe reset all fix

sei-db
* [#87](https://github.com/sei-protocol/sei-db/pull/87) Add Upper Bound ReverseIterator

## v6.0.5
sei-chain
* [#2100](https://github.com/sei-protocol/sei-chain/pull/2100) Refactor RPC log logic
* [#2092](https://github.com/sei-protocol/sei-chain/pull/2092) Integrate with MaxGasWanted

sei-cosmos
* [#567](https://github.com/sei-protocol/sei-cosmos/pull/567) Do no use legacy marshaling on key exports

sei-tendermint
* [#271](https://github.com/sei-protocol/sei-tendermint/pull/271) Use txs from SafeGetTxsByKeys
* [#269](https://github.com/sei-protocol/sei-tendermint/pull/269) Make missing txs check atomic
* [#267](https://github.com/sei-protocol/sei-tendermint/pull/267) Add a hard max gas wanted at 50mil gas as a consensus param

sei-db
* [#82](https://github.com/sei-protocol/sei-db/pull/82) Improve SeiDB replay&restart time by 2x

## v6.0.4
sei-chain
* [#2091](https://github.com/sei-protocol/sei-chain/pull/2091) Fix RPC subscription fields
* [#2089](https://github.com/sei-protocol/sei-chain/pull/2089) Tracer RPC fixes
* [#2087](https://github.com/sei-protocol/sei-chain/pull/2087) Make coinbase distribution in EndBlock more efficient
* [#2085](https://github.com/sei-protocol/sei-chain/pull/2085) Allow safe/latest/final to be passed as block number to trace/simulate endpoints
* [#2075](https://github.com/sei-protocol/sei-chain/pull/2075) Improve pointer/pointee query UX
* [#2073](https://github.com/sei-protocol/sei-chain/pull/2073) RPC simulation with gas used estimate tagging
* [#2071](https://github.com/sei-protocol/sei-chain/pull/2071) Improve tracer/simulation RPC
* [#2068](https://github.com/sei-protocol/sei-chain/pull/2068) Fix eth_gasPrice not found
* [#2067](https://github.com/sei-protocol/sei-chain/pull/2067) Set log index across all transactions in a block
* [#2065](https://github.com/sei-protocol/sei-chain/pull/2064) Add sei2_getBlock endpoints to include bank transfers
* [#2059](https://github.com/sei-protocol/sei-chain/pull/2059) Add tools to scan and compute hash for IAVL db
* [#2058](https://github.com/sei-protocol/sei-chain/pull/2058) Exclude Synthetic txs from *ExcludePanicTx endpoints
* [#2054](https://github.com/sei-protocol/sei-chain/pull/2054) Add extractAsBytesFromArray method for JSON precompile
* [#2050](https://github.com/sei-protocol/sei-chain/pull/2050) Extract multiple EVM logs from a single WASM event
* [#2048](https://github.com/sei-protocol/sei-chain/pull/2048) Add logic to remove a small number of tx hashes each block

sei-cosmos
* [#568](https://github.com/sei-protocol/sei-cosmos/pull/568) Blacklist evm coinbase address from receiving
* [#565](https://github.com/sei-protocol/sei-cosmos/pull/565) Bypass unnecessary logics in BeginBlock for simulate
* [#564](https://github.com/sei-protocol/sei-cosmos/pull/564) Add whitelist for fee denoms

sei-tendermint
* [#265](https://github.com/sei-protocol/sei-tendermint/pull/264) Fix: peer manager nil pointer
* [#263](https://github.com/sei-protocol/sei-tendermint/pull/263) Update ReapMaxBytesMaxGas to include estimated gas
* [#259](https://github.com/sei-protocol/sei-tendermint/pull/259) Add simulate flag to RequestBeginBlock
* [#258](https://github.com/sei-protocol/sei-tendermint/pull/258) Add utils to get RequestBeginBlock
 
## v6.0.3
sei-chain
* [#2057](https://github.com/sei-protocol/sei-chain/pull/2057) Avoid panic tx error message in debug trace
* [#2056](https://github.com/sei-protocol/sei-chain/pull/2056) Properly encode ERC1155 translated batch event data
* [#2051](https://github.com/sei-protocol/sei-chain/pull/2051) Add IBC support for 0x addresses
* [#2027](https://github.com/sei-protocol/sei-chain/pull/2027) Fix eth_subscribe with geth open ended range
* [#2043](https://github.com/sei-protocol/sei-chain/pull/2043) Query owner on ERC-721 and ERC-1155 pointers
* [#2044](https://github.com/sei-protocol/sei-chain/pull/2044) Support JS tracers
* [#2031](https://github.com/sei-protocol/sei-chain/pull/2031) Add custom query handling for unbonding balances
* [#1755](https://github.com/sei-protocol/sei-chain/pull/1755) Pointer contracts: support for ERC1155 and CW1155 contracts

## v6.0.2
sei-chain
* [#2018](https://github.com/sei-protocol/sei-chain/pull/2018) Remove TxHashes from EVM module
* [#2006](https://github.com/sei-protocol/sei-chain/pull/2006) Fix volatile eth_gasPrice
* [#2005](https://github.com/sei-protocol/sei-chain/pull/2005) Exclude block receipts whose block number do not match
* [#2004](https://github.com/sei-protocol/sei-chain/pull/2004) Integrate with MinTxsInBlock
* [#1983](https://github.com/sei-protocol/sei-chain/pull/1983) Handle oracle overflow and rounding to zero
* [#2002](https://github.com/sei-protocol/sei-chain/pull/2002) Update IBC version to use utc on error msg
* [#2000](https://github.com/sei-protocol/sei-chain/pull/2000) Catch panic in trace transaction / trace call
* [#1995](https://github.com/sei-protocol/sei-chain/pull/1995) RPC endpoints for excluding tracing failures
* [#1993](https://github.com/sei-protocol/sei-chain/pull/1993) Avoid panic in getLogs
* [#1991](https://github.com/sei-protocol/sei-chain/pull/1991) Add defer recovery for failed txs when tracing and estimating gas
* [#1988](https://github.com/sei-protocol/sei-chain/pull/1988) getLogs endpoint should check whether or not to include cosmos txs based on namespace
* [#1984](https://github.com/sei-protocol/sei-chain/pull/1984) Client state pagniation by using filtered pagination
* [#1982](https://github.com/sei-protocol/sei-chain/pull/1982) Fix method handler crash due to nil min fee per gas
* [#1974](https://github.com/sei-protocol/sei-chain/pull/1974) Optimize getLogs with parallelization
* [#1971](https://github.com/sei-protocol/sei-chain/pull/1971) Remove tokenfactory config
* [#1970](https://github.com/sei-protocol/sei-chain/pull/1970) Add unbonding delegation query

sei-cosmos
* [#559](https://github.com/sei-protocol/sei-cosmos/pull/559) Fix state sync halt issue
* [#558](https://github.com/sei-protocol/sei-cosmos/pull/558) Integrate with MinTxsInBlock
* [#557](https://github.com/sei-protocol/sei-cosmos/pull/557) Fix seid rollback state mismatch error
* [#555](https://github.com/sei-protocol/sei-cosmos/pull/555) Set earliest version update
* [#552](https://github.com/sei-protocol/sei-cosmos/pull/552) Add confidential transfer constants

sei-tendermint
* [#252](https://github.com/sei-protocol/sei-tendermint/pull/252) Add new MinTxsInBlock consensus param

## v6.0.1
sei-chain
* [#1956](https://github.com/sei-protocol/sei-chain/pull/1956) Assign owner correctly when there are multiple transfers
* [#1955](https://github.com/sei-protocol/sei-chain/pull/1955) Add missing modules to migration and add command to export IAVL
* [#1954](https://github.com/sei-protocol/sei-chain/pull/1954) Enable Queries to IAVL for Non-Migrating Nodes
* [#1952](https://github.com/sei-protocol/sei-chain/pull/1952) Fix for failed txs in block
* [#1951](https://github.com/sei-protocol/sei-chain/pull/1951) Add max base fee as a param
* [#1949](https://github.com/sei-protocol/sei-chain/pull/1949) Be resilient to failing txs in debug trace block
* [#1941](https://github.com/sei-protocol/sei-chain/pull/1941) Fix eth_getLogs missing events early return
* [#1932](https://github.com/sei-protocol/sei-chain/pull/1932) Use owner event to populate ERC721 transfer topic
* [#1930](https://github.com/sei-protocol/sei-chain/pull/1930) Exclude cosmos txs from base fee calculation
* [#1926](https://github.com/sei-protocol/sei-chain/pull/1926) Refactor x/bank precompile to use dynamic gas
* [#1922](https://github.com/sei-protocol/sei-chain/pull/1922) Use msg server send in bank precompile
* [#1913](https://github.com/sei-protocol/sei-chain/pull/1913) Use tendermint store to get Tx hashes instead of storing explicitly
* [#1906](https://github.com/sei-protocol/sei-chain/pull/1906) Remove vue code
* [#1908](https://github.com/sei-protocol/sei-chain/pull/1908) QuerySmart to always use cached ctx


sei-cosmos
* [#551](https://github.com/sei-protocol/sei-cosmos/pull/551) Param change verification
* [#553](https://github.com/sei-protocol/sei-cosmos/pull/553) Remove unnecessary serving logs

sei-wasmd
* [#67](https://github.com/sei-protocol/sei-wasmd/pull/67) Emit CW721 token owner before transfer
* [#65](https://github.com/sei-protocol/sei-wasmd/pull/65) Add QuerySmartSafe in WasmViewKeeper


## v6.0.0
sei-chain
* [#1905](https://github.com/sei-protocol/sei-chain/pull/1905) Use limited wasm gas meter
* [#1889](https://github.com/sei-protocol/sei-chain/pull/1889) Fix amino registry for custom modules
* [#1888](https://github.com/sei-protocol/sei-chain/pull/1888) Set EIP-1559 default values
* [#1884](https://github.com/sei-protocol/sei-chain/pull/1884) Update gas tip cap param range
* [#1878](https://github.com/sei-protocol/sei-chain/pull/1878) Add endpoint to estimate gas after simulating calls

sei-cosmos
* [#547](https://github.com/sei-protocol/sei-cosmos/pull/547) Do not early return for validated tasks in synchronous mode
* [#544](https://github.com/sei-protocol/sei-cosmos/pull/544) Only apply DeliverTx hooks if there is no error
* [#538](https://github.com/sei-protocol/sei-cosmos/pull/538) Token allowlist feature

sei-tendermint
* [#248](https://github.com/sei-protocol/sei-tendermint/pull/248) Improve Peer Score algorithm
* [#245](https://github.com/sei-protocol/sei-tendermint/pull/245) Exclude unconditional peers when connection limit checking
* [#244](https://github.com/sei-protocol/sei-tendermint/pull/244) Add new config to speed up block sync

sei-db
* [#75](https://github.com/sei-protocol/sei-db/pull/75) Online archive node migration
 
## v5.9.0
sei-chain
* [#1867](https://github.com/sei-protocol/sei-chain/pull/1867) Add synthetic events in separate sei endpoints
* [#1861](https://github.com/sei-protocol/sei-chain/pull/1861) Revert showing wasm txs in EVM RPCs
* [#1857](https://github.com/sei-protocol/sei-chain/pull/1857) Fix events in 2-hop scenarios
* [#1856](https://github.com/sei-protocol/sei-chain/pull/1856) Add delegatecall flag to properly detect delegatecalls
* [#1850](https://github.com/sei-protocol/sei-chain/pull/1853) Fix websocket from_height
* [#1849](https://github.com/sei-protocol/sei-chain/pull/1849) Reduce block bloom storage
* [#1844](https://github.com/sei-protocol/sei-chain/pull/1844) Allowlist for token extensions

sei-iavl
*[#41](https://github.com/sei-protocol/sei-iavl/pull/41) Fix tree versions causing slow restart and OOM
## v5.8.0
sei-chain
* [#1840](https://github.com/sei-protocol/sei-chain/pull/1840) Add migration for new params
* [#1837](https://github.com/sei-protocol/sei-chain/pull/1837) Move token id from Data to Topic in ERC721 Event
* [#1836](https://github.com/sei-protocol/sei-chain/pull/1836) Properly handle gas in pointer precompile
* [#1835](https://github.com/sei-protocol/sei-chain/pull/1835) Check TX nonce before registering hook to bump nonce for failed tx
* [#1832](https://github.com/sei-protocol/sei-chain/pull/1832) Show CW transactions that have synthetic EVM events in eth_getBlock response
* [#1831](https://github.com/sei-protocol/sei-chain/pull/1831) Fork event manager when creating EVM snapshots
* [#1830](https://github.com/sei-protocol/sei-chain/pull/1830) Add wasm contract query gas limit
* [#1826](https://github.com/sei-protocol/sei-chain/pull/1826) limit MsgExec max nested level
* [#1821](https://github.com/sei-protocol/sei-chain/pull/1821) Add antehandler for EVM to check gas exceed limit or not
* [#1818](https://github.com/sei-protocol/sei-chain/pull/1818) Prevent ddos against associate msgs
* [#1816](https://github.com/sei-protocol/sei-chain/pull/1816) Actually remove dex module
* [#1813](https://github.com/sei-protocol/sei-chain/pull/1813) Tune Configs
* [#1812](https://github.com/sei-protocol/sei-chain/pull/1812) Evidence Max Bytes Update
* [#1785](https://github.com/sei-protocol/sei-chain/pull/1785) Allow CW->ERC pointers to be called through wasmd precompile
* [#1778](https://github.com/sei-protocol/sei-chain/pull/1778) Bump nonce even if tx fails

sei-cosmos
* [#535](https://github.com/sei-protocol/sei-cosmos/pull/535) init app earliest version correctly after state sync
* [#534](https://github.com/sei-protocol/sei-cosmos/pull/534) Stop executing the handler when proposal is submitted
* [#533](https://github.com/sei-protocol/sei-cosmos/pull/533) Delete kvstore specified in store upgrades
* [#532](https://github.com/sei-protocol/sei-cosmos/pull/532) Add max gas limit check in ante handler
* [#528](https://github.com/sei-protocol/sei-cosmos/pull/528) Add logs for snapshot export and impor

sei-wasmd
* [#63](https://github.com/sei-protocol/sei-wasmd/pull/63) Add CW dispatch call depth
* [#62](https://github.com/sei-protocol/sei-wasmd/pull/62) Patch Gas mispricing in CW VM

sei-tendermint
* [#242](https://github.com/sei-protocol/sei-tendermint/pull/242) Allow hyphen in event query

## v5.7.5
sei-chain
* [#1795](https://github.com/sei-protocol/sei-chain/pull/1795) Do not charge gas for feecollector address query
* [#1782](https://github.com/sei-protocol/sei-chain/pull/1782) Update excessBlobGas and BlobBaseFee to fix simulate evmcontext
* [#1741](https://github.com/sei-protocol/sei-chain/pull/1782) Update excessBlobGas and BlobBaseFee to fix simulate evmcontext

sei-cosmos
* [#530](https://github.com/sei-protocol/sei-cosmos/pull/530) Add EVMEntryViaWasmdPrecompile flag
* [#519](https://github.com/sei-protocol/sei-cosmos/pull/519) Genesis export stream
* [#529](https://github.com/sei-protocol/sei-cosmos/pull/529) Add DeliverTx callback
* [#528](https://github.com/sei-protocol/sei-cosmos/pull/528) Add logs for snapshot export and import

sei-wasmd
* [#58](https://github.com/sei-protocol/sei-wasmd/pull/58) Genesis Export OOM

sei-tendermint
* [#239](https://github.com/sei-protocol/sei-tendermint/pull/239) Use Marshal and UnmarshalJSON For HexBytes

## v5.7.1 & v5.7.2
sei-chain
* [#1779](https://github.com/sei-protocol/sei-chain/pull/1779) Fix subscribe logs empty params crash
* [#1783](https://github.com/sei-protocol/sei-chain/pull/1783) Add meaningful message for eth_call balance override overflow
* [#1783](https://github.com/sei-protocol/sei-chain/pull/1784) Fix log index on synthetic receipt
* [#1775](https://github.com/sei-protocol/sei-chain/pull/1775) Disallow sending to direct cast addr after association

sei-wasmd
* [#60](https://github.com/sei-protocol/sei-wasmd/pull/60) Query penalty fixes

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
* [#238](https://github.com/sei-protocol/sei-tendermint/pull/238) Make RPC timeout configurable
* [#219](https://github.com/sei-protocol/sei-tendermint/pull/219) Add metrics for mempool change


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
* [#505](https://github.com/sei-protocol/sei-cosmos/pull/505) Fix export genesis for historical height
* [#506](https://github.com/sei-protocol/sei-cosmos/pull/506) Allow reading pairs in changeset before flush

sei-wasmd
* [#50](https://github.com/sei-protocol/sei-wasmd/pull/50) Changes to fix runtime gas and add paramsKeeper to wasmKeeper for query gas multiplier

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
* [#211](https://github.com/sei-protocol/sei-tendermint/pull/211) Replay events during restart to avoid tx missing

sei-db:
* [#62](https://github.com/sei-protocol/sei-db/pull/62) Set CreateIfMissing to false when copyExisting

sei-wasmd:
* [#45](https://github.com/sei-protocol/sei-wasmd/pull/45) Update LimitSimulationGasDecorator with custom Gas Meter Setter
* [#44](https://github.com/sei-protocol/sei-wasmd/pull/44) Bump wasmvm to v1.5.2

## v3.8.0
sei-tendermint:
* [#209](https://github.com/sei-protocol/sei-tendermint/pull/209) Use write-lock in (*TxPriorityQueue).ReapMax funcs

sei-db:
* [#61](https://github.com/sei-protocol/sei-db/pull/61) LoadVersion should open DB with read only

sei-wasmd:
* [#41](https://github.com/sei-protocol/sei-wasmd/pull/42) Bump wasmd version

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
sei-tendermint:
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

sei-chain: 
https://github.com/sei-protocol/sei-chain/compare/2.0.44beta...2.0.45beta-release
* [#666](https://github.com/sei-protocol/sei-chain/pull/666) [DEX] remove BeginBlock/FinalizeBlock sudo hooks
* [#674](https://github.com/sei-protocol/sei-chain/pull/674) Longterm fix for max gas enforcement

sei-cosmos: 
https://github.com/sei-protocol/sei-cosmos/releases/tag/v0.2.14
* [#210](https://github.com/sei-protocol/sei-cosmos/pull/210) Add levelDB compaction goroutine

sei-tendermint: 
https://github.com/sei-protocol/sei-tendermint/releases/tag/v0.2.4
* [#110](https://github.com/sei-protocol/sei-tendermint/pull/110) Add more granular buckets for block interval
* [#111](https://github.com/sei-protocol/sei-tendermint/pull/111) Add unused prival pubKey back to node info - fix for IBC on full nodes
* [#113](https://github.com/sei-protocol/sei-tendermint/pull/113) Add metrics label for missing val power

## 2.0.44beta

sei-chain:
* [#658](https://github.com/sei-protocol/sei-chain/pull/658) Revert EventAttribute fields to byte array

sei-cosmos: 
https://github.com/sei-protocol/sei-cosmos/compare/sei-cosmos-2.0.42beta...v2.0.43beta-release
* [#204](https://github.com/sei-protocol/sei-cosmos/pull/204) IBC Compatibility Fix

sei-tendermint: 
https://github.com/sei-protocol/sei-tendermint/compare/2.0.42beta-release...2.0.43beta-release
* IBC Compatibility Fix, Bump default max gas limit, Add metrics & visibility for high block time

## 2.0.42beta

sei-chain:
* [#670](https://github.com/sei-protocol/sei-chain/pull/670) Add add-wasm-genesis-message to seid
* [#654](https://github.com/sei-protocol/sei-chain/pull/654) Improve endblock performance and fix trace

sei-cosmos: 
https://github.com/sei-protocol/sei-cosmos/compare/v0.2.8...v0.2.12
* improvements around monitoring for sei-cosmos, dont enforce gas limit on deliverTx, refactor slashing module


sei-tendermint:
* [#95](https://github.com/sei-protocol/sei-tendermint/pull/95) Patch forging empty merkle tree attack vector, set default max gas param to 6mil, log tunning for p2p

## 2.0.40beta - 2023-03-10

sei-chain:
* [#646](https://github.com/sei-protocol/sei-chain/pull/646) Optimizations for FinalizeBlock
* [#644](https://github.com/sei-protocol/sei-chain/pull/644) [Oak Audit] Add check for non-existent transaction
* [#647](https://github.com/sei-protocol/sei-chain/pull/647) Fixes to race conditions
* [#638](https://github.com/sei-protocol/sei-chain/pull/638) Emit Version Related Metrics
* [#636](https://github.com/sei-protocol/sei-chain/pull/636) Fix deadlock with upgrades
* [#635](https://github.com/sei-protocol/sei-chain/pull/635) Add event to dex messages

## 2.0.39beta - 2023-03-06

sei-chain:
* [#632](https://github.com/sei-protocol/sei-chain/pull/632) Bump Sei-tendermint to reduce log volume
* [#631](https://github.com/sei-protocol/sei-chain/pull/631) Nondeterminism deadlock fixes
* [#630](https://github.com/sei-protocol/sei-chain/pull/630) Mempool configs to avoid node slow down

## 2.0.38beta - 2023-03-04

sei-chain:
* [#623](https://github.com/sei-protocol/sei-chain/pull/623) [epoch] Add new epoch events by @udpatil
* [#624](https://github.com/sei-protocol/sei-chain/pull/624) [dex][mint] Add long messages for dex and mint by @udpatil 
* [#588](https://github.com/sei-protocol/sei-chain/pull/588) Send deposit funds in message server instead of EndBlock by @codchen 
* [#627](https://github.com/sei-protocol/sei-chain/pull/627) [oracle] Add slash window progress query by @udpatil
[label](x/oracle/README.md)
* [#625](https://github.com/sei-protocol/sei-chain/pull/625) Update contract rent deposit logic + add query endpoint by @LCyson

## 2.0.37beta - 2023-02-27

sei-chain:
* [#621](https://github.com/sei-protocol/sei-chain/pull/621) Add success count to the oracle query
* [#600](https://github.com/sei-protocol/sei-chain/pull/600) Add params to guard Nitro fraud challenge
* [#617](https://github.com/sei-protocol/sei-chain/pull/617) gracefully handle nil response for new provider
* [#619](https://github.com/sei-protocol/sei-chain/pull/619) Move store operations outside of iterator

sei-tendermint:
* [#73](https://github.com/sei-protocol/sei-tendermint/pull/73) reduce checktx log noise

## 2.0.36beta - 2023-02-27

sei-chain:
* [#603](https://github.com/sei-protocol/sei-chain/pull/603) Set mempool ttl
* [#612](https://github.com/sei-protocol/sei-chain/pull/612) Optimistic Processing should finish before main goroutine
* [#613](https://github.com/sei-protocol/sei-chain/pull/613) Incorporate IAVL change that removes mutex locking
* Various audit fixes
