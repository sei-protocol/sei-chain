package seeds

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/app/genesis"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/stretchr/testify/require"
)

const (
	pacific  = "pacific-1"
	atlantic = "atlantic-2"
	arctic   = "arctic-1"
)

// Addresses are parsed with the same parser the router uses when it dials, so
// this cannot drift from what p2p actually accepts. What it protects against is
// a typo in the table above: the NodeID is verified during the
// secret-connection handshake, so a wrong one is a rejected dial rather than a
// degraded connection, and seed mode serves no metrics to notice it by.
//
// Uniqueness is asserted across the whole table rather than per chain: the
// likeliest copy/paste error when adding a network is a pacific-1 entry landing
// in the atlantic-2 block, which a per-chain check cannot see.
func TestSeedAddressesParseAndAreUnique(t *testing.T) {
	seenID := map[string]string{}
	seenHost := map[string]string{}

	for chainID, addrs := range chainSeeds {
		require.NotEmptyf(t, addrs, "%s has no seeds", chainID)

		for _, entry := range addrs {
			addr, err := config.ParseNodeAddress(entry)
			require.NoErrorf(t, err, "%s: %q", chainID, entry)

			// Parsing alone does not cover the port: ParseNodeAddress
			// substitutes 26657, the RPC port, when one is missing or zero.
			// A dropped ":26656" would therefore parse clean, pass every other
			// assertion here, and ship a permanently wrong port to operators.
			require.EqualValuesf(t, 26656, addr.Port,
				"%s: %q must publish the P2P port explicitly", chainID, entry)

			id := string(addr.NodeID)
			require.NotContainsf(t, seenID, id,
				"NodeID %s appears in both %s and %s", id, seenID[id], chainID)
			seenID[id] = chainID

			require.NotContainsf(t, seenHost, addr.Hostname,
				"host %s appears in both %s and %s", addr.Hostname, seenHost[addr.Hostname], chainID)
			seenHost[addr.Hostname] = chainID
		}
	}
}

// Every chain we ship seeds for must also be a chain seid can initialise, or
// the entry is a typo that would silently never apply. The converse is not
// asserted: arctic-1 is intentionally well-known for genesis but has no seeds.
func TestSeedChainsAreWellKnown(t *testing.T) {
	for chainID := range chainSeeds {
		require.Truef(t, genesis.IsWellKnown(chainID),
			"chain %q has seeds but is not a well-known chain (typo?)", chainID)
	}
}

func TestArcticIsDeliberatelyExcluded(t *testing.T) {
	require.Empty(t, BootstrapPeers(arctic), "arctic-1 is a devnet and must not ship seeds")
	// Guard the premise of the exclusion: arctic-1 is still initialisable.
	require.True(t, genesis.IsWellKnown(arctic))
}

func TestBootstrapPeers(t *testing.T) {
	for _, chainID := range []string{pacific, atlantic} {
		got := BootstrapPeers(chainID)
		require.NotEmptyf(t, got, "%s should ship seeds", chainID)
		// Round-trip the rendered value through the parser the way seid does,
		// so the joined form is asserted rather than just the table entries.
		for _, entry := range strings.Split(got, ",") {
			_, err := config.ParseNodeAddress(entry)
			require.NoErrorf(t, err, "%s: %q", chainID, entry)
		}
	}

	// Exact match only — a chain-id we do not recognise must contribute nothing,
	// so private and local chains are unaffected.
	for _, unknown := range []string{"", "unknown-1", "Pacific-1", pacific + " "} {
		require.Emptyf(t, BootstrapPeers(unknown), "chain %q should ship no seeds", unknown)
	}
}
