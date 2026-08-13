# Changelog


## Current

June 13 2023

### FEATURES

- Gossip TxKey in proposal and allow validators to proactively create blocks from txs in mempool
- [Hard rollback by deleting app and block states](https://github.com/sei-protocol/sei-tendermint/pull/24)
    - Cherry picked from this [PR](https://github.com/tendermint/tendermint/pull/9261) with some modifications

### IMPROVEMENTS

- Increase block part size and delay fsync
- Increase WAL message size
- Add tracing
- [#148](https://github.com/sei-protocol/sei-tendermint/pull/148) [Backport](https://github.com/cometbft/cometbft/pull/241/files) add peer gossip sleep

### BUG FIXES

- Fix open connection race conditions within p2p channels by waiting synchronously for descriptors to be registered before establishing peer connections
