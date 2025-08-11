# SeiDB
SeiDB is the next-gen on-chain database which is designed to replace the [IAVL Store](https://github.com/cosmos/iavl) of Cosmos based chain.
The goal of SeiDB is to improve the overall data access performance and prevent state bloat issues.

## Key Wins of SeiDB
- Reduces active chain state size by 60%
- Reduces historical data growth rate by ~90%
- Improves state sync times by 1200% and block sync time by 2x
- Enables 287x improvement in block commit times
- Provides faster state access and state commit resulting in overall TPS improved by 2x
- All while ensuring Sei archive nodes are able to achieve the same high performance as any full node.

## Architecture
The original idea of SeiDB came from [Cosmos StoreV2 ADR](https://docs.cosmos.network/main/build/architecture/adr-065-store-v2), in which the high level idea is that instead of
using a single giant database to store both latest and historical data, SeiDb split into 2 separate storage layers:
- State Commitment (SC Store): This stores the active chain state data in a memory mapped Merkle tree, providing fast transaction state access and Merkle hashing
- State Store (SS Store): Specifically designed and tuned for full nodes and archive nodes to serve historical queries

### Advantages
- SC and SS backends becomes be easily swappable
- SS store only need to store raw key/values to save disk space and reduce write amplifications
- The delineation of active state and historical data massively improves performance for all node operators in the Sei ecosystem.

### Trade-offs
- Not supporting historical proofs for all historical blocks
- Lacking integrity and correctness validation for historical data

## State Commitment (SC) Layer
Responsibility of SC layer:
- Provide root app hash for each new block
- Provide data access layer for transaction execution
- Provide API to import/export chain state for state sync requirements
- Provide historical proofs for heights not pruned yet

SeiDB currently forks [MemIAVL](https://github.com/crypto-org-chain/cronos/tree/main/memiavl) and uses that as its SC layer implementation.

In order to keep backward compatible with existing Cosmos chains, MemIAVL uses the same data structure (Merkelized AVL tree) as Cosmos SDK.

However, the biggest difference is that MemIAVL represent IAVL tree with memory-mapped flat files instead of persisting the whole tree as key/values in the database engine.

## State Store (SS) Layer
The goal of SS store is to provide a modular storage backend which supports multiple implementations,
to facilitate storing versioned raw key/value pairs in a fast embedded database.

The responsibility and functions of SS include the following:
- Provided fast and efficient queries for versioned raw key/value pairs
- Provide versioned CRUD operations
- Provide versioned batching functionality
- Provide versioned iteration functionality
- Provide pruning functionality

### DB Backend
Extensive benchmarking was conducted with Sei chain key-value data measuring random write, read and forward / back iteration performance for LevelDB, RocksDB, PebbleDB, and SQLite.
Our benchmarks shows that PebbleDB performs the best among these database backends, which is why SeiDB SS store use PebbleDB as the recommended default backend.
