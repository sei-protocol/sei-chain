package seeds

import (
	"regexp"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/app/genesis"
)

// addrRe matches CometBFT's `NodeID@host:port`, where NodeID is the 20-byte
// address as 40 lowercase hex characters.
var addrRe = regexp.MustCompile(`^[0-9a-f]{40}@[a-zA-Z0-9.-]+:[0-9]{1,5}$`)

// A malformed NodeID is rejected at the secret-connection handshake, so the
// seed silently never connects — no error, no metric (seed mode serves none).
// This is the test that stops that shipping.
func TestSeedAddressesAreWellFormed(t *testing.T) {
	for chainID, addrs := range chainSeeds {
		if len(addrs) == 0 {
			t.Errorf("%s: no seeds configured", chainID)
		}
		seen := make(map[string]bool, len(addrs))
		for _, a := range addrs {
			if !addrRe.MatchString(a) {
				t.Errorf("%s: malformed seed address %q (want NodeID@host:port)", chainID, a)
				continue
			}
			id, hostPort, _ := strings.Cut(a, "@")
			if seen[id] {
				t.Errorf("%s: duplicate NodeID %s", chainID, id)
			}
			seen[id] = true
			if !strings.HasSuffix(hostPort, ":26656") {
				t.Errorf("%s: seed %q does not use the default P2P port 26656", chainID, a)
			}
		}
	}
}

// Every chain we ship seeds for must also be a chain seid can initialise, or
// the entry is a typo that would silently never apply. The converse is not
// asserted: arctic-1 is intentionally well-known for genesis but has no seeds.
func TestSeedChainsAreWellKnown(t *testing.T) {
	for _, chainID := range Chains() {
		if !genesis.IsWellKnown(chainID) {
			t.Errorf("chain %q has seeds but is not a well-known chain (typo?)", chainID)
		}
	}
}

func TestArcticIsDeliberatelyExcluded(t *testing.T) {
	if got := ForChain("arctic-1"); got != nil {
		t.Fatalf("arctic-1 is a devnet and must not ship seeds, got %v", got)
	}
	// Guard the premise of the exclusion: arctic-1 is still initialisable.
	if !genesis.IsWellKnown("arctic-1") {
		t.Error("arctic-1 is expected to remain a well-known chain for genesis")
	}
}

func TestForChain(t *testing.T) {
	for _, chainID := range []string{"pacific-1", "atlantic-2"} {
		if got := ForChain(chainID); len(got) != 3 {
			t.Errorf("%s: expected 3 seeds, got %d", chainID, len(got))
		}
	}
	for _, unknown := range []string{"", "unknown-1", "Pacific-1", "pacific-1 "} {
		if got := ForChain(unknown); got != nil {
			t.Errorf("chain %q: expected nil (exact match only), got %v", unknown, got)
		}
	}
}

// The returned slice must not alias package state.
func TestForChainReturnsCopy(t *testing.T) {
	first := ForChain("pacific-1")
	first[0] = "tampered"
	if second := ForChain("pacific-1"); second[0] == "tampered" {
		t.Fatal("ForChain leaked the package-level slice to callers")
	}
}

func TestBootstrapPeers(t *testing.T) {
	got := BootstrapPeers("pacific-1")
	if n := len(strings.Split(got, ",")); n != 3 {
		t.Errorf("expected 3 comma-separated entries, got %d (%q)", n, got)
	}
	if strings.Contains(got, " ") {
		t.Errorf("bootstrap-peers must not contain spaces: %q", got)
	}
	if BootstrapPeers("unknown-1") != "" {
		t.Error("unknown chain must yield an empty bootstrap-peers value")
	}
}
