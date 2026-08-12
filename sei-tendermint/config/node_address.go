package config

import "github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"

// NodeAddress is a peer address in `NodeID@host:port` form, as the p2p router
// understands it.
//
// Re-exported here because the parser lives under sei-tendermint/internal and
// is therefore unreachable from packages outside this module — app/seeds, which
// ships the default bootstrap-peers, being the case that prompted this. Without
// it, callers re-implement the address format and drift from what p2p actually
// accepts. This package already surfaces p2p types (see AutobahnValidator.NodeKey).
type NodeAddress = p2p.NodeAddress

// ParseNodeAddress parses and validates a peer address, applying exactly the
// rules the router applies when it dials one.
func ParseNodeAddress(address string) (NodeAddress, error) {
	return p2p.ParseNodeAddress(address)
}
