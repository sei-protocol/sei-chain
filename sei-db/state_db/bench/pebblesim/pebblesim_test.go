package pebblesim

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

// newSim builds a PebbleSim with no store behind it. Key generation touches only cfg and rng, so
// these tests exercise it without opening PebbleDB.
func newSim(newKeyPct, newContractPct float64, numContracts int) *PebbleSim {
	cfg := DefaultConfig()
	cfg.NewKeyPct = newKeyPct
	cfg.NewContractPct = newContractPct
	cfg.NumContracts = numContracts
	return &PebbleSim{cfg: cfg, rng: newSimRNG(cfg.Seed)}
}

// TestKeyForIDIsStable is the invariant the whole design rests on: revisiting an id has to
// reproduce the key that id already wrote, or a "new version of an existing key" would in fact
// be a brand-new key and NewKeyPct would mean nothing.
func TestKeyForIDIsStable(t *testing.T) {
	p := newSim(10, 1, 100)

	for _, id := range []uint64{0, 1, 7, 1000, 999_999} {
		require.Equal(t, p.contractForSlot(id), p.contractForSlot(id), "contract for slot %d", id)
		require.Equal(t, slotForID(id), slotForID(id), "slot %d", id)
		require.Equal(t, contractAddress(id), contractAddress(id), "contract address %d", id)
		require.Equal(t, accountAddress(id), accountAddress(id), "account address %d", id)
	}

	// Growing the contract pool past an id must not move that id's slot to another contract.
	require.Equal(t, p.contractForSlot(42), newSim(10, 1, 100).contractForSlot(42))
}

// TestDerivationsAreDistinct guards the domain separators: ids that collide numerically across
// spaces must still derive different bytes.
func TestDerivationsAreDistinct(t *testing.T) {
	const id = 12345
	require.NotEqual(t, contractAddress(id), accountAddress(id))
	require.Len(t, contractAddress(id), keys.AddressLen)
	require.Len(t, slotForID(id), slotLen)

	seen := map[string]bool{}
	for id := uint64(0); id < 10_000; id++ {
		require.False(t, seen[string(slotForID(id))], "slot id %d collided", id)
		seen[string(slotForID(id))] = true
	}
}

func TestDrawIDHonoursNewKeyPct(t *testing.T) {
	const draws = 200_000
	for _, pct := range []float64{0, 1, 25, 100} {
		p := newSim(pct, 0, 100)
		for i := 0; i < draws; i++ {
			p.drawID(&p.nextSlotID)
		}
		// The first draw always mints, so 0% yields exactly one key.
		want := float64(draws) * pct / 100
		if pct == 0 {
			want = 1
		}
		require.InEpsilon(t, want, float64(p.nextSlotID.Load()), 0.05, "new-key-pct %v", pct)
	}
}

func TestContractPoolHonoursNewContractPct(t *testing.T) {
	const slots = 1_000_000

	fixed := newSim(100, 0, 100)
	require.Equal(t, uint64(100), fixed.contractsAt(slots), "0%% must hold the pool fixed")

	for _, pct := range []float64{0.01, 1, 100} {
		p := newSim(100, pct, 100)
		require.Equal(t, uint64(100)+uint64(slots*pct/100), p.contractsAt(slots), "new-contract-pct %v", pct)

		// Every slot lands on a contract that exists by the time it is minted, and each new
		// contract's first slot is the one that minted it.
		minters := 0
		for id := uint64(0); id < 10_000; id++ {
			contract := p.contractForSlot(id)
			require.Less(t, contract, p.contractsAt(id+1), "slot %d escaped the pool", id)
			if contract >= p.contractsAt(id) {
				minters++
			}
		}
		require.Equal(t, int(p.contractsAt(10_000)-100), minters, "new-contract-pct %v", pct)
	}
}

// TestRevisitedKeysAreBytewiseIdentical checks the property end to end, through the real key
// builders rather than the derivations alone: at 0% new, every write after the first targets the
// same key.
func TestRevisitedKeysAreBytewiseIdentical(t *testing.T) {
	p := newSim(0, 5, 100)

	first := p.randomStorageKey()
	for i := 0; i < 1000; i++ {
		require.True(t, bytes.Equal(first, p.randomStorageKey()), "storage key drifted on write %d", i)
	}

	firstNonce := p.randomNonceKey()
	for i := 0; i < 1000; i++ {
		require.True(t, bytes.Equal(firstNonce, p.randomNonceKey()), "nonce key drifted on write %d", i)
	}
}
