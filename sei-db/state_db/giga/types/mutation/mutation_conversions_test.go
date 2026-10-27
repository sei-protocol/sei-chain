package mutation

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

var _ gigatypes.StateView = (*fakeStateView)(nil)

// fakeStateView is a StateView over a fixed set of rows, keyed by physical key. Only the raw reads are supported.
type fakeStateView struct {
	// Left nil, so every read other than the raw reads panics.
	gigatypes.StateView

	// The block height the view reports.
	height int64

	// The stored rows, keyed by string(physical key).
	rows map[string][]byte
}

func (v *fakeStateView) GetBlockHeight() int64 { return v.height }

func (v *fakeStateView) GetRaw(key []byte) ([]byte, bool) {
	value, ok := v.rows[string(key)]
	return value, ok
}

func (v *fakeStateView) BatchGetRaw(physicalKeys [][]byte) map[string][]byte {
	found := make(map[string][]byte)
	for _, key := range physicalKeys {
		if value, ok := v.rows[string(key)]; ok {
			found[string(key)] = value
		}
	}
	return found
}

func (v *fakeStateView) Close() {}

// stateAt returns a view at height holding rows, keyed by physical key.
func stateAt(height int64, rows map[string][]byte) *fakeStateView {
	if rows == nil {
		rows = map[string][]byte{}
	}
	return &fakeStateView{height: height, rows: rows}
}

var (
	testAddress  = bytes.Repeat([]byte{0x11}, keys.AddressLen)
	testSlot     = bytes.Repeat([]byte{0x22}, 32)
	testCodeHash = bytes.Repeat([]byte{0x33}, vtype.CodeHashLen)
	testBalance  = append(bytes.Repeat([]byte{0}, vtype.BalanceLen-1), 0x44)
	testMiscKey  = []byte{0x01, 0x02, 0x03}

	testNonceKey    = keys.BuildEVMKey(keys.EVMKeyNonce, testAddress)
	testCodeHashKey = keys.BuildEVMKey(keys.EVMKeyCodeHash, testAddress)
	testBalanceKey  = keys.BuildEVMKey(keys.EVMKeyBalance, testAddress)
	testCodeKey     = keys.BuildEVMKey(keys.EVMKeyCode, testAddress)
	testStorageKey  = keys.BuildEVMKey(keys.EVMKeyStorage, append(bytes.Clone(testAddress), testSlot...))

	testAccountPhysicalKey = ktype.BuildPhysicalEVMKey(ktype.EVMKeyAccount, testAddress)
	testCodePhysicalKey    = ktype.BuildPhysicalEVMKey(keys.EVMKeyCode, testAddress)
	testStoragePhysicalKey = ktype.BuildPhysicalEVMKey(keys.EVMKeyStorage,
		append(bytes.Clone(testAddress), testSlot...))
	testMiscPhysicalKey = ktype.BuildPhysicalKey(keys.EVMStoreKey, testMiscKey)
)

func nonceBytes(nonce uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, nonce)
}

func evmChangeSet(pairs ...*proto.KVPair) *proto.NamedChangeSet {
	return moduleChangeSet(keys.EVMStoreKey, pairs...)
}

func moduleChangeSet(moduleName string, pairs ...*proto.KVPair) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{Name: moduleName, Changeset: proto.ChangeSet{Pairs: pairs}}
}

func set(key []byte, value []byte) *proto.KVPair {
	return &proto.KVPair{Key: key, Value: value}
}

func del(key []byte) *proto.KVPair {
	return &proto.KVPair{Key: key, Delete: true}
}

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

// requireChangeSetsMatch requires that actual holds one changeset per module of expected, with the same pairs in
// any order.
func requireChangeSetsMatch(t *testing.T, expected []*proto.NamedChangeSet, actual []*proto.NamedChangeSet) {
	t.Helper()
	actualPairs := make(map[string][]*proto.KVPair)
	for _, changeSet := range actual {
		_, duplicate := actualPairs[changeSet.Name]
		require.False(t, duplicate, "module %q appears more than once", changeSet.Name)
		actualPairs[changeSet.Name] = changeSet.Changeset.Pairs
	}
	require.Len(t, actualPairs, len(expected))
	for _, changeSet := range expected {
		pairs := actualPairs[changeSet.Name]
		require.ElementsMatch(t, changeSet.Changeset.Pairs, pairs, "module %q", changeSet.Name)
	}
}

