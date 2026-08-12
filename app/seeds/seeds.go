// Package seeds ships the Sei Labs operated P2P seed nodes for the public Sei
// networks, so a freshly initialised node bootstraps peer discovery with no
// operator configuration.
//
// Seeds are dialled to populate the address book via the PEX reactor and may
// then be dropped, which is why they belong in `bootstrap-peers` rather than
// `persistent-peers` — an operator should not hold connections open against
// them indefinitely.
package seeds

import "strings"

// chainSeeds maps a well-known chain-id to its Sei Labs seed nodes, each in
// CometBFT's `NodeID@host:port` form. Three per network, one per cell
// (eu-central-1, eu-west-1, us-east-2), so losing a region does not cost
// bootstrap capability.
//
// PERMANENCE: these strings ship inside released binaries and operators pin
// them. The secret-connection handshake verifies the NodeID, so a changed ID
// is a rejected dial, not a degraded one — and a release already in the wild
// cannot be recalled. Retiring an address therefore means keeping it dialable
// until every release carrying it is out of use. Treat edits here as one-way.
//
// arctic-1 is deliberately absent. It is a devnet: it has no Cosmos
// chain-registry entry, so it is not an operator-facing network, and a devnet
// is the most likely to be reset or re-keyed — exactly the case where baking a
// permanent address into a binary is wrong. Devnet users set bootstrap-peers
// explicitly.
//
// Source of truth: clusters/<cell>/<chain>/seeds/seed-N/seed-N.yaml in
// sei-protocol/platform (the SeiNode's externalAddress plus its NodeID).
var chainSeeds = map[string][]string{
	"pacific-1": {
		"0cd5f57c249b5aca815710338e1fe7a14797585d@seed-0-p2p.pacific-1.prod.platform.sei.io:26656",
		"f0f057f1593d28bec11591cf146bd223e0be1866@seed-1-p2p.pacific-1.prod-euw1.platform.sei.io:26656",
		"8e28f62368a1ceae0102645db8584b218650930d@seed-2-p2p.pacific-1.prod-use2.platform.sei.io:26656",
	},
	"atlantic-2": {
		"362f934ead3654fca9cafdac63b52b47b2f9a95e@seed-0-p2p.atlantic-2.prod.platform.sei.io:26656",
		"1f55cd51183d3a6cad8a3667b91d08d0338bd52e@seed-1-p2p.atlantic-2.prod-euw1.platform.sei.io:26656",
		"7152be2e4c1a057d2b2467723058c5f0ec790472@seed-2-p2p.atlantic-2.prod-use2.platform.sei.io:26656",
	},
}

// BootstrapPeers returns the Sei Labs seeds for a chain as the comma-separated
// value CometBFT's `bootstrap-peers` expects, or "" when the chain-id is not
// recognised (private and local chains included).
func BootstrapPeers(chainID string) string {
	return strings.Join(chainSeeds[chainID], ",")
}
