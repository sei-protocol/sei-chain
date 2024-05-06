# Sei V2 Config Migration Guide

## Intro
SeiV2 introduces a parallelized EVM along with a host of other features to the Sei blockchain
This doc is meant to help node operators with migrating their configs when running on SeiV2.


## Overview
We will focus particularly on the `config.toml` and `app.toml`.
For the `config.toml`, there are only minimal changes required adding in some params to the
mempool config. For the `app.toml`, we will make sure to enable seidb and add in a whole section for evm.

# Config.toml 

## Step 1: Add Extra Mempool Configurations

Add the following params to the `Mempool Configuration Option` section

```bash
pending-size = 5000

max-pending-txs-bytes = 1073741824

pending-ttl-duration = "0s"

pending-ttl-num-blocks = 0
```


## Full Example SeiV2 config.toml

```bash
# This is a TOML config file.
# For more information, see https://github.com/toml-lang/toml

# NOTE: Any path below can be absolute (e.g. "/var/myawesomeapp/data") or
# relative to the home directory (e.g. "data"). The home directory is
# "$HOME/.tendermint" by default, but could be changed via $TMHOME env variable
# or --home cmd flag.

#######################################################################
###                   Main Base Config Options                      ###
#######################################################################

# TCP or UNIX socket address of the ABCI application,
# or the name of an ABCI application compiled in with the Tendermint binary
proxy-app = "tcp://127.0.0.1:26658"

# A custom human readable name for this node
moniker = "demo"

# Mode of Node: full | validator | seed
# * validator node
#   - all reactors
#   - with priv_validator_key.json, priv_validator_state.json
# * full node
#   - all reactors
#   - No priv_validator_key.json, priv_validator_state.json
# * seed node
#   - only P2P, PEX Reactor
#   - No priv_validator_key.json, priv_validator_state.json
mode = "validator"

# Database backend: goleveldb | cleveldb | boltdb | rocksdb | badgerdb
# * goleveldb (github.com/syndtr/goleveldb - most popular implementation)
#   - pure go
#   - stable
# * cleveldb (uses levigo wrapper)
#   - fast
#   - requires gcc
#   - use cleveldb build tag (go build -tags cleveldb)
# * boltdb (uses etcd's fork of bolt - github.com/etcd-io/bbolt)
#   - EXPERIMENTAL
#   - may be faster is some use-cases (random reads - indexer)
#   - use boltdb build tag (go build -tags boltdb)
# * rocksdb (uses github.com/tecbot/gorocksdb)
#   - EXPERIMENTAL
#   - requires gcc
#   - use rocksdb build tag (go build -tags rocksdb)
# * badgerdb (uses github.com/dgraph-io/badger)
#   - EXPERIMENTAL
#   - use badgerdb build tag (go build -tags badgerdb)
db-backend = "goleveldb"

# Database directory
db-dir = "data"

# Output level for logging, including package level options
log-level = "info"

# Output format: 'plain' (colored text) or 'json'
log-format = "plain"

##### additional base config options #####

# Path to the JSON file containing the initial validator set and other meta data
genesis-file = "config/genesis.json"

# Path to the JSON file containing the private key to use for node authentication in the p2p protocol
node-key-file = "config/node_key.json"

# Mechanism to connect to the ABCI application: socket | grpc
abci = "socket"

# If true, query the ABCI app on connecting to a new peer
# so the app can decide if we should keep the connection or not
filter-peers = false


#######################################################
###       Priv Validator Configuration              ###
#######################################################
[priv-validator]

# Path to the JSON file containing the private key to use as a validator in the consensus protocol
key-file = "config/priv_validator_key.json"

# Path to the JSON file containing the last sign state of a validator
state-file = "data/priv_validator_state.json"

# TCP or UNIX socket address for Tendermint to listen on for
# connections from an external PrivValidator process
# when the listenAddr is prefixed with grpc instead of tcp it will use the gRPC Client
laddr = ""

# Path to the client certificate generated while creating needed files for secure connection.
# If a remote validator address is provided but no certificate, the connection will be insecure
client-certificate-file = ""

# Client key generated while creating certificates for secure connection
client-key-file = ""

# Path to the Root Certificate Authority used to sign both client and server certificates
root-ca-file = ""


#######################################################################
###                 Advanced Configuration Options                  ###
#######################################################################

#######################################################
###       RPC Server Configuration Options          ###
#######################################################
[rpc]

# TCP or UNIX socket address for the RPC server to listen on
laddr = "tcp://127.0.0.1:26657"

# A list of origins a cross-domain request can be executed from
# Default value '[]' disables cors support
# Use '["*"]' to allow any origin
cors-allowed-origins = []

# A list of methods the client is allowed to use with cross-domain requests
cors-allowed-methods = ["HEAD", "GET", "POST", ]

# A list of non simple headers the client is allowed to use with cross-domain requests
cors-allowed-headers = ["Origin", "Accept", "Content-Type", "X-Requested-With", "X-Server-Time", ]

# Activate unsafe RPC commands like /dial-seeds and /unsafe-flush-mempool
unsafe = false

# Maximum number of simultaneous connections (including WebSocket).
# If you want to accept a larger number than the default, make sure
# you increase your OS limits.
# 0 - unlimited.
# Should be < {ulimit -Sn} - {MaxNumInboundPeers} - {MaxNumOutboundPeers} - {N of wal, db and other open files}
# 1024 - 40 - 10 - 50 = 924 = ~900
max-open-connections = 900

# Maximum number of unique clientIDs that can /subscribe
# If you're using /broadcast_tx_commit, set to the estimated maximum number
# of broadcast_tx_commit calls per block.
max-subscription-clients = 100

# Maximum number of unique queries a given client can /subscribe to
# If you're using a Local RPC client and /broadcast_tx_commit, set this
# to the estimated maximum number of broadcast_tx_commit calls per block.
max-subscriptions-per-client = 5

# If true, disable the websocket interface to the RPC service.  This has
# the effect of disabling the /subscribe, /unsubscribe, and /unsubscribe_all
# methods for event subscription.
#
# EXPERIMENTAL: This setting will be removed in Tendermint v0.37.
experimental-disable-websocket = false

# The time window size for the event log. All events up to this long before
# the latest (up to EventLogMaxItems) will be available for subscribers to
# fetch via the /events method.  If 0 (the default) the event log and the
# /events RPC method are disabled.
event-log-window-size = "30s"

# The maxiumum number of events that may be retained by the event log.  If
# this value is 0, no upper limit is set. Otherwise, items in excess of
# this number will be discarded from the event log.
#
# Warning: This setting is a safety valve. Setting it too low may cause
# subscribers to miss events.  Try to choose a value higher than the
# maximum worst-case expected event load within the chosen window size in
# ordinary operation.
#
# For example, if the window size is 10 minutes and the node typically
# averages 1000 events per ten minutes, but with occasional known spikes of
# up to 2000, choose a value > 2000.
event-log-max-items = 0

# How long to wait for a tx to be committed during /broadcast_tx_commit.
# WARNING: Using a value larger than 10s will result in increasing the
# global HTTP write timeout, which applies to all connections and endpoints.
# See https://github.com/tendermint/tendermint/issues/3435
timeout-broadcast-tx-commit = "10s"

# Maximum size of request body, in bytes
max-body-bytes = 1000000

# Maximum size of request header, in bytes
max-header-bytes = 1048576

# The path to a file containing certificate that is used to create the HTTPS server.
# Might be either absolute path or path related to Tendermint's config directory.
# If the certificate is signed by a certificate authority,
# the certFile should be the concatenation of the server's certificate, any intermediates,
# and the CA's certificate.
# NOTE: both tls-cert-file and tls-key-file must be present for Tendermint to create HTTPS server.
# Otherwise, HTTP server is run.
tls-cert-file = ""

# The path to a file containing matching private key that is used to create the HTTPS server.
# Might be either absolute path or path related to Tendermint's config directory.
# NOTE: both tls-cert-file and tls-key-file must be present for Tendermint to create HTTPS server.
# Otherwise, HTTP server is run.
tls-key-file = ""

# pprof listen address (https://golang.org/pkg/net/http/pprof)
pprof-laddr = "localhost:6060"

#######################################################
###           P2P Configuration Options             ###
#######################################################
[p2p]

# Select the p2p internal queue
queue-type = "simple-priority"

# Address to listen for incoming connections
laddr = "tcp://0.0.0.0:26656"

# Address to advertise to peers for them to dial
# If empty, will use the same port as the laddr,
# and will introspect on the listener or use UPnP
# to figure out the address. ip and port are required
# example: 159.89.10.97:26656
external-address = ""

# Comma separated list of peers to be added to the peer store
# on startup. Either BootstrapPeers or PersistentPeers are
# needed for peer discovery
bootstrap-peers = ""

# Comma separated list of nodes to keep persistent connections to
persistent-peers = ""

# UPNP port forwarding
upnp = false

# Maximum number of connections (inbound and outbound).
max-connections = 200

# Rate limits the number of incoming connection attempts per IP address.
max-incoming-connection-attempts = 100

# Set true to enable the peer-exchange reactor
pex = true

# Comma separated list of peer IDs to keep private (will not be gossiped to other peers)
# Warning: IPs will be exposed at /net_info, for more information https://github.com/tendermint/tendermint/issues/3055
private-peer-ids = ""

# Toggle to disable guard against peers connecting from the same ip.
allow-duplicate-ip = false

# Peer connection configuration.
handshake-timeout = "20s"
dial-timeout = "3s"

# Time to wait before flushing messages out on the connection
# TODO: Remove once MConnConnection is removed.
flush-throttle-timeout = "10ms"

# Maximum size of a message packet payload, in bytes
# TODO: Remove once MConnConnection is removed.
max-packet-msg-payload-size = 1000000

# Rate at which packets can be sent, in bytes/second
# TODO: Remove once MConnConnection is removed.
send-rate = 20480000

# Rate at which packets can be received, in bytes/second
# TODO: Remove once MConnConnection is removed.
recv-rate = 20480000

# List of node IDs, to which a connection will be (re)established ignoring any existing limits
unconditional-peer-ids = ""


#######################################################
###          Mempool Configuration Option          ###
#######################################################
[mempool]

# recheck has been moved from a config option to a global
# consensus param in v0.36
# See https://github.com/tendermint/tendermint/issues/8244 for more information.

# Set true to broadcast transactions in the mempool to other nodes
broadcast = true

# Maximum number of transactions in the mempool
size = 1000

# Limit the total size of all txs in the mempool.
# This only accounts for raw transactions (e.g. given 1MB transactions and
# max-txs-bytes=5MB, mempool will only accept 5 transactions).
max-txs-bytes = 10737418240

# Size of the cache (used to filter transactions we saw earlier) in transactions
cache-size = 10000

# Do not remove invalid transactions from the cache (default: false)
# Set to true if it's not possible for any invalid transaction to become valid
# again in the future.
keep-invalid-txs-in-cache = false

# Maximum size of a single transaction.
# NOTE: the max size of a tx transmitted over the network is {max-tx-bytes}.
max-tx-bytes = 2048576

# Maximum size of a batch of transactions to send to a peer
# Including space needed by encoding (one varint per transaction).
# XXX: Unused due to https://github.com/tendermint/tendermint/issues/5796
max-batch-bytes = 0

# ttl-duration, if non-zero, defines the maximum amount of time a transaction
# can exist for in the mempool.
#
# Note, if ttl-num-blocks is also defined, a transaction will be removed if it
# has existed in the mempool at least ttl-num-blocks number of blocks or if it's
# insertion time into the mempool is beyond ttl-duration.
ttl-duration = "30s"

# ttl-num-blocks, if non-zero, defines the maximum number of blocks a transaction
# can exist for in the mempool.
#
# Note, if ttl-duration is also defined, a transaction will be removed if it
# has existed in the mempool at least ttl-num-blocks number of blocks or if
# it's insertion time into the mempool is beyond ttl-duration.
ttl-num-blocks = 100

tx-notify-threshold = 0

check-tx-error-blacklist-enabled = false

check-tx-error-threshold = 0

pending-size = 5000

max-pending-txs-bytes = 1073741824

pending-ttl-duration = "0s"

pending-ttl-num-blocks = 0

#######################################################
###         State Sync Configuration Options        ###
#######################################################
[statesync]
# State sync rapidly bootstraps a new node by discovering, fetching, and restoring a state machine
# snapshot from peers instead of fetching and replaying historical blocks. Requires some peers in
# the network to take and serve state machine snapshots. State sync is not attempted if the node
# has any local state (LastBlockHeight > 0). The node will have a truncated block history,
# starting from the height of the snapshot.
enable = false

# State sync uses light client verification to verify state. This can be done either through the
# P2P layer or RPC layer. Set this to true to use the P2P layer. If false (default), RPC layer
# will be used.
use-p2p = false

# If using RPC, at least two addresses need to be provided. They should be compatible with net.Dial,
# for example: "host.example.com:2125"
rpc-servers = ""

# The hash and height of a trusted block. Must be within the trust-period.
trust-height = 0
trust-hash = ""

# The trust period should be set so that Tendermint can detect and gossip misbehavior before
# it is considered expired. For chains based on the Cosmos SDK, one day less than the unbonding
# period should suffice.
trust-period = "168h0m0s"

# Backfill sequentially fetches after state sync completes, verifies and stores light blocks in reverse order.
# backfill-blocks means it will keep reverse fetching up to backfill-blocks number of blocks behind state sync position
# backfill-duration means it will keep fetching up to backfill-duration old time
# The actual backfill process will take at backfill-blocks as priority:
# - If backfill-blocks is set, use backfill-blocks to backfill
# - If backfill-blocks is not set to be greater than 0, use backfill-duration to backfill
backfill-blocks = "0"
backfill-duration = "0s"

# Time to spend discovering snapshots before initiating a restore.
discovery-time = "15s"

# Temporary directory for state sync snapshot chunks, defaults to os.TempDir().
# The synchronizer will create a new, randomly named directory within this directory
# and remove it when the sync is complete.
temp-dir = ""

# The timeout duration before re-requesting a chunk, possibly from a different
# peer (default: 15 seconds).
chunk-request-timeout = "15s"

# The number of concurrent chunk and block fetchers to run (default: 4).
fetchers = "4"

verify-light-block-timeout = "1m0s"

blacklist-ttl = "5m0s"

#######################################################
###         Consensus Configuration Options         ###
#######################################################
[consensus]

wal-file = "data/cs.wal/wal"

# How many blocks to look back to check existence of the node's consensus votes before joining consensus
# When non-zero, the node will panic upon restart
# if the same consensus key was used to sign {double-sign-check-height} last blocks.
# So, validators should stop the state machine, wait for some blocks, and then restart the state machine to avoid panic.
double-sign-check-height = 0

# EmptyBlocks mode and possible interval between empty blocks
create-empty-blocks = true
create-empty-blocks-interval = "0s"

# Only gossip hashes, not the actual data
gossip-tx-key-only = "true"

# Reactor sleep duration parameters
peer-gossip-sleep-duration = "100ms"
peer-query-maj23-sleep-duration = "2s"

### Unsafe Timeout Overrides ###

# These fields provide temporary overrides for the Timeout consensus parameters.
# Use of these parameters is strongly discouraged. Using these parameters may have serious
# liveness implications for the validator and for the chain.
#
# These fields will be removed from the configuration file in the v0.37 release of Tendermint.
# For additional information, see ADR-74:
# https://github.com/tendermint/tendermint/blob/master/docs/architecture/adr-074-timeout-params.md

# This field provides an unsafe override of the Propose timeout consensus parameter.
# This field configures how long the consensus engine will wait for a proposal block before prevoting nil.
# If this field is set to a value greater than 0, it will take effect.
unsafe-propose-timeout-override = "2s"

# This field provides an unsafe override of the ProposeDelta timeout consensus parameter.
# This field configures how much the propose timeout increases with each round.
# If this field is set to a value greater than 0, it will take effect.
unsafe-propose-timeout-delta-override = "2s"

# This field provides an unsafe override of the Vote timeout consensus parameter.
# This field configures how long the consensus engine will wait after
# receiving +2/3 votes in a round.
# If this field is set to a value greater than 0, it will take effect.
unsafe-vote-timeout-override = "2s"

# This field provides an unsafe override of the VoteDelta timeout consensus parameter.
# This field configures how much the vote timeout increases with each round.
# If this field is set to a value greater than 0, it will take effect.
unsafe-vote-timeout-delta-override = "2s"

# This field provides an unsafe override of the Commit timeout consensus parameter.
# This field configures how long the consensus engine will wait after receiving
# +2/3 precommits before beginning the next height.
# If this field is set to a value greater than 0, it will take effect.
unsafe-commit-timeout-override = "2s"

# This field provides an unsafe override of the BypassCommitTimeout consensus parameter.
# This field configures if the consensus engine will wait for the full Commit timeout
# before proceeding to the next height.
# If this field is set to true, the consensus engine will proceed to the next height
# as soon as the node has gathered votes from all of the validators on the network.
# unsafe-bypass-commit-timeout-override = <nil>

#######################################################
###   Transaction Indexer Configuration Options     ###
#######################################################
[tx-index]

# The backend database list to back the indexer.
# If list contains "null" or "", meaning no indexer service will be used.
#
# The application will set which txs to index. In some cases a node operator will be able
# to decide which txs to index based on configuration set in the application.
#
# Options:
#   1) "null" (default) - no indexer services.
#   2) "kv" - a simple indexer backed by key-value storage (see DBBackend)
#   3) "psql" - the indexer services backed by PostgreSQL.
# When "kv" or "psql" is chosen "tx.height" and "tx.hash" will always be indexed.
indexer = ["kv"]

# The PostgreSQL connection configuration, the connection format:
#   postgresql://<user>:<password>@<host>:<port>/<db>?<opts>
psql-conn = ""

#######################################################
###       Instrumentation Configuration Options     ###
#######################################################
[instrumentation]

# When true, Prometheus metrics are served under /metrics on
# PrometheusListenAddr.
# Check out the documentation for the list of available metrics.
prometheus = true

# Address to listen for Prometheus collector(s) connections
prometheus-listen-addr = ":26660"

# Maximum number of simultaneous connections.
# If you want to accept a larger number than the default, make sure
# you increase your OS limits.
# 0 - unlimited.
max-open-connections = 3

# Instrumentation namespace
namespace = "tendermint"

#######################################################
###       SelfRemediation Configuration Options     ###
#######################################################
[self-remediation]

# If the node has no p2p peers available then trigger a restart
# Set to 0 to disable
p2p-no-peers-available-window-seconds = 0

# If node has no peers for statesync after a period of time then restart
# Set to 0 to disable
statesync-no-peers-available-window-seconds = 0

# Threshold for how far back the node can be behind the current block height before triggering a restart
# Set to 0 to disable
blocks-behind-threshold = 0

# How often to check if node is behind
blocks-behind-check-interval = 60

# Cooldown between each restart
restart-cooldown-seconds = 600

[db-sync]
db-sync-enable = "false"
snapshot-interval = "0"
snapshot-directory = ""
snapshot-worker-count = "16"
timeout-in-seconds = "1200"
no-file-sleep-in-seconds = "1"
file-worker-count = "32"
file-worker-timeout = "30"
trust-height = "0"
trust-hash = ""
trust-period = "24h0m0s"
verify-light-block-timeout = "1m0s"
blacklist-ttl = "5m0s"
```