func TestMutationsFromChangeSetsBuildsEachKind(t *testing.T) {
	storageValue := bytes.Repeat([]byte{0x55}, 32)
	code := []byte{0x60, 0x80}
	changeSets := []*proto.NamedChangeSet{
		evmChangeSet(
			set(testStorageKey, storageValue),
			set(testCodeKey, code),
			set(testMiscKey, []byte("evm misc")),
			set(testNonceKey, nonceBytes(7)),
			set(testCodeHashKey, testCodeHash),
			set(testBalanceKey, testBalance),
		),
		moduleChangeSet("bank", set([]byte("supply"), []byte("100"))),
	}

	mutations, err := MutationsFromChangeSets(changeSets, 5, stateAt(4, nil))
	require.NoError(t, err)

	expected := []Mutation{
		{Key: testStoragePhysicalKey, NewValue: storageRow(t, 5, storageValue)},
		{Key: testCodePhysicalKey, NewValue: vtype.SerializeCode(5, code)},
		{Key: testMiscPhysicalKey, NewValue: vtype.SerializeMisc(5, []byte("evm misc"))},
		{Key: testAccountPhysicalKey, NewValue: accountRow(t, 5, 7, testCodeHash, testBalance)},
		{
			Key:      ktype.BuildPhysicalKey("bank", []byte("supply")),
			NewValue: vtype.SerializeMisc(5, []byte("100")),
		},
	}
	require.ElementsMatch(t, expected, mutations)
}

func TestMutationsFromChangeSetsMergesAccountOntoPriorRow(t *testing.T) {
	priorRow := accountRow(t, 3, 7, testCodeHash, testBalance)
	prior := stateAt(4, map[string][]byte{string(testAccountPhysicalKey): priorRow})

	changeSets := []*proto.NamedChangeSet{evmChangeSet(set(testNonceKey, nonceBytes(8)))}
	mutations, err := MutationsFromChangeSets(changeSets, 5, prior)
	require.NoError(t, err)

	expected := []Mutation{{
		Key:           testAccountPhysicalKey,
		PreviousValue: priorRow,
		NewValue:      accountRow(t, 5, 8, testCodeHash, testBalance),
	}}
	require.Equal(t, expected, mutations)
}

func TestMutationsFromChangeSetsDeletes(t *testing.T) {
	otherAddress := bytes.Repeat([]byte{0x12}, keys.AddressLen)
	otherSlotKey := keys.BuildEVMKey(keys.EVMKeyStorage, append(bytes.Clone(testAddress), make([]byte, 32)...))
	emptyMiscKey := []byte{0x01, 0x09}
	prior := stateAt(4, map[string][]byte{
		string(testAccountPhysicalKey): accountRow(t, 3, 7, testCodeHash, testBalance),
	})

	changeSets := []*proto.NamedChangeSet{
		evmChangeSet(
			set(testStorageKey, make([]byte, 32)),
			del(otherSlotKey),
			set(testCodeKey, []byte{}),
			del(keys.BuildEVMKey(keys.EVMKeyCode, otherAddress)),
			del(testMiscKey),
			set(emptyMiscKey, nil),
			del(testNonceKey),
			del(testCodeHashKey),
			set(testBalanceKey, make([]byte, vtype.BalanceLen)),
		),
	}
	mutations, err := MutationsFromChangeSets(changeSets, 5, prior)
	require.NoError(t, err)

	expected := []Mutation{
		{Key: testStoragePhysicalKey},
		{Key: ktype.BuildPhysicalEVMKey(keys.EVMKeyStorage,
			append(bytes.Clone(testAddress), make([]byte, 32)...))},
		{Key: testCodePhysicalKey},
		{Key: ktype.BuildPhysicalEVMKey(keys.EVMKeyCode, otherAddress)},
		{Key: testMiscPhysicalKey},
		// An empty misc value is a write, not a deletion.
		{
			Key:      ktype.BuildPhysicalKey(keys.EVMStoreKey, emptyMiscKey),
			NewValue: vtype.SerializeMisc(5, nil),
		},
		// An account left with every field zero is deleted.
		{Key: testAccountPhysicalKey, PreviousValue: accountRow(t, 3, 7, testCodeHash, testBalance)},
	}
	require.ElementsMatch(t, expected, mutations)
}

func TestMutationsFromChangeSetsKeepsLastWrite(t *testing.T) {
	keyA := []byte{0x01, 0x0a}
	keyB := []byte{0x01, 0x0b}
	changeSets := []*proto.NamedChangeSet{
		evmChangeSet(set(keyA, []byte("first")), set(keyB, []byte("other"))),
		evmChangeSet(set(keyA, []byte("last"))),
	}
	mutations, err := MutationsFromChangeSets(changeSets, 5, stateAt(4, nil))
	require.NoError(t, err)

	evmMiscKey := func(key []byte) []byte { return ktype.BuildPhysicalKey(keys.EVMStoreKey, key) }
	expected := []Mutation{
		{Key: evmMiscKey(keyA), NewValue: vtype.SerializeMisc(5, []byte("last"))},
		{Key: evmMiscKey(keyB), NewValue: vtype.SerializeMisc(5, []byte("other"))},
	}
	require.ElementsMatch(t, expected, mutations)
}

