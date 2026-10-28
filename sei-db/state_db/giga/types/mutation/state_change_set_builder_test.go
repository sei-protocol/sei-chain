package mutation

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

var (
	testAddress  = bytes.Repeat([]byte{0x11}, keys.AddressLen)
	testSlot     = bytes.Repeat([]byte{0x22}, 32)
	testCodeHash = bytes.Repeat([]byte{0x33}, vtype.CodeHashLen)
	testBalance  = append(bytes.Repeat([]byte{0}, vtype.BalanceLen-1), 0x44)
	testMiscKey  = []byte{0x01, 0x02, 0x03}

	testBalanceKey = keys.BuildEVMKey(keys.EVMKeyBalance, testAddress)

	testAccountPhysicalKey = ktype.BuildPhysicalEVMKey(ktype.EVMKeyAccount, testAddress)
	testCodePhysicalKey    = ktype.BuildPhysicalEVMKey(keys.EVMKeyCode, testAddress)
	testStoragePhysicalKey = ktype.BuildPhysicalEVMKey(keys.EVMKeyStorage,
		append(bytes.Clone(testAddress), testSlot...))
	testMiscPhysicalKey = ktype.BuildPhysicalKey(keys.EVMStoreKey, testMiscKey)
)

func accountRow(t *testing.T, height int64, nonce uint64, codeHash []byte, balance []byte) []byte {
	t.Helper()
	parsedCodeHash, err := vtype.ParseCodeHash(codeHash)
	require.NoError(t, err)
	parsedBalance, err := vtype.ParseBalance(balance)
	require.NoError(t, err)
	return vtype.NewAccountData().
		SetBlockHeight(height).
		SetNonce(nonce).
		SetCodeHash(parsedCodeHash).
		SetBalance(parsedBalance).
		Serialize()
}

func storageRow(t *testing.T, height int64, value []byte) []byte {
	t.Helper()
	row, err := vtype.SerializeStorage(height, value)
	require.NoError(t, err)
	return row
}

// bigBalance returns balance, a big-endian 32-byte balance, as a big.Int.
func bigBalance(balance []byte) *big.Int {
	return new(big.Int).SetBytes(balance)
}

func TestMutationsToStateChangeSetBuildsEachKind(t *testing.T) {
	priorCode := []byte{0x60}
	code := []byte{0x60, 0x80}
	priorStorageValue := bytes.Repeat([]byte{0x55}, 32)
	mutations := []Mutation{
		{Key: testAccountPhysicalKey, NewValue: accountRow(t, 5, 7, testCodeHash, testBalance)},
		{
			Key:           testCodePhysicalKey,
			PreviousValue: vtype.SerializeCode(3, priorCode),
			NewValue:      vtype.SerializeCode(5, code),
		},
		{
			Key:           testStoragePhysicalKey,
			PreviousValue: storageRow(t, 3, priorStorageValue),
			NewValue:      storageRow(t, 5, testCodeHash),
		},
	}

	changes, err := MutationsToStateChangeSet(mutations, false)
	require.NoError(t, err)

	address := gigatypes.Address(testAddress)
	expected := &gigatypes.StateChangeSet{
		Accounts: []gigatypes.AccountChange{{
			Address: address,
			Account: gigatypes.AccountState{Balance: bigBalance(testBalance), Nonce: 7, Code: code},
			Prior: gigatypes.AccountState{
				Balance: bigBalance(make([]byte, vtype.BalanceLen)),
				Code:    priorCode,
			},
		}},
		Storage: []gigatypes.StorageChange{{
			Address:    address,
			Key:        gigatypes.Hash(testSlot),
			Value:      gigatypes.Hash(testCodeHash),
			PriorValue: gigatypes.Hash(priorStorageValue),
		}},
	}
	require.Equal(t, expected, changes)
}

func TestMutationsToStateChangeSetDeletes(t *testing.T) {
	priorCode := []byte{0x60}
	priorStorageValue := bytes.Repeat([]byte{0x55}, 32)
	mutations := []Mutation{
		{Key: testAccountPhysicalKey, PreviousValue: accountRow(t, 3, 7, testCodeHash, testBalance)},
		{Key: testCodePhysicalKey, PreviousValue: vtype.SerializeCode(3, priorCode)},
		{Key: testStoragePhysicalKey, PreviousValue: storageRow(t, 3, priorStorageValue)},
	}

	changes, err := MutationsToStateChangeSet(mutations, false)
	require.NoError(t, err)

	address := gigatypes.Address(testAddress)
	expected := &gigatypes.StateChangeSet{
		Accounts: []gigatypes.AccountChange{{
			Address: address,
			Account: gigatypes.AccountState{Balance: bigBalance(make([]byte, vtype.BalanceLen))},
			Prior:   gigatypes.AccountState{Balance: bigBalance(testBalance), Nonce: 7, Code: priorCode},
		}},
		Storage: []gigatypes.StorageChange{{
			Address:    address,
			Key:        gigatypes.Hash(testSlot),
			Delete:     true,
			PriorValue: gigatypes.Hash(priorStorageValue),
		}},
	}
	require.Equal(t, expected, changes)
}