# App.toml 

## Step 1: Enable OCC

Alter the following in the `Base Configuration` section to enable OCC:

```bash
concurrency-workers = 500
occ-enabled = true
```

## Step 2: Enable SeiDB

Alter the following:

```bash
ss-enable = true
ss-db-directory = "
```

This is just for validator nodes. Reference `seidb_migration.md` for more information on enabling state store for rpc nodes.

## Step 3: Add EVM Section

This is the largest edit involved for the configs. Add the following whole sections for `evm`, `eth_replay`, `eth_blocktest`
and `evm_query` at the bottom of the `app.toml`:

```bash
[evm]
# controls whether an HTTP EVM server is enabled
http_enabled = true
http_port = 8545

# controls whether a websocket server is enabled
ws_enabled = true
ws_port = 8546

# ReadTimeout is the maximum duration for reading the entire
# request, including the body.
# Because ReadTimeout does not let Handlers make per-request
# decisions on each request body's acceptable deadline or
# upload rate, most users will prefer to use
# ReadHeaderTimeout. It is valid to use them both.
read_timeout = "30s"

# ReadHeaderTimeout is the amount of time allowed to read
# request headers. The connection's read deadline is reset
# after reading the headers and the Handler can decide what
# is considered too slow for the body. If ReadHeaderTimeout
# is zero, the value of ReadTimeout is used. If both are
# zero, there is no timeout.
read_header_timeout = "30s"

# WriteTimeout is the maximum duration before timing out
# writes of the response. It is reset whenever a new
# request's header is read. Like ReadTimeout, it does not
# let Handlers make decisions on a per-request basis.
write_timeout = "30s"

# IdleTimeout is the maximum amount of time to wait for the
# next request when keep-alives are enabled. If IdleTimeout
# is zero, the value of ReadTimeout is used. If both are
# zero, ReadHeaderTimeout is used.
idle_timeout = "2m0s"

# Maximum gas limit for simulation
simulation_gas_limit = 10000000

# Timeout for EVM call in simulation
simulation_evm_timeout = "1m0s"

# list of CORS allowed origins, separated by comma
cors_origins = "*"

# list of WS origins, separated by comma
ws_origins = "*"

# timeout for filters
filter_timeout = "2m0s"

# checkTx timeout for sig verify
checktx_timeout = "5s"

# controls whether to have txns go through one by one
slow = false

# Deny list defines list of methods that EVM RPC should fail fast
deny_list = []

# max number of logs returned if block range is open-ended
max_log_no_block = 10000

# max number of blocks to query logs for
max_blocks_for_log = 2000

[eth_replay]
eth_replay_enabled = false
eth_rpc = "http://44.234.105.54:18545"
eth_data_dir = "/root/.ethereum/chaindata"
eth_replay_contract_state_checks = false

[eth_blocktest]
eth_blocktest_enabled = false
eth_blocktest_test_data_path = "~/testdata/"

[evm_query]
evm_query_gas_limit = 300000
```

