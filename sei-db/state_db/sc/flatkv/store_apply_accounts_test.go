package flatkv

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// mergeAccountValuesReference is the implementation mergeAccountValues replaced: build a
// PendingAccountWrite per account, then merge each one onto the account's prior value. Kept here as
// the reference the differential test below compares against.
func mergeAccountValuesReference(
	t *testing.T,
	nonceChanges []classifiedChange,
	codeHashChanges []classifiedChange,
	balanceChanges []classifiedChange,
	oldValues map[string]*vtype.AccountData,
	blockHeight int64,
) map[string]*vtype.AccountData {
	t.Helper()
	pendingWrites, err := mergeAccountUpdates(nonceChanges, codeHashChanges, balanceChanges)
	require.NoError(t, err)

	result := make(map[string]*vtype.AccountData, len(pendingWrites))
	for addrStr, pendingWrite := range pendingWrites {
		result[addrStr] = pendingWrite.Merge(oldValues[addrStr], blockHeight)
	}
	return result
}

// requireSameAccounts asserts two account maps hold the same keys with byte-identical serialized
// values. Serialized form is what reaches the store and the lattice hash, so it is the comparison
// that matters.
func requireSameAccounts(t *testing.T, want map[string]*vtype.AccountData, got map[string]*vtype.AccountData) {
	t.Helper()
	require.Len(t, got, len(want))
	for key, wantAccount := range want {
		gotAccount, ok := got[key]
		require.True(t, ok, "missing account for key %x", key)
		require.Equal(t, wantAccount.Serialize(), gotAccount.Serialize(),
			"serialized account differs for key %x", key)
	}
}

// mergeAccountValues folds per-field changes straight onto the account instead of accumulating a
// PendingAccountWrite first. This runs both implementations over randomized blocks — accounts named
// by one kind, by both, repeatedly, and deleted — and requires byte-identical output.
func TestMergeAccountValuesMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(20260813))

	for round := 0; round < 200; round++ {
		accountCount := 1 + rng.Intn(12)
		keysForRound := make([]string, accountCount)
		for i := range keysForRound {
			keysForRound[i] = string(accountPhysKey(addrN(byte(i))))
		}

		// Some accounts already exist and some do not, so both the copy-forward and the
		// start-from-zero paths are covered.
		oldValues := make(map[string]*vtype.AccountData)
		for i, key := range keysForRound {
			if rng.Intn(2) == 0 {
				continue
			}
			old := vtype.NewAccountData().SetBlockHeight(int64(rng.Intn(100))).SetNonce(uint64(i + 1))
			if rng.Intn(2) == 0 {
				codeHash := codeHashN(byte(i + 1))
				old = old.SetCodeHash(&codeHash)
			}
			oldValues[key] = old
		}

		randomChanges := func(valueFor func() []byte) []classifiedChange {
			changes := make([]classifiedChange, 0, accountCount)
			for _, key := range keysForRound {
				// Not every account is named by every kind, and some are named more than once.
				for repeat := rng.Intn(3); repeat > 0; repeat-- {
					change := classifiedChange{key: key}
					if rng.Intn(4) > 0 {
						change.value = valueFor()
					}
					changes = append(changes, change)
				}
			}
			return changes
		}

		nonceChanges := randomChanges(func() []byte { return nonceBytes(uint64(rng.Intn(1000))) })
		codeHashChanges := randomChanges(func() []byte {
			codeHash := codeHashN(byte(rng.Intn(256)))
			return codeHash[:]
		})

		blockHeight := int64(100 + round)
		want := mergeAccountValuesReference(t, nonceChanges, codeHashChanges, nil, oldValues, blockHeight)
		got, err := mergeAccountValues(nonceChanges, codeHashChanges, nil, oldValues, blockHeight)
		require.NoError(t, err, "round %d", round)
		requireSameAccounts(t, want, got)
	}
}

// The prior account values handed in must survive the merge untouched: they are read out of the
// store, and mutating them would corrupt the value the store still holds.
func TestMergeAccountValuesDoesNotMutateOldValues(t *testing.T) {
	key := string(accountPhysKey(addrN(0x01)))
	oldCodeHash := codeHashN(0x77)
	old := vtype.NewAccountData().SetBlockHeight(7).SetNonce(3).SetCodeHash(&oldCodeHash)
	before := append([]byte(nil), old.Serialize()...)

	newCodeHash := codeHashN(0x99)
	_, err := mergeAccountValues(
		[]classifiedChange{{key: key, value: nonceBytes(42)}},
		[]classifiedChange{{key: key, value: newCodeHash[:]}},
		nil,
		map[string]*vtype.AccountData{key: old},
		99,
	)
	require.NoError(t, err)
	require.Equal(t, before, old.Serialize(), "the prior account value must not be modified")
}

// An invalid value must fail the whole merge rather than land a malformed account.
func TestMergeAccountValuesRejectsMalformedValues(t *testing.T) {
	key := string(accountPhysKey(addrN(0x02)))

	for name, changes := range map[string]struct {
		nonce    []classifiedChange
		codeHash []classifiedChange
	}{
		"short nonce":    {nonce: []classifiedChange{{key: key, value: []byte{0x01}}}},
		"short codehash": {codeHash: []classifiedChange{{key: key, value: []byte{0x01}}}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := mergeAccountValues(changes.nonce, changes.codeHash, nil, nil, 1)
			require.Error(t, err)
		})
	}
}

// A block that names the same account through both kinds must produce one account carrying both
// fields, not one per kind.
func TestMergeAccountValuesCombinesKindsIntoOneAccount(t *testing.T) {
	key := string(accountPhysKey(addrN(0x03)))
	codeHash := codeHashN(0x55)

	got, err := mergeAccountValues(
		[]classifiedChange{{key: key, value: nonceBytes(9)}},
		[]classifiedChange{{key: key, value: codeHash[:]}},
		nil,
		nil,
		123,
	)
	require.NoError(t, err)
	require.Len(t, got, 1)

	account := got[key]
	require.Equal(t, uint64(9), account.GetNonce())
	require.Equal(t, codeHash, *account.GetCodeHash())
	require.Equal(t, int64(123), account.GetBlockHeight())
}