func TestMutationsToStateChangeSetOmitsUnchangedValues(t *testing.T) {
	address := gigatypes.Address(testAddress)
	otherBalance := bytes.Repeat([]byte{0x01}, vtype.BalanceLen)
	priorAccountRow := accountRow(t, 3, 7, testCodeHash, testBalance)
	accountMutation := func(newRow []byte) Mutation {
		return Mutation{Key: testAccountPhysicalKey, PreviousValue: priorAccountRow, NewValue: newRow}
	}
	// accountChange returns the change of an account whose prior state is testBalance and nonce 7.
	accountChange := func(balance []byte, nonce uint64) *gigatypes.StateChangeSet {
		return &gigatypes.StateChangeSet{Accounts: []gigatypes.AccountChange{{
			Address: address,
			Account: gigatypes.AccountState{Balance: bigBalance(balance), Nonce: nonce},
			Prior:   gigatypes.AccountState{Balance: bigBalance(testBalance), Nonce: 7},
		}}, Storage: []gigatypes.StorageChange{}}
	}
	noChanges := &gigatypes.StateChangeSet{
		Accounts: []gigatypes.AccountChange{},
		Storage:  []gigatypes.StorageChange{},
	}
	zeroCodeHash := make([]byte, vtype.CodeHashLen)
	code := []byte{0x60, 0x80}
	storageValue := bytes.Repeat([]byte{0x55}, 32)

	tests := []struct {
		name     string
		mutation Mutation
		expected *gigatypes.StateChangeSet
	}{
		{
			name:     "account balance only",
			mutation: accountMutation(accountRow(t, 5, 7, testCodeHash, otherBalance)),
			expected: accountChange(otherBalance, 7),
		},
		{
			name:     "account nonce only",
			mutation: accountMutation(accountRow(t, 5, 8, testCodeHash, testBalance)),
			expected: accountChange(testBalance, 8),
		},
		{
			name:     "account balance and nonce",
			mutation: accountMutation(accountRow(t, 5, 8, testCodeHash, otherBalance)),
			expected: accountChange(otherBalance, 8),
		},
		{
			name:     "account code hash only",
			mutation: accountMutation(accountRow(t, 5, 7, zeroCodeHash, testBalance)),
			expected: noChanges,
		},
		{
			name: "code rewritten with the same bytecode",
			mutation: Mutation{
				Key:           testCodePhysicalKey,
				PreviousValue: vtype.SerializeCode(3, code),
				NewValue:      vtype.SerializeCode(5, code),
			},
			expected: noChanges,
		},
		{
			name: "storage rewritten with the same value",
			mutation: Mutation{
				Key:           testStoragePhysicalKey,
				PreviousValue: storageRow(t, 3, storageValue),
				NewValue:      storageRow(t, 5, storageValue),
			},
			expected: noChanges,
		},
		{
			name:     "storage deleted with no prior value",
			mutation: Mutation{Key: testStoragePhysicalKey},
			expected: noChanges,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutations := []Mutation{tt.mutation}

			changes, err := MutationsToStateChangeSet(mutations, false)
			require.NoError(t, err)
			require.Equal(t, tt.expected, changes)
		})
	}
}

func TestMutationsToStateChangeSetUnsupportedKeys(t *testing.T) {
	storageMutation := Mutation{Key: testStoragePhysicalKey, NewValue: storageRow(t, 5, testCodeHash)}
	tests := []struct {
		name     string
		mutation Mutation
	}{
		{
			name:     "EVM misc key",
			mutation: Mutation{Key: testMiscPhysicalKey, NewValue: vtype.SerializeMisc(5, []byte("misc"))},
		},
		{
			name: "non-EVM module key",
			mutation: Mutation{
				Key:      ktype.BuildPhysicalKey("bank", []byte("supply")),
				NewValue: vtype.SerializeMisc(5, []byte("100")),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutations := []Mutation{storageMutation, tt.mutation}

			_, err := MutationsToStateChangeSet(mutations, false)
			require.Error(t, err)

			changes, err := MutationsToStateChangeSet(mutations, true)
			require.NoError(t, err)
			expected := &gigatypes.StateChangeSet{
				Accounts: []gigatypes.AccountChange{},
				Storage: []gigatypes.StorageChange{{
					Address: gigatypes.Address(testAddress),
					Key:     gigatypes.Hash(testSlot),
					Value:   gigatypes.Hash(testCodeHash),
				}},
			}
			require.Equal(t, expected, changes)
		})
	}
}

func TestMutationsToStateChangeSetRejectsMalformedMutations(t *testing.T) {
	tests := []struct {
		name     string
		mutation Mutation
	}{
		{name: "key without module prefix", mutation: Mutation{Key: []byte("no-prefix")}},
		{name: "empty EVM key", mutation: Mutation{Key: []byte("evm/")}},
		{
			name:     "empty module name",
			mutation: Mutation{Key: ktype.BuildPhysicalKey("", []byte("supply"))},
		},
		{
			name:     "account field key",
			mutation: Mutation{Key: ktype.BuildPhysicalKey(keys.EVMStoreKey, testBalanceKey)},
		},
		{
			name:     "account key too short",
			mutation: Mutation{Key: testAccountPhysicalKey[:len(testAccountPhysicalKey)-1]},
		},
		{
			name:     "code key too long",
			mutation: Mutation{Key: append(bytes.Clone(testCodePhysicalKey), 0x00)},
		},
		{
			name:     "storage key too short",
			mutation: Mutation{Key: testStoragePhysicalKey[:len(testStoragePhysicalKey)-1]},
		},
		{
			name:     "corrupt new row",
			mutation: Mutation{Key: testCodePhysicalKey, NewValue: []byte{0xff}},
		},
		{
			name:     "corrupt previous row",
			mutation: Mutation{Key: testAccountPhysicalKey, PreviousValue: []byte{0xff}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutations := []Mutation{tt.mutation}
			for _, ignoreUnsupportedKeys := range []bool{false, true} {
				_, err := MutationsToStateChangeSet(mutations, ignoreUnsupportedKeys)
				require.Error(t, err, "ignoreUnsupportedKeys=%v", ignoreUnsupportedKeys)
			}
		})
	}
}
