package flatkv

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// seedPriorRows commits rows to s as block 1, so a conversion of block 2 reads them as its prior state.
func seedPriorRows(t *testing.T, s *CommitStore, rows []mutation.Mutation) {
	t.Helper()
	require.NoError(t, s.CommitStateChanges(1, rows))
}

// convertBlock2 converts changes as block 2 against the state s holds at block 1.
func convertBlock2(
	t *testing.T,
	s *CommitStore,
	changes *gigatypes.StateChangeSet,
	cfg *config.Config,
) []mutation.Mutation {
	t.Helper()
	prior := s.OpenView()
	defer prior.Close()
	mutations, err := MutationsFromStateChangeSet(changes, 2, prior, cfg)
	require.NoError(t, err)
	return mutations
}

// codeRowKey returns the physical key of an address's code row.
func codeRowKey(addr ktype.Address) []byte {
	return ktype.BuildPhysicalEVMKey(keys.EVMKeyCode, addr[:])
}

// A storage change is one row: a new slot has no previous value, an overwrite carries the prior row, and both a
// deletion and a zero value delete the row.
func TestStateChangeSetStorageMutations(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	addr := addrN(1)
	seedPriorRows(t, s, []mutation.Mutation{
		{Key: storagePhysKey(addr, slotN(1)), NewValue: storageRow(t, 1, padLeft32(0x11))},
		{Key: storagePhysKey(addr, slotN(2)), NewValue: storageRow(t, 1, padLeft32(0x22))},
		{Key: storagePhysKey(addr, slotN(3)), NewValue: storageRow(t, 1, padLeft32(0x33))},
	})

	owner := gigatypes.Address(addr)
	changes := &gigatypes.StateChangeSet{Storage: []gigatypes.StorageChange{
		{Address: owner, Key: gigatypes.Hash(slotN(4)), Value: gigatypes.Hash(padLeft32(0x44))},
		{Address: owner, Key: gigatypes.Hash(slotN(1)), Value: gigatypes.Hash(padLeft32(0x55))},
		{Address: owner, Key: gigatypes.Hash(slotN(2)), Delete: true},
		{Address: owner, Key: gigatypes.Hash(slotN(3))},
	}}

	require.Equal(t, []mutation.Mutation{
		{Key: storagePhysKey(addr, slotN(4)), NewValue: storageRow(t, 2, padLeft32(0x44))},
		{
			Key:           storagePhysKey(addr, slotN(1)),
			PreviousValue: storageRow(t, 1, padLeft32(0x11)),
			NewValue:      storageRow(t, 2, padLeft32(0x55)),
		},
		{Key: storagePhysKey(addr, slotN(2)), PreviousValue: storageRow(t, 1, padLeft32(0x22))},
		{Key: storagePhysKey(addr, slotN(3)), PreviousValue: storageRow(t, 1, padLeft32(0x33))},
	}, convertBlock2(t, s, changes, config.DefaultTestConfig(t)))
}

// An account change writes only the fields that changed over the prior row, so the fields it leaves alone keep
// their prior values. An account whose fields all end at zero is deleted, and one that changes nothing produces no
// mutation.
func TestStateChangeSetAccountMutations(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	code := []byte{0x60, 0x00}
	codeHash := vtype.CodeHash(crypto.Keccak256Hash(code))
	var noCode vtype.CodeHash
	seedPriorRows(t, s, []mutation.Mutation{
		{Key: accountPhysKey(addrN(1)), NewValue: accountRow(t, 1, 3, codeHash[:], padLeft32(5))},
		{Key: accountPhysKey(addrN(2)), NewValue: accountRow(t, 1, 1, noCode[:], padLeft32(7))},
		{Key: accountPhysKey(addrN(4)), NewValue: accountRow(t, 1, 0, noCode[:], padLeft32(6))},
		{Key: accountPhysKey(addrN(5)), NewValue: accountRow(t, 1, 2, noCode[:], padLeft32(8))},
	})

	changes := &gigatypes.StateChangeSet{Accounts: []gigatypes.AccountChange{
		{
			// Balance only: nonce and code hash are kept from the prior row.
			Address: gigatypes.Address(addrN(1)),
			Account: gigatypes.AccountState{Balance: big.NewInt(9), Nonce: 3, Code: code},
			Prior:   gigatypes.AccountState{Balance: big.NewInt(5), Nonce: 3, Code: code},
		},
		{
			// Nonce only.
			Address: gigatypes.Address(addrN(2)),
			Account: gigatypes.AccountState{Balance: big.NewInt(7), Nonce: 2},
			Prior:   gigatypes.AccountState{Balance: big.NewInt(7), Nonce: 1},
		},
		{
			// New account.
			Address: gigatypes.Address(addrN(3)),
			Account: gigatypes.AccountState{Balance: big.NewInt(4)},
		},
		{
			// Every field ends at zero.
			Address: gigatypes.Address(addrN(4)),
			Account: gigatypes.AccountState{Balance: big.NewInt(0)},
			Prior:   gigatypes.AccountState{Balance: big.NewInt(6)},
		},
		{
			// Nothing changed.
			Address: gigatypes.Address(addrN(5)),
			Account: gigatypes.AccountState{Balance: big.NewInt(8), Nonce: 2},
			Prior:   gigatypes.AccountState{Balance: big.NewInt(8), Nonce: 2},
		},
	}}

	require.Equal(t, []mutation.Mutation{
		{
			Key:           accountPhysKey(addrN(1)),
			PreviousValue: accountRow(t, 1, 3, codeHash[:], padLeft32(5)),
			NewValue:      accountRow(t, 2, 3, codeHash[:], padLeft32(9)),
		},
		{
			Key:           accountPhysKey(addrN(2)),
			PreviousValue: accountRow(t, 1, 1, noCode[:], padLeft32(7)),
			NewValue:      accountRow(t, 2, 2, noCode[:], padLeft32(7)),
		},
		{Key: accountPhysKey(addrN(3)), NewValue: accountRow(t, 2, 0, noCode[:], padLeft32(4))},
		{Key: accountPhysKey(addrN(4)), PreviousValue: accountRow(t, 1, 0, noCode[:], padLeft32(6))},
	}, convertBlock2(t, s, changes, config.DefaultTestConfig(t)))
}

