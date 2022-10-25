# Changelog


## Current

October 261 2022

### FEATURES
- Gossip TxKey in proposal and allow validators to proactively create blocks from txs in mempool

### IMPROVEMENTS
- Increase block part size and delay fsync
- Increase WAL message size
- Add tracing

### BUG FIXES
- Fix open connection race conditions within p2p channels by waiting synchronously for descriptors to be registered before establishing peer connections
-
