package config

import "github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"

// NodeAddress is a peer address in `NodeID@host:port` form, as the p2p router
// understands it.
//
// Aliased here so callers outside the sei-tendermint tree, which cannot reach
// internal/p2p, can still name the type.
type NodeAddress = p2p.NodeAddress

// ParseNodeAddress parses and validates a peer address, applying the same rules
// the router applies when it dials one.
//
// A missing or zero port is substituted with 26657, so a caller that requires a
// particular port must assert it separately; parsing alone will not catch an
// omitted one.
func ParseNodeAddress(address string) (NodeAddress, error) {
	return p2p.ParseNodeAddress(address)
}
