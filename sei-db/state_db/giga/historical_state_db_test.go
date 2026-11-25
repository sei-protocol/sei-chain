package giga

import (
	"encoding/binary"
	"maps"
	"math/big"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// undoTestHome holds the configs a StateDB over an undo-log historical state DB reopens from.
type undoTestHome struct {
	flatkv *flatkvconfig.Config
	ss     config.StateStoreConfig
}

func newUndoTestHome(t *testing.T) undoTestHome {
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = true
	ssCfg.EVMDBDirectory = filepath.Join(t.TempDir(), "ss")
	ssCfg.ExternalPruning = true
	return undoTestHome{flatkv: flatkvconfig.DefaultTestConfig(t), ss: ssCfg}
}

func (h undoTestHome) open(t *testing.T) *StateDB {
	t.Helper()
	db, err := assembleStateDB(t, h, h.ss, true)
	require.NoError(t, err)
	require.NotNil(t, db.HistoricalStateDB())
	return db
}

// undoModel is the reference: every key's value after each block, as a current-state view reports it.
type undoModel struct {
	states []map[string][]byte
}

func (m *undoModel) at(height int64, key []byte) ([]byte, bool) {
	v, ok := m.states[height][string(key)]
	return v, ok
}

// commitBlock commits one block writing each nonce or storage key, with the block's change set
// carrying the value each key held before it, as execution hands them over.
func (m *undoModel) commitBlock(t *testing.T, db *StateDB, writes map[string][]byte) {
	t.Helper()
	height := int64(len(m.states))
	prev := m.states[height-1]
	next := maps.Clone(prev)
	var changes gigatypes.StateChangeSet
	for key, value := range writes {
		old := prev[key]
		if key[0] == nonceKey(common.Address{})[0] {
			changes.Accounts = append(changes.Accounts, gigatypes.AccountChange{
				Address: common.BytesToAddress([]byte(key[1:])),
				Account: gigatypes.AccountState{Nonce: nonceOf(value)},
				Prior:   gigatypes.AccountState{Nonce: nonceOf(old)}})
		} else {
			changes.Storage = append(changes.Storage, gigatypes.StorageChange{
				Address: common.BytesToAddress([]byte(key[1:21])), Key: common.BytesToHash([]byte(key[21:])),
				Value: common.BytesToHash(value), PriorValue: common.BytesToHash(old)})
		}
		next[key] = value
	}
	m.states = append(m.states, next)
	require.NoError(t, db.CommitStateChanges(height, &changes))
}

func storageKey(addr common.Address, slot byte) []byte {
	var slotHash common.Hash
	slotHash[31] = slot
	key := make([]byte, 1+len(addr)+len(slotHash))
	keys.PutEVMKey(key, keys.EVMKeyStorage, addr[:], slotHash[:])
	return key
}

func nonceKey(addr common.Address) []byte {
	return keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
}

func nonceValue(nonce uint64) []byte {
	v := make([]byte, 8)
	binary.BigEndian.PutUint64(v, nonce)
	return v
}

// nonceChanges is a change set moving addr's nonce from prior to nonce.
func nonceChanges(addr common.Address, prior uint64, nonce uint64) *gigatypes.StateChangeSet {
	return &gigatypes.StateChangeSet{Accounts: []gigatypes.AccountChange{{
		Address: addr,
		Account: gigatypes.AccountState{Nonce: nonce},
		Prior:   gigatypes.AccountState{Nonce: prior},
	}}}
}

// nonceOf decodes a nonce value, 0 for none.
func nonceOf(value []byte) uint64 {
	if value == nil {
		return 0
	}
	return binary.BigEndian.Uint64(value)
}

func (m *undoModel) requireHistory(t *testing.T, db *StateDB, from, to int64, keyList [][]byte) {
	t.Helper()
	db.HistoricalStateDB().WaitForPendingWrites()
	for h := from; h <= to; h++ {
		view, ok := db.OpenViewAt(h)
		require.True(t, ok, "height %d", h)
		require.Equal(t, h, view.GetBlockHeight())
		for _, key := range keyList {
			want, wantFound := m.at(h, key)
			got, found := view.Get(keys.EVMStoreKey, key)
			require.Equal(t, wantFound, found, "key %x at %d", key, h)
			require.Equal(t, want, got, "key %x at %d", key, h)
		}
		view.Close()
	}
}

func TestUndoStateDBServesHistoryAcrossReopen(t *testing.T) {
	home := newUndoTestHome(t)
	db := home.open(t)
	rng := rand.New(rand.NewPCG(1, 1))

	addrs := []common.Address{{1}, {2}, {3}}
	var keyList [][]byte
	for _, addr := range addrs {
		keyList = append(keyList, nonceKey(addr), storageKey(addr, 1), storageKey(addr, 2))
	}
	model := &undoModel{states: []map[string][]byte{{}}}
	for block := 1; block <= 30; block++ {
		writes := map[string][]byte{}
		for range 1 + rng.IntN(3) {
			addr := addrs[rng.IntN(len(addrs))]
			writes[string(nonceKey(addr))] = nonceValue(uint64(block))
			writes[string(storageKey(addr, byte(1+rng.IntN(2))))] = common.Hash{byte(block), 0xff}.Bytes()
		}
		model.commitBlock(t, db, writes)
	}
	model.requireHistory(t, db, 0, 30, keyList)

	view, ok := db.OpenViewAt(12)
	require.True(t, ok)
	want, _ := model.at(12, nonceKey(addrs[0]))
	require.Equal(t, binary.BigEndian.Uint64(append(make([]byte, 8-len(want)), want...)), view.GetNonce(addrs[0]))
	view.Close()

	_, ok = db.OpenViewAt(31)
	require.False(t, ok, "a height above the applied head is not served")
	require.NoError(t, db.Close())

	// A clean close drains the log onto the live state DB's height, so reopening keeps every block readable.
	db = home.open(t)
	require.Equal(t, int64(30), db.HistoricalStateDB().GetLatestVersion())
	model.requireHistory(t, db, 0, 30, keyList)
	model.commitBlock(t, db, map[string][]byte{string(nonceKey(addrs[1])): nonceValue(99)})
	model.requireHistory(t, db, 25, 31, keyList)
	require.NoError(t, db.Close())
}

func TestUndoStateDBPrunesThroughTheCollectorInterface(t *testing.T) {
	home := newUndoTestHome(t)
	db := home.open(t)
	defer func() { require.NoError(t, db.Close()) }()
	addr := common.Address{7}
	model := &undoModel{states: []map[string][]byte{{}}}
	for block := 1; block <= 20; block++ {
		model.commitBlock(t, db, map[string][]byte{string(nonceKey(addr)): nonceValue(uint64(block))})
	}
	db.HistoricalStateDB().WaitForPendingWrites()

	var pruned bool
	for _, store := range db.PrunableStores() {
		if store.Name() != db.HistoricalStateDB().Name() {
			continue
		}
		require.True(t, store.ExternalPruning())
		require.NoError(t, store.PruneHistory(11))
		pruned = true
	}
	require.True(t, pruned, "the undo log joins the prune cycle")
	require.Equal(t, int64(11), db.HistoricalStateDB().GetEarliestVersion())

	_, ok := db.OpenViewAt(10)
	require.False(t, ok, "history below the cut line is no longer served")
	model.requireHistory(t, db, 11, 20, [][]byte{nonceKey(addr)})
}

// A read below a block serves each balance and code the block replaced.
func TestUndoStateDBServesPriorBalanceAndCode(t *testing.T) {
	db := newUndoTestHome(t).open(t)
	defer func() { require.NoError(t, db.Close()) }()
	addr := common.Address{5}
	oldCode, newCode := []byte{0x60, 0x00}, []byte{0x60, 0x01}

	require.NoError(t, db.CommitStateChanges(1, &gigatypes.StateChangeSet{
		Accounts: []gigatypes.AccountChange{{
			Address: addr,
			Account: gigatypes.AccountState{Balance: big.NewInt(4), Code: oldCode},
		}},
	}))
	require.NoError(t, db.CommitStateChanges(2, &gigatypes.StateChangeSet{
		Accounts: []gigatypes.AccountChange{{
			Address: addr,
			Account: gigatypes.AccountState{Balance: big.NewInt(9), Code: newCode},
			Prior:   gigatypes.AccountState{Balance: big.NewInt(4), Code: oldCode},
		}},
	}))
	db.HistoricalStateDB().WaitForPendingWrites()

	for height, want := range map[int64]struct {
		balance int64
		code    []byte
	}{1: {4, oldCode}, 2: {9, newCode}} {
		view, ok := db.OpenViewAt(height)
		require.True(t, ok)
		require.Equal(t, common.BigToHash(big.NewInt(want.balance)), view.GetBalance(addr), "height %d", height)
		require.Equal(t, want.code, view.GetCode(addr), "height %d", height)
		require.Equal(t, crypto.Keccak256Hash(want.code), view.GetCodeHash(addr), "height %d", height)
		view.Close()
	}
}

// The undo log keeps the history of every module the live state DB holds, not only the EVM one.
func TestUndoStateDBServesHistoryOutsideTheEVMModule(t *testing.T) {
	db := newUndoTestHome(t).open(t)
	defer func() { require.NoError(t, db.Close()) }()
	addr := common.Address{3}
	require.NoError(t, db.CommitStateChanges(1, nonceChanges(addr, 0, 1)))
	writeHead := func(value byte) *gigatypes.StateChangeSet {
		return &gigatypes.StateChangeSet{
			Misc: []gigatypes.MiscChange{{Module: "cursor", Key: []byte("head"), Value: []byte{value}}},
		}
	}
	require.NoError(t, db.CommitStateChanges(2, writeHead(2)))
	require.NoError(t, db.CommitStateChanges(3, writeHead(3)))
	db.HistoricalStateDB().WaitForPendingWrites()

	require.Equal(t, int64(3), db.HistoricalStateDB().GetLatestVersion())
	for height, want := range []struct {
		nonce uint64
		head  []byte
	}{{0, nil}, {1, nil}, {1, []byte{2}}, {1, []byte{3}}} {
		view, ok := db.OpenViewAt(int64(height))
		require.True(t, ok, "height %d", height)
		require.Equal(t, want.nonce, view.GetNonce(addr), "height %d", height)
		head, found := view.Get("cursor", []byte("head"))
		require.Equal(t, want.head != nil, found, "height %d", height)
		require.Equal(t, want.head, head, "height %d", height)
		view.Close()
	}
}

// Seeding an empty live state DB to a later height, as InitChain does for a chain that starts above block 1, leaves
// the undo log to resume on the seeded height at the first block.
func TestUndoStateDBResumesOnASeededHeight(t *testing.T) {
	db := newUndoTestHome(t).open(t)
	defer func() { require.NoError(t, db.Close()) }()
	require.NoError(t, db.LiveStateDB().SetInitialVersion(5))

	addr := common.Address{4}
	for block := int64(5); block <= 6; block++ {
		nonce := uint64(block - 4) //nolint:gosec // positive
		require.NoError(t, db.CommitStateChanges(block, nonceChanges(addr, nonce-1, nonce)))
	}
	db.HistoricalStateDB().WaitForPendingWrites()

	require.Equal(t, int64(4), db.HistoricalStateDB().GetEarliestVersion())
	for height, want := range map[int64]uint64{4: 0, 5: 1, 6: 2} {
		view, ok := db.OpenViewAt(height)
		require.True(t, ok, "height %d", height)
		require.Equal(t, want, view.GetNonce(addr), "height %d", height)
		view.Close()
	}
}

// An undo log that holds blocks over an empty live state DB, as left by wiping the live state DB alone, is
// refused at open.
func TestUndoStateDBRefusesAnUndoLogOverAnEmptyLiveStateDB(t *testing.T) {
	home := newUndoTestHome(t)
	db := home.open(t)
	model := &undoModel{states: []map[string][]byte{{}}}
	model.commitBlock(t, db, map[string][]byte{string(nonceKey(common.Address{8})): nonceValue(1)})
	require.NoError(t, db.Close())
	require.NoError(t, os.RemoveAll(home.flatkv.DataDir))

	_, err := assembleStateDB(t, home, home.ss, true)
	require.ErrorContains(t, err, "the live state DB is empty")
}

// Closing with a historical view still open succeeds without waiting for it, and the view's reads
// fail from then on.
func TestUndoStateDBClosesWithAHistoricalViewOpen(t *testing.T) {
	db := newUndoTestHome(t).open(t)
	addr := common.Address{6}
	model := &undoModel{states: []map[string][]byte{{}}}
	model.commitBlock(t, db, map[string][]byte{string(nonceKey(addr)): nonceValue(1)})
	model.commitBlock(t, db, map[string][]byte{string(nonceKey(addr)): nonceValue(2)})
	db.HistoricalStateDB().WaitForPendingWrites()
	view, ok := db.OpenViewAt(1)
	require.True(t, ok)
	require.Equal(t, uint64(1), view.GetNonce(addr))

	require.NoError(t, db.Close())
	require.Panics(t, func() { view.GetNonce(addr) }, "a view has no recoverable errors")
	view.Close()
}

// A block that changes nothing has nothing for the undo log to record, and still commits.
func TestUndoStateDBCommitsAnEmptyChangeSet(t *testing.T) {
	db := newUndoTestHome(t).open(t)
	defer func() { require.NoError(t, db.Close()) }()

	require.NoError(t, db.CommitStateChanges(1, &gigatypes.StateChangeSet{}),
		"a block that changes nothing has nothing to record")
}