## Full Example SeiV2 app.toml

```bash
# This is a TOML config file.
# For more information, see https://github.com/toml-lang/toml

###############################################################################
###                           Base Configuration                            ###
###############################################################################

# The minimum gas prices a validator is willing to accept for processing a
# transaction. A transaction's fees must meet the minimum of any denomination
# specified in this config (e.g. 0.25token1;0.0001token2).
minimum-gas-prices = "0.02usei"

# Pruning Strategies:
# - default: Keep the recent 362880 blocks and prune is triggered every 10 blocks
# - nothing: all historic states will be saved, nothing will be deleted (i.e. archiving node)
# - everything: all saved states will be deleted, storing only the recent 2 blocks; pruning at every block
# - custom: allow pruning options to be manually specified through 'pruning-keep-recent' and 'pruning-interval'
# Pruning strategy is completely ignored when seidb is enabled
pruning = "default"

# These are applied if and only if the pruning strategy is custom, and seidb is not enabled
pruning-keep-recent = "0"
pruning-keep-every = "0"
pruning-interval = "3467"

# HaltHeight contains a non-zero block height at which a node will gracefully
# halt and shutdown that can be used to assist upgrades and testing.
#
# Note: Commitment of state will be attempted on the corresponding block.
halt-height = 0

# HaltTime contains a non-zero minimum block time (in Unix seconds) at which
# a node will gracefully halt and shutdown that can be used to assist upgrades
# and testing.
#
# Note: Commitment of state will be attempted on the corresponding block.
halt-time = 0

# MinRetainBlocks defines the minimum block height offset from the current
# block being committed, such that all blocks past this offset are pruned
# from Tendermint. It is used as part of the process of determining the
# ResponseCommit.RetainHeight value during ABCI Commit. A value of 0 indicates
# that no blocks should be pruned.
#
# This configuration value is only responsible for pruning Tendermint blocks.
# It has no bearing on application state pruning which is determined by the
# "pruning-*" configurations.
#
# Note: Tendermint block pruning is dependant on this parameter in conunction
# with the unbonding (safety threshold) period, state pruning and state sync
# snapshot parameters to determine the correct minimum value of
# ResponseCommit.RetainHeight.
min-retain-blocks = 0

# InterBlockCache enables inter-block caching.
inter-block-cache = true

# IndexEvents defines the set of events in the form {eventType}.{attributeKey},
# which informs Tendermint what to index. If empty, all events will be indexed.
#
# Example:
# ["message.sender", "message.recipient"]
index-events = []

# IavlCacheSize set the size of the iavl tree cache.
# Default cache size is 50mb.
iavl-cache-size = 781250

# IAVLDisableFastNode enables or disables the fast node feature of IAVL.
# Default is true.
iavl-disable-fastnode = true

# CompactionInterval sets (in seconds) the interval between forced levelDB
# compaction. A value of 0 means no forced levelDB.
# Default is 0.
compaction-interval = 0

# deprecated
no-versioning = false

# Whether to store orphan data (to-be-deleted data pointers) outside the main
# application LevelDB
separate-orphan-storage = false

# if separate-orphan-storage is true, how many versions of orphan data to keep
separate-orphan-versions-to-keep = 0

# if separate-orphan-storage is true, how many orphans to store in each file
num-orphan-per-file = 0

# if separate-orphan-storage is true, where to store orphan data
orphan-dir = ""

# concurrency-workers defines how many workers to run for concurrent transaction execution
concurrency-workers = 500

# occ-enabled defines whether OCC is enabled or not for transaction execution
occ-enabled = true

###############################################################################
###                         Telemetry Configuration                         ###
###############################################################################

[telemetry]

# Prefixed with keys to separate services.
service-name = ""

# Enabled enables the application telemetry functionality. When enabled,
# an in-memory sink is also enabled by default. Operators may also enabled
# other sinks such as Prometheus.
enabled = true

# Enable prefixing gauge values with hostname.
enable-hostname = false

# Enable adding hostname to labels.
enable-hostname-label = false

# Enable adding service to labels.
enable-service-label = false

# PrometheusRetentionTime, when positive, enables a Prometheus metrics sink.
prometheus-retention-time = 60

# GlobalLabels defines a global set of name/value label tuples applied to all
# metrics emitted using the wrapper functions defined in telemetry package.
#
# Example:
# [["chain_id", "cosmoshub-1"]]
global-labels = [
]

###############################################################################
###                           API Configuration                             ###
###############################################################################

[api]

# Enable defines if the API server should be enabled.
enable = true

# Swagger defines if swagger documentation should automatically be registered.
swagger = false

# Address defines the API server to listen on.
address = "tcp://0.0.0.0:1317"

# MaxOpenConnections defines the number of maximum open connections.
max-open-connections = 1000

# RPCReadTimeout defines the Tendermint RPC read timeout (in seconds).
rpc-read-timeout = 10

# RPCWriteTimeout defines the Tendermint RPC write timeout (in seconds).
rpc-write-timeout = 0

# RPCMaxBodyBytes defines the Tendermint maximum response body (in bytes).
rpc-max-body-bytes = 1000000

# EnableUnsafeCORS defines if CORS should be enabled (unsafe - use it at your own risk).
enabled-unsafe-cors = false

###############################################################################
###                           Rosetta Configuration                         ###
###############################################################################

[rosetta]

# Enable defines if the Rosetta API server should be enabled.
enable = false

# Address defines the Rosetta API server to listen on.
address = ":8080"

# Network defines the name of the blockchain that will be returned by Rosetta.
blockchain = "app"

# Network defines the name of the network that will be returned by Rosetta.
network = "network"

# Retries defines the number of retries when connecting to the node before failing.
retries = 3

# Offline defines if Rosetta server should run in offline mode.
offline = false

###############################################################################
###                           gRPC Configuration                            ###
###############################################################################

[grpc]

# Enable defines if the gRPC server should be enabled.
enable = true

# Address defines the gRPC server address to bind to.
address = "0.0.0.0:9090"

###############################################################################
###                        gRPC Web Configuration                           ###
###############################################################################

[grpc-web]

# GRPCWebEnable defines if the gRPC-web should be enabled.
# NOTE: gRPC must also be enabled, otherwise, this configuration is a no-op.
enable = true

# Address defines the gRPC-web server address to bind to.
address = "0.0.0.0:9091"

# EnableUnsafeCORS defines if CORS should be enabled (unsafe - use it at your own risk).
enable-unsafe-cors = false

###############################################################################
###                        State Sync Configuration                         ###
###############################################################################

# State sync snapshots allow other nodes to rapidly join the network without replaying historical
# blocks, instead downloading and applying a snapshot of the application state at a given height.
[state-sync]

# snapshot-interval specifies the block interval at which local state sync snapshots are
# taken (0 to disable). Must be a multiple of pruning-keep-every.
snapshot-interval = 0

# snapshot-keep-recent specifies the number of recent snapshots to keep and serve (0 to keep all).
snapshot-keep-recent = 2

# snapshot-directory sets the directory for where state sync snapshots are persisted.
# default is emtpy which will then store under the app home directory same as before.
snapshot-directory = ""


#############################################################################
###                             SeiDB Configuration                       ###
#############################################################################

[state-commit]
# Enable defines if the SeiDB should be enabled to override existing IAVL db backend.
sc-enable = true

# Defines the SC store directory, if not explicitly set, default to application home directory
sc-directory = ""

# ZeroCopy defines if memiavl should return slices pointing to mmap-ed buffers directly (zero-copy),
# the zero-copied slices must not be retained beyond current block's execution.
# the sdk address cache will be disabled if zero-copy is enabled.
sc-zero-copy = false

# AsyncCommitBuffer defines the size of asynchronous commit queue, this greatly improve block catching-up
# performance, setting to 0 means synchronous commit.
sc-async-commit-buffer = 100

# KeepRecent defines how many state-commit snapshots (besides the latest one) to keep
# defaults to 1 to make sure ibc relayers work.
sc-keep-recent = 1

# SnapshotInterval defines the block interval the snapshot is taken, default to 10000 blocks.
sc-snapshot-interval = 10000

# SnapshotWriterLimit defines the max concurrency for taking commit store snapshot
sc-snapshot-writer-limit = 0

[state-store]
# Enable defines whether the state-store should be enabled for storing historical data.
# Supporting historical queries or exporting state snapshot requires setting this to true
# This config only take effect when SeiDB is enabled (sc-enable = true
ss-enable = true

# Defines the directory to store the state store db files
# If not explicitly set, default to application home directory
ss-db-directory = ""

# DBBackend defines the backend database used for state-store.
# Supported backends: pebbledb, rocksdb
# defaults to pebbledb (recommended)
ss-backend = "pebbledb"

# AsyncWriteBuffer defines the async queue length for commits to be applied to State Store
# Set <= 0 for synchronous writes, which means commits also need to wait for data to be persisted in State Store.
# defaults to 100 for asynchronous writes
ss-async-write-buffer = 100

# KeepRecent defines the number of versions to keep in state store
# Setting it to 0 means keep everything
# Default to keep the last 100,000 blocks
ss-keep-recent = 100000

# PruneInterval defines the minimum interval in seconds + some random delay to trigger SS pruning.
# It is recommended to trigger pruning less frequently with a large interval.
# default to 600 seconds
ss-prune-interval = 600

# ImportNumWorkers defines the concurrency for state sync import
# defaults to 1
ss-import-num-workers = 1


[wasm]
# This is the maximum sdk gas (wasm and storage) that we allow for any x/wasm "smart" queries
query_gas_limit = 300000
# This is the number of wasm vm instances we keep cached in memory for speed-up
# Warning: this is currently unstable and may lead to crashes, best to keep for 0 unless testing locally
lru_size = 0

[evm]
# controls whether an HTTP EVM server is enabled
http_enabled = true
http_port = 8545

# controls whether a websocket server is enabled
ws_enabled = true
ws_port = 8546

# ReadTimeout is the maximum duration for reading the entire
# request, including the body.
# Because ReadTimeout does not let Handlers make per-request
# decisions on each request body's acceptable deadline or
# upload rate, most users will prefer to use
# ReadHeaderTimeout. It is valid to use them both.
read_timeout = "30s"

# ReadHeaderTimeout is the amount of time allowed to read
# request headers. The connection's read deadline is reset
# after reading the headers and the Handler can decide what
# is considered too slow for the body. If ReadHeaderTimeout
# is zero, the value of ReadTimeout is used. If both are
# zero, there is no timeout.
read_header_timeout = "30s"

# WriteTimeout is the maximum duration before timing out
# writes of the response. It is reset whenever a new
# request's header is read. Like ReadTimeout, it does not
# let Handlers make decisions on a per-request basis.
write_timeout = "30s"

# IdleTimeout is the maximum amount of time to wait for the
# next request when keep-alives are enabled. If IdleTimeout
# is zero, the value of ReadTimeout is used. If both are
# zero, ReadHeaderTimeout is used.
idle_timeout = "2m0s"

# Maximum gas limit for simulation
simulation_gas_limit = 10000000

# Timeout for EVM call in simulation
simulation_evm_timeout = "1m0s"

# list of CORS allowed origins, separated by comma
cors_origins = "*"

# list of WS origins, separated by comma
ws_origins = "*"

# timeout for filters
filter_timeout = "2m0s"

# checkTx timeout for sig verify
checktx_timeout = "5s"

# controls whether to have txns go through one by one
slow = false

# Deny list defines list of methods that EVM RPC should fail fast
deny_list = []

# max number of logs returned if block range is open-ended
max_log_no_block = 10000

# max number of blocks to query logs for
max_blocks_for_log = 2000

[eth_replay]
eth_replay_enabled = false
eth_rpc = "http://44.234.105.54:18545"
eth_data_dir = "/root/.ethereum/chaindata"
eth_replay_contract_state_checks = false

[eth_blocktest]
eth_blocktest_enabled = false
eth_blocktest_test_data_path = "~/testdata/"

[evm_query]
evm_query_gas_limit = 300000
```