func TestMutationsFromChangeSetsDoesNotAliasChangeSetValues(t *testing.T) {
	value := []byte("value")
	changeSets := []*proto.NamedChangeSet{evmChangeSet(set(testMiscKey, value))}
	mutations, err := MutationsFromChangeSets(changeSets, 5, stateAt(4, nil))
	require.NoError(t, err)

	value[0] = 'X'
	require.Equal(t, vtype.SerializeMisc(5, []byte("value")), mutations[0].NewValue)
}

func TestMutationsFromChangeSetsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name       string
		changeSets []*proto.NamedChangeSet
		prior      gigatypes.StateView
	}{
		{
			name:  "prior state at the wrong height",
			prior: stateAt(3, nil),
		},
		{
			name:       "empty module name",
			changeSets: []*proto.NamedChangeSet{moduleChangeSet("", set([]byte("k"), []byte("v")))},
			prior:      stateAt(4, nil),
		},
		{
			name:       "empty EVM key",
			changeSets: []*proto.NamedChangeSet{evmChangeSet(set(nil, []byte("v")))},
			prior:      stateAt(4, nil),
		},
		{
			name:       "invalid nonce",
			changeSets: []*proto.NamedChangeSet{evmChangeSet(set(testNonceKey, []byte{1}))},
			prior:      stateAt(4, nil),
		},
		{
			name:       "invalid storage value",
			changeSets: []*proto.NamedChangeSet{evmChangeSet(set(testStorageKey, []byte{1}))},
			prior:      stateAt(4, nil),
		},
		{
			name:       "corrupt prior account row",
			changeSets: []*proto.NamedChangeSet{evmChangeSet(set(testNonceKey, nonceBytes(1)))},
			prior:      stateAt(4, map[string][]byte{string(testAccountPhysicalKey): {0xff}}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MutationsFromChangeSets(tt.changeSets, 5, tt.prior)
			require.Error(t, err)
		})
	}
}

func TestAccountMutationToChangeSet(t *testing.T) {
	row := accountRow(t, 5, 0, make([]byte, vtype.CodeHashLen), testBalance)
	changeSet, err := (&Mutation{Key: testAccountPhysicalKey, NewValue: row}).ToChangeSet()
	require.NoError(t, err)
	expected := evmChangeSet(
		set(testNonceKey, nonceBytes(0)),
		del(testCodeHashKey),
		set(testBalanceKey, testBalance),
	)
	requireChangeSetsMatch(t, []*proto.NamedChangeSet{expected}, []*proto.NamedChangeSet{changeSet})

	changeSet, err = (&Mutation{Key: testAccountPhysicalKey, PreviousValue: row}).ToChangeSet()
	require.NoError(t, err)
	expected = evmChangeSet(del(testNonceKey), del(testCodeHashKey), del(testBalanceKey))
	requireChangeSetsMatch(t, []*proto.NamedChangeSet{expected}, []*proto.NamedChangeSet{changeSet})
}

func TestMutationToChangeSetRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		mutation Mutation
	}{
		{name: "key without module prefix", mutation: Mutation{Key: []byte("no-prefix")}},
		{name: "empty EVM key", mutation: Mutation{Key: []byte("evm/")}},
		{
			name:     "account field key",
			mutation: Mutation{Key: ktype.BuildPhysicalKey(keys.EVMStoreKey, testBalanceKey)},
		},
		{
			name:     "corrupt row",
			mutation: Mutation{Key: testCodePhysicalKey, NewValue: []byte{0xff}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.mutation.ToChangeSet()
			require.Error(t, err)
		})
	}
}

func TestMutationsToChangeSetsGroupsByModule(t *testing.T) {
	keyA := []byte{0x01, 0x0a}
	keyC := []byte{0x01, 0x0c}
	mutations := []Mutation{
		{
			Key:      ktype.BuildPhysicalKey(keys.EVMStoreKey, keyA),
			NewValue: vtype.SerializeMisc(5, []byte("a")),
		},
		{Key: ktype.BuildPhysicalKey("bank", []byte("b"))},
		{
			Key:      ktype.BuildPhysicalKey(keys.EVMStoreKey, keyC),
			NewValue: vtype.SerializeMisc(5, []byte("c")),
		},
	}
	changeSets, err := MutationsToChangeSets(mutations)
	require.NoError(t, err)

	expected := []*proto.NamedChangeSet{
		evmChangeSet(set(keyA, []byte("a")), set(keyC, []byte("c"))),
		moduleChangeSet("bank", del([]byte("b"))),
	}
	requireChangeSetsMatch(t, expected, changeSets)
}