// A code change writes the account row's code hash and the code row: deploying sets both, and removing the code
// clears the hash and deletes the code row.
func TestStateChangeSetCodeMutations(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	oldCode := []byte{0x60, 0x01}
	oldHash := vtype.CodeHash(crypto.Keccak256Hash(oldCode))
	newCode := bytes.Repeat([]byte{0x60}, 40)
	newHash := vtype.CodeHash(crypto.Keccak256Hash(newCode))
	var noCode vtype.CodeHash
	seedPriorRows(t, s, []mutation.Mutation{
		{Key: accountPhysKey(addrN(1)), NewValue: accountRow(t, 1, 1, oldHash[:], padLeft32(1))},
		{Key: codeRowKey(addrN(1)), NewValue: vtype.SerializeCode(1, oldCode)},
	})

	changes := &gigatypes.StateChangeSet{Accounts: []gigatypes.AccountChange{
		{
			// Code removed; the balance and nonce keep the row alive.
			Address: gigatypes.Address(addrN(1)),
			Account: gigatypes.AccountState{Balance: big.NewInt(1), Nonce: 1},
			Prior:   gigatypes.AccountState{Balance: big.NewInt(1), Nonce: 1, Code: oldCode},
		},
		{
			// Deployed to a new account.
			Address: gigatypes.Address(addrN(2)),
			Account: gigatypes.AccountState{Nonce: 1, Code: newCode},
		},
	}}

	require.Equal(t, []mutation.Mutation{
		{
			Key:           accountPhysKey(addrN(1)),
			PreviousValue: accountRow(t, 1, 1, oldHash[:], padLeft32(1)),
			NewValue:      accountRow(t, 2, 1, noCode[:], padLeft32(1)),
		},
		{Key: codeRowKey(addrN(1)), PreviousValue: vtype.SerializeCode(1, oldCode)},
		{Key: accountPhysKey(addrN(2)), NewValue: accountRow(t, 2, 1, newHash[:], padLeft32(0))},
		{Key: codeRowKey(addrN(2)), NewValue: vtype.SerializeCode(2, newCode)},
	}, convertBlock2(t, s, changes, config.DefaultTestConfig(t)))
}

// A misc change is one row under its own module's key prefix: a write carries the prior row, an empty value is an
// empty row rather than a deletion, and a deletion deletes the row.
func TestStateChangeSetMiscMutations(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	seedPriorRows(t, s, []mutation.Mutation{
		{Key: ktype.BuildPhysicalKey("bank", []byte("k1")), NewValue: vtype.SerializeMisc(1, []byte("old"))},
		{Key: ktype.BuildPhysicalKey("bank", []byte("k3")), NewValue: vtype.SerializeMisc(1, []byte("gone"))},
	})

	changes := &gigatypes.StateChangeSet{Misc: []gigatypes.MiscChange{
		{Module: "bank", Key: []byte("k1"), Value: []byte("new")},
		{Module: "evmonly", Key: []byte("k2"), Value: []byte{}},
		{Module: "bank", Key: []byte("k3"), Delete: true},
	}}

	require.Equal(t, []mutation.Mutation{
		{
			Key:           ktype.BuildPhysicalKey("bank", []byte("k1")),
			PreviousValue: vtype.SerializeMisc(1, []byte("old")),
			NewValue:      vtype.SerializeMisc(2, []byte("new")),
		},
		{Key: ktype.BuildPhysicalKey("evmonly", []byte("k2")), NewValue: vtype.SerializeMisc(2, []byte{})},
		{
			Key:           ktype.BuildPhysicalKey("bank", []byte("k3")),
			PreviousValue: vtype.SerializeMisc(1, []byte("gone")),
		},
	}, convertBlock2(t, s, changes, config.DefaultTestConfig(t)))
}

