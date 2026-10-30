package giga

import (
	"bytes"
	"context"
	"math/big"
	"math/rand"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// changeSetShape sizes a randomly generated state change set.
type changeSetShape struct {
	// The number of account changes.
	accounts int
	// The number of storage changes.
	slots int
	// The number of misc changes.
	misc int
	// The number of distinct addresses accounts and slots are drawn from. Smaller pools revisit rows written
	// by earlier blocks.
	addressPool int
	// The number of distinct slots per address storage keys are drawn from.
	slotPool int
}

// randomStateChangeSet returns a state change set of the given shape, with every account, slot and misc key
// unique. Each account field is randomly changed or left equal to its prior value, including accounts that change
// nothing.
func randomStateChangeSet(rng *rand.Rand, shape changeSetShape) *gigatypes.StateChangeSet {
	changes := &gigatypes.StateChangeSet{}

	addresses := rng.Perm(shape.addressPool)[:shape.accounts]
	for _, a := range addresses {
		prior := randomAccountState(rng)
		account := prior
		if rng.Intn(3) != 0 {
			account.Balance = randomBalance(rng)
		}
		if rng.Intn(3) != 0 {
			account.Nonce = uint64(rng.Intn(4)) //nolint:gosec // small non-negative
		}
		if rng.Intn(4) == 0 {
			account.Code = randomCode(rng)
		}
		changes.Accounts = append(changes.Accounts, gigatypes.AccountChange{
			Address: testAddress(a),
			Account: account,
			Prior:   prior,
		})
	}

	for _, n := range rng.Perm(shape.addressPool * shape.slotPool)[:shape.slots] {
		change := gigatypes.StorageChange{
			Address: testAddress(n / shape.slotPool),
			Key:     common.BigToHash(big.NewInt(int64(n % shape.slotPool))),
		}
		switch rng.Intn(5) {
		case 0:
			change.Delete = true
		case 1:
			// Zero value without Delete: also a deletion.
		default:
			rng.Read(change.Value[:])
		}
		changes.Storage = append(changes.Storage, change)
	}

	modules := []string{"bank", "evmonly"}
	for i := 0; i < shape.misc; i++ {
		change := gigatypes.MiscChange{
			Module: modules[i%len(modules)],
			Key:    []byte{byte(i), byte(i >> 8), 'k'},
			Delete: rng.Intn(4) == 0,
		}
		if !change.Delete {
			change.Value = make([]byte, rng.Intn(16))
			rng.Read(change.Value)
		}
		changes.Misc = append(changes.Misc, change)
	}
	return changes
}

// randomAccountState returns an account state with a random balance, nonce and code, any of which may be empty.
func randomAccountState(rng *rand.Rand) gigatypes.AccountState {
	return gigatypes.AccountState{
		Balance: randomBalance(rng),
		Nonce:   uint64(rng.Intn(4)), //nolint:gosec // small non-negative
		Code:    randomCode(rng),
	}
}

// randomBalance returns nil, zero, a small balance, or a balance using all 256 bits.
func randomBalance(rng *rand.Rand) *big.Int {
	switch rng.Intn(4) {
	case 0:
		return nil
	case 1:
		return new(big.Int)
	case 2:
		return big.NewInt(int64(rng.Intn(1000)) + 1)
	default:
		limit := new(big.Int).Lsh(big.NewInt(1), 256)
		return new(big.Int).Rand(rng, limit)
	}
}

// randomCode returns no code, or one of a few short bytecodes.
func randomCode(rng *rand.Rand) []byte {
	if rng.Intn(2) == 0 {
		return nil
	}
	return bytes.Repeat([]byte{byte(rng.Intn(3) + 1)}, rng.Intn(40)+1)
}

// testAddress returns a distinct address for each index.
func testAddress(i int) gigatypes.Address {
	return common.BigToAddress(big.NewInt(int64(i) + 1))
}

// legacyMutations converts changes the way the commit path did before converting state change sets directly:
// through legacy changesets.
func legacyMutations(
	t testing.TB,
	changes *gigatypes.StateChangeSet,
	blockHeight uint64,
	prior gigatypes.StateView,
) []mutation.Mutation {
	t.Helper()
	mutations, err := flatkv.MutationsFromChangeSets(encodeLegacyChangeSets(changes), blockHeight, prior)
	require.NoError(t, err)
	return mutations
}

// sortedByKey returns mutations sorted by key, so two conversions that emit them in different orders compare equal.
func sortedByKey(mutations []mutation.Mutation) []mutation.Mutation {
	sorted := slices.Clone(mutations)
	slices.SortFunc(sorted, func(a mutation.Mutation, b mutation.Mutation) int {
		return bytes.Compare(a.Key, b.Key)
	})
	return sorted
}

// newConversionTestStore opens an empty FlatKV store to convert against.
func newConversionTestStore(t testing.TB, cfg *flatkvconfig.Config) *flatkv.CommitStore {
	t.Helper()
	// Not t.Context(): it is cancelled before cleanups run, and a store closed after its context is
	// cancelled cannot drain its in-flight blocks.
	store, err := flatkv.NewCommitStore(context.Background(), cfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.LoadLatest())
	return store
}

// The direct conversion replaces the legacy path on the commit path, so any byte it disagrees on is a different
// lattice hash and a different row on disk. Blocks are committed in sequence so later blocks overwrite and delete
// rows earlier ones wrote, and one block is large enough to be split across workers.
func TestMutationsFromStateChangeSetMatchesTheLegacyConversion(t *testing.T) {
	store := newConversionTestStore(t, flatkvconfig.DefaultTestConfig(t))
	// Far below the default, so that most blocks span several chunks.
	cfg := flatkvconfig.DefaultConfig()
	cfg.StateChangeSetChunkSize = 64
	rng := rand.New(rand.NewSource(1))

	shapes := []changeSetShape{
		{accounts: 40, slots: 100, misc: 6, addressPool: 64, slotPool: 4},
		{accounts: 40, slots: 100, misc: 6, addressPool: 64, slotPool: 4},
		{accounts: 6000, slots: 20000, misc: 50, addressPool: 8000, slotPool: 4},
		{accounts: 40, slots: 100, misc: 6, addressPool: 64, slotPool: 4},
		{accounts: 0, slots: 0, misc: 0, addressPool: 1, slotPool: 1},
	}
	for i, shape := range shapes {
		height := int64(i) + 1
		changes := randomStateChangeSet(rng, shape)

		prior := store.OpenView()
		//nolint:gosec // small positive heights
		direct, err := flatkv.MutationsFromStateChangeSet(changes, uint64(height), prior, cfg)
		require.NoError(t, err)
		legacy := legacyMutations(t, changes, uint64(height), prior) //nolint:gosec // small positive heights
		prior.Close()

		require.Equal(t, sortedByKey(legacy), sortedByKey(direct), "block %d", height)
		require.NoError(t, store.CommitStateChanges(height, direct))
	}
}
