/*
Package blocksync implements the blocksync protocol used for serving block
requests and catching up to the network head. This mechanism was formerly known
as fast-sync.

In order for a full node to successfully participate in consensus, it must have
the latest view of state. The blocksync protocol is a mechanism in which peers
may exchange and gossip entire blocks with one another, in a request/response
type model, until they've successfully synced to the latest head block. Once
succussfully synced, the full node can switch to an active role in consensus and
will no longer blocksync and thus no longer run the blocksync process.

Note, the blocksync reactor Service gossips entire block and relevant data such
that each receiving peer may construct the entire view of the blocksync state.

There is currently one blocksync protocol implementation. Internally the
top-level Reactor owns the single blocksync p2p channel and the always-on query
serving path:

- serve inbound BlockRequest and StatusRequest messages

Active syncing itself is handled by a separate sync controller that manages the
block pool, requests blocks, applies them locally, and hands off to consensus
once caught up. Sync-specific responses received on the shared channel are
forwarded into that controller.
*/
package blocksync
