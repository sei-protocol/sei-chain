package giga

import (
	"encoding/binary"
	"maps"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// undoTestHome holds the configs a StateDB over an undo-log SS reopens from.
type undoTestHome struct {
	flatkv *flatkvconfig.Config
	ss     config.StateStoreConfig
}

func newUndoTestHome(t *testing.T) undoTestHome {
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = true
	ssCfg.Backend = config.PebbleDBUndoBackend
	ssCfg.EVMDBDirectory = filepath.Join(t.TempDir(), "ss")
	ssCfg.ExternalPruning = true
	return undoTestHome{flatkv: flatkvconfig.DefaultTestConfig(t), ss: ssCfg}
}

func (h undoTestHome) open(t *testing.T) *StateDB {
	t.Helper()
	db, err := NewStateDB(t.Context(), h.flatkv, h.ss, config.CheckpointConfig{BlockInterval: 1_000})
	require.NoError(t, err)
	require.NotNil(t, db.UndoSS())
	require.Nil(t, db.SS())
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

// commitBlock commits one block writing each key, with the value each key held before it, as
// execution hands them over.
func (m *undoModel) commitBlock(t *testing.T, db *StateDB, writes map[string][]byte) {
	t.Helper()
	height := int64(len(m.states))
	prev := m.states[height-1]
	next := maps.Clone(prev)
	var pairs, prior []*proto.KVPair
	for key, value := range writes {
		old, found := prev[key]
		prior = append(prior, &proto.KVPair{Key: []byte(key), Value: old, Delete: !found})
		pairs = append(pairs, &proto.KVPair{Key: []byte(key), Value: value})
		next[key] = value
	}
	m.states = append(m.states, next)
	require.NoError(t, db.CommitStateChangesWithPrior(height,
		[]*proto.NamedChangeSet{{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: pairs}}}, prior))
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

func (m *undoModel) requireHistory(t *testing.T, db *StateDB, from, to int64, keyList [][]byte) {
	t.Helper()
	db.UndoSS().WaitForPendingWrites()
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

	// A clean close drains the log onto SC's height, so reopening keeps every block readable.
	db = home.open(t)
	require.Equal(t, int64(30), db.UndoSS().GetLatestVersion())
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
	db.UndoSS().WaitForPendingWrites()

	var pruned bool
	for _, store := range db.PrunableStores() {
		if store.Name() != db.UndoSS().Name() {
			continue
		}
		require.True(t, store.ExternalPruning())
		require.NoError(t, store.PruneHistory(11))
		pruned = true
	}
	require.True(t, pruned, "the undo log joins the prune cycle")
	require.Equal(t, int64(11), db.UndoSS().GetEarliestVersion())

	_, ok := db.OpenViewAt(10)
	require.False(t, ok, "history below the cut line is no longer served")
	model.requireHistory(t, db, 11, 20, [][]byte{nonceKey(addr)})
}

func TestUndoStateDBRefusesACommitWithoutPriorValues(t *testing.T) {
	db := newUndoTestHome(t).open(t)
	defer func() { require.NoError(t, db.Close()) }()
	require.True(t, db.NeedsPriorValues())

	changed := []*proto.NamedChangeSet{{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{
		Pairs: []*proto.KVPair{{Key: nonceKey(common.Address{1}), Value: nonceValue(1)}}}}}
	require.ErrorContains(t, db.CommitStateChanges(1, changed), "1 EVM keys written but 0 prior values given")

	// A block that writes no EVM key has no prior value to give.
	require.NoError(t, db.CommitStateChanges(1, []*proto.NamedChangeSet{{Name: keys.EVMStoreKey}}))
}

func TestUndoStateDBRollsBackByDiscardingLaterRecords(t *testing.T) {
	home := newUndoTestHome(t)
	db := home.open(t)
	addr, other := common.Address{9}, common.Address{10}
	model := &undoModel{states: []map[string][]byte{{}}}
	for block := 1; block <= 14; block++ {
		writes := map[string][]byte{string(nonceKey(addr)): nonceValue(uint64(block))}
		// other changes once below the rollback target and three times above it.
		if block == 3 || block == 10 || block == 11 || block == 13 {
			writes[string(nonceKey(other))] = nonceValue(uint64(block))
		}
		model.commitBlock(t, db, writes)
	}
	require.NoError(t, db.Close())

	// The rollback to 9 discards every record above it.
	db, err := NewStateDBWithRollback(t.Context(), home.flatkv, home.ss, config.CheckpointConfig{BlockInterval: 1_000}, 9)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	require.Equal(t, int64(9), db.UndoSS().GetLatestVersion())

	// The chain goes on differently from 9 and never writes other again, so a record of 11 or 13
	// left behind would show through as a change it no longer has.
	model.states = model.states[:10]
	for block := 10; block <= 16; block++ {
		model.commitBlock(t, db, map[string][]byte{string(nonceKey(addr)): nonceValue(uint64(100 + block))})
	}
	model.requireHistory(t, db, 0, 16, [][]byte{nonceKey(addr), nonceKey(other)})
}