// A block split across several chunks and workers produces the same mutations, in the same order, as converting it
// whole: accounts first, a code row right after its account row, then storage, then misc.
func TestStateChangeSetMutationsAcrossChunks(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	seedPriorRows(t, s, nil)

	code := []byte{0x60, 0x02}
	codeHash := vtype.CodeHash(crypto.Keccak256Hash(code))
	var noCode vtype.CodeHash
	changes := &gigatypes.StateChangeSet{}
	var want []mutation.Mutation
	for i := byte(1); i <= 10; i++ {
		account := gigatypes.AccountState{Balance: big.NewInt(int64(i))}
		if i%3 == 0 {
			account.Code = code
		}
		changes.Accounts = append(changes.Accounts, gigatypes.AccountChange{
			Address: gigatypes.Address(addrN(i)),
			Account: account,
		})
		hash := noCode
		if i%3 == 0 {
			hash = codeHash
		}
		want = append(want, mutation.Mutation{
			Key:      accountPhysKey(addrN(i)),
			NewValue: accountRow(t, 2, 0, hash[:], padLeft32(i)),
		})
		if i%3 == 0 {
			codeRow := vtype.SerializeCode(2, code)
			want = append(want, mutation.Mutation{Key: codeRowKey(addrN(i)), NewValue: codeRow})
		}
	}
	for i := byte(1); i <= 10; i++ {
		changes.Storage = append(changes.Storage, gigatypes.StorageChange{
			Address: gigatypes.Address(addrN(i)),
			Key:     gigatypes.Hash(slotN(i)),
			Value:   gigatypes.Hash(padLeft32(i)),
		})
		want = append(want, mutation.Mutation{
			Key:      storagePhysKey(addrN(i), slotN(i)),
			NewValue: storageRow(t, 2, padLeft32(i)),
		})
	}
	changes.Misc = []gigatypes.MiscChange{{Module: "bank", Key: []byte("k"), Value: []byte("v")}}
	want = append(want, mutation.Mutation{
		Key:      ktype.BuildPhysicalKey("bank", []byte("k")),
		NewValue: vtype.SerializeMisc(2, []byte("v")),
	})

	cfg := config.DefaultTestConfig(t)
	cfg.StateChangeSetChunkSize = 3
	cfg.StateChangeSetThreadsPerCore = 2
	require.Equal(t, want, convertBlock2(t, s, changes, cfg))
}

// A misc change in the EVM module would land a misc row among the account, code or storage rows.
func TestStateChangeSetRejectsAMiscChangeInTheEVMModule(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	seedPriorRows(t, s, nil)
	changes := &gigatypes.StateChangeSet{
		Misc: []gigatypes.MiscChange{{Module: keys.EVMStoreKey, Key: []byte{0x01}, Value: []byte{0x02}}},
	}

	prior := s.OpenView()
	defer prior.Close()
	_, err := MutationsFromStateChangeSet(changes, 2, prior, config.DefaultTestConfig(t))
	require.ErrorContains(t, err, "EVM module")
}

// A balance wider than an account row's balance field cannot be stored without truncation.
func TestStateChangeSetRejectsABalanceWiderThan256Bits(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	seedPriorRows(t, s, nil)
	changes := &gigatypes.StateChangeSet{Accounts: []gigatypes.AccountChange{{
		Address: gigatypes.Address(addrN(1)),
		Account: gigatypes.AccountState{Balance: new(big.Int).Lsh(big.NewInt(1), 256)},
	}}}

	prior := s.OpenView()
	defer prior.Close()
	_, err := MutationsFromStateChangeSet(changes, 2, prior, config.DefaultTestConfig(t))
	require.ErrorContains(t, err, "256-bit")
}

// Every previous value comes from the prior view, which is only meaningful if it is the block before.
func TestStateChangeSetRejectsAPriorViewAtTheWrongHeight(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	seedPriorRows(t, s, nil)

	prior := s.OpenView()
	defer prior.Close()
	_, err := MutationsFromStateChangeSet(&gigatypes.StateChangeSet{}, 3, prior, config.DefaultTestConfig(t))
	require.ErrorContains(t, err, "needs the state of the block before")
}