func TestMutationsRoundTripThroughChangeSets(t *testing.T) {
	prior := stateAt(4, map[string][]byte{
		string(testAccountPhysicalKey): accountRow(t, 3, 7, testCodeHash, testBalance),
		string(testCodePhysicalKey):    vtype.SerializeCode(3, []byte{0x60}),
	})
	changeSets := []*proto.NamedChangeSet{
		evmChangeSet(
			set(testBalanceKey, bytes.Repeat([]byte{0x01}, vtype.BalanceLen)),
			del(testCodeKey),
			set(testStorageKey, testCodeHash),
			set(testMiscKey, []byte{}),
		),
		moduleChangeSet("bank", set([]byte("supply"), []byte("100"))),
	}

	mutations, err := MutationsFromChangeSets(changeSets, 5, prior)
	require.NoError(t, err)
	converted, err := MutationsToChangeSets(mutations)
	require.NoError(t, err)
	roundTripped, err := MutationsFromChangeSets(converted, 5, prior)
	require.NoError(t, err)
	require.ElementsMatch(t, mutations, roundTripped)
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

	changes, err := MutationsToStateChangeSet(mutations, false, false)
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

	changes, err := MutationsToStateChangeSet(mutations, false, false)
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

func TestMutationsToStateChangeSetUnchangedValues(t *testing.T) {
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
		// The expected set when unchanged values are omitted.
		withoutUnchanged *gigatypes.StateChangeSet
		// The expected set when unchanged values are included.
		withUnchanged *gigatypes.StateChangeSet
	}{
		{
			name:             "account balance only",
			mutation:         accountMutation(accountRow(t, 5, 7, testCodeHash, otherBalance)),
			withoutUnchanged: accountChange(otherBalance, 7),
			withUnchanged:    accountChange(otherBalance, 7),
		},
		{
			name:             "account nonce only",
			mutation:         accountMutation(accountRow(t, 5, 8, testCodeHash, testBalance)),
			withoutUnchanged: accountChange(testBalance, 8),
			withUnchanged:    accountChange(testBalance, 8),
		},
		{
			name:             "account balance and nonce",
			mutation:         accountMutation(accountRow(t, 5, 8, testCodeHash, otherBalance)),
			withoutUnchanged: accountChange(otherBalance, 8),
			withUnchanged:    accountChange(otherBalance, 8),
		},
		{
			name:             "account code hash only",
			mutation:         accountMutation(accountRow(t, 5, 7, zeroCodeHash, testBalance)),
			withoutUnchanged: noChanges,
			withUnchanged:    accountChange(testBalance, 7),
		},
		{
			name: "code rewritten with the same bytecode",
			mutation: Mutation{
				Key:           testCodePhysicalKey,
				PreviousValue: vtype.SerializeCode(3, code),
				NewValue:      vtype.SerializeCode(5, code),
			},
			withoutUnchanged: noChanges,
			withUnchanged: &gigatypes.StateChangeSet{Accounts: []gigatypes.AccountChange{{
				Address: address,
				Account: gigatypes.AccountState{Code: code},
				Prior:   gigatypes.AccountState{Code: code},
			}}, Storage: []gigatypes.StorageChange{}},
		},
		{
			name: "storage rewritten with the same value",
			mutation: Mutation{
				Key:           testStoragePhysicalKey,
				PreviousValue: storageRow(t, 3, storageValue),
				NewValue:      storageRow(t, 5, storageValue),
			},
			withoutUnchanged: noChanges,
			withUnchanged: &gigatypes.StateChangeSet{
				Accounts: []gigatypes.AccountChange{},
				Storage: []gigatypes.StorageChange{{
					Address:    address,
					Key:        gigatypes.Hash(testSlot),
					Value:      gigatypes.Hash(storageValue),
					PriorValue: gigatypes.Hash(storageValue),
				}},
			},
		},
		{
			name:             "storage deleted with no prior value",
			mutation:         Mutation{Key: testStoragePhysicalKey},
			withoutUnchanged: noChanges,
			withUnchanged: &gigatypes.StateChangeSet{
				Accounts: []gigatypes.AccountChange{},
				Storage: []gigatypes.StorageChange{{
					Address: address,
					Key:     gigatypes.Hash(testSlot),
					Delete:  true,
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutations := []Mutation{tt.mutation}

			changes, err := MutationsToStateChangeSet(mutations, false, false)
			require.NoError(t, err)
			require.Equal(t, tt.withoutUnchanged, changes)

			changes, err = MutationsToStateChangeSet(mutations, true, false)
			require.NoError(t, err)
			require.Equal(t, tt.withUnchanged, changes)
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

			_, err := MutationsToStateChangeSet(mutations, false, false)
			require.Error(t, err)

			changes, err := MutationsToStateChangeSet(mutations, false, true)
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
				_, err := MutationsToStateChangeSet(mutations, false, ignoreUnsupportedKeys)
				require.Error(t, err, "ignoreUnsupportedKeys=%v", ignoreUnsupportedKeys)
			}
		})
	}
}
