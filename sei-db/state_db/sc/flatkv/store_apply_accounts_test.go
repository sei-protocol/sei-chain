package flatkv

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// mergeAccountValuesReference is the implementation accountUpdater replaced: build a
// PendingAccountWrite per account, then merge each one onto the account's prior value. Kept here as
// the reference the differential test below compares against.
//
// Reports the bytes that would reach the store, nil where the merged account is a deletion, since that
// is what the production path hands over.
func mergeAccountValuesReference(
	t *testing.T,
	nonceChanges []classifiedChange,
	codeHashChanges []classifiedChange,
	balanceChanges []classifiedChange,
	oldValues map[string]*vtype.AccountData,
	blockHeight int64,
) map[string][]byte {
	t.Helper()
	pendingWrites, err := mergeAccountUpdates(nonceChanges, codeHashChanges, balanceChanges)
	require.NoError(t, err)

	result := make(map[string][]byte, len(pendingWrites))
	for addrStr, pendingWrite := range pendingWrites {
		merged := pendingWrite.Merge(oldValues[addrStr], blockHeight)
		if merged.IsDelete() {
			result[addrStr] = nil
			continue
		}
		result[addrStr] = merged.Serialize()
	}
	return result
}

// mergeOnto runs the production merge the way ApplyChangeSets does: an accountUpdater is built from the
// changes, then asked for each account's new value. oldValues stands in for the account store, holding
// the rows that already exist, so a key it does not hold arrives as a nil prior value exactly as the
// engine would deliver it.
func mergeOnto(
	t *testing.T,
	nonceChanges []classifiedChange,
	codeHashChanges []classifiedChange,
	balanceChanges []classifiedChange,
	oldValues map[string]*vtype.AccountData,
	blockHeight int64,
) (map[string][]byte, error) {
	t.Helper()
	updater, err := newAccountUpdater(nonceChanges, codeHashChanges, balanceChanges, blockHeight)
	if err != nil {
		return nil, err
	}
	if updater == nil {
		return map[string][]byte{}, nil
	}

	result := make(map[string][]byte, len(updater.keys))
	for _, key := range updater.keys {
		var priorValue []byte
		if old, ok := oldValues[key]; ok {
			priorValue = old.Serialize()
		}
		value, err := updater.NewValueFor(key, priorValue)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

// requireSameAccounts asserts two sets of account writes hold the same keys with byte-identical
// values, a nil value meaning the account is deleted. Serialized form is what reaches the store and
// the lattice hash, so it is the comparison that matters.
func requireSameAccounts(t *testing.T, want map[string][]byte, got map[string][]byte) {
	t.Helper()
	require.Len(t, got, len(want))
	for key, wantValue := range want {
		gotValue, ok := got[key]
		require.True(t, ok, "missing account for key %x", key)
		require.Equal(t, wantValue, gotValue, "serialized account differs for key %x", key)
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
		got, err := mergeOnto(t, nonceChanges, codeHashChanges, nil, oldValues, blockHeight)
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
	_, err := mergeOnto(t,
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
			_, err := mergeOnto(t, changes.nonce, changes.codeHash, nil, nil, 1)
			require.Error(t, err)
		})
	}
}

// A block that names the same account through both kinds must produce one account carrying both
// fields, not one per kind.
func TestMergeAccountValuesCombinesKindsIntoOneAccount(t *testing.T) {
	key := string(accountPhysKey(addrN(0x03)))
	codeHash := codeHashN(0x55)

	got, err := mergeOnto(t,
		[]classifiedChange{{key: key, value: nonceBytes(9)}},
		[]classifiedChange{{key: key, value: codeHash[:]}},
		nil,
		nil,
		123,
	)
	require.NoError(t, err)
	require.Len(t, got, 1)

	require.NotNil(t, got[key], "an account carrying both fields must not serialize as a deletion")
	account, err := vtype.DeserializeAccountData(got[key])
	require.NoError(t, err)
	require.Equal(t, uint64(9), account.GetNonce())
	require.Equal(t, codeHash, *account.GetCodeHash())
	require.Equal(t, int64(123), account.GetBlockHeight())
}
