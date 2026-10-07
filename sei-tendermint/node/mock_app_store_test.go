package node

import (
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func openTestMockApp(t *testing.T, inner abci.Application, dir string) *MockApp {
	t.Helper()
	app, err := OpenMockApp(inner, dir)
	require.NoError(t, err)
	return app
}

func TestMockAppResumesAfterRestart(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)
	tx0, _, addr := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	tx1, _, _ := buildFastCheckTxBytesForKey(t, key, 1, 21_000)
	tx2, _, _ := buildFastCheckTxBytesForKey(t, key, 2, 21_000)
	dir := filepath.Join(t.TempDir(), "mockapp")
	validators := []abci.ValidatorUpdate{{Power: 7}, {Power: 9}}

	app := openTestMockApp(t, &mockAppValidatorsTarget{validators: validators}, dir)
	_, err = app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)
	for h, tx := range [][]byte{tx0, tx1} {
		_, err = app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
			Txs:    [][]byte{tx},
			Header: &tmproto.Header{Height: int64(h) + 1},
			Hash:   []byte{byte(h)},
		})
		require.NoError(t, err)
		_, err = app.Commit(t.Context())
		require.NoError(t, err)
	}
	wantHash := app.Info().LastBlockAppHash
	require.NoError(t, app.Close())

	restarted := openTestMockApp(t, abci.BaseApplication{}, dir)
	t.Cleanup(func() { _ = restarted.Close() })
	require.Equal(t, int64(2), restarted.Info().LastBlockHeight)
	require.Equal(t, wantHash, restarted.Info().LastBlockAppHash)
	require.Equal(t, uint64(2), restarted.EvmNonce(addr))
	require.Equal(t, validators, restarted.GetValidators())

	res, err := restarted.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:    [][]byte{tx2},
		Header: &tmproto.Header{Height: 3},
	})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, res.TxResults[0].Code)
}

func TestMockAppRestartDropsUncommittedBlock(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)
	tx0, _, addr := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	dir := filepath.Join(t.TempDir(), "mockapp")

	app := openTestMockApp(t, abci.BaseApplication{}, dir)
	_, err = app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)
	_, err = app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Header: &tmproto.Header{Height: 1}})
	require.NoError(t, err)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
	_, err = app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:    [][]byte{tx0},
		Header: &tmproto.Header{Height: 2},
	})
	require.NoError(t, err)
	require.NoError(t, app.Close())

	restarted := openTestMockApp(t, abci.BaseApplication{}, dir)
	t.Cleanup(func() { _ = restarted.Close() })
	require.Equal(t, int64(1), restarted.LastBlockHeight())
	require.Equal(t, uint64(0), restarted.EvmNonce(addr))
}

func TestMockAppEmptyStoreStartsFresh(t *testing.T) {
	app := openTestMockApp(t, abci.BaseApplication{}, filepath.Join(t.TempDir(), "mockapp"))
	t.Cleanup(func() { _ = app.Close() })
	require.Equal(t, int64(0), app.LastBlockHeight())
	_, err := app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)
}

// BenchmarkMockAppStoreSave measures the per-block save of a full lane block's nonces.
func BenchmarkMockAppStoreSave(b *testing.B) {
	store, err := openMockAppStore(filepath.Join(b.TempDir(), "mockapp"))
	require.NoError(b, err)
	b.Cleanup(func() { _ = store.Close() })
	snap := mockAppSnapshot{appHash: make([]byte, 32), nonces: map[common.Address]uint64{}}
	for i := range 2000 {
		snap.nonces[common.BytesToAddress([]byte{byte(i >> 8), byte(i)})] = uint64(i)
	}
	b.ResetTimer()
	for i := range b.N {
		snap.height = int64(i) + 1
		if err := store.save(snap); err != nil {
			b.Fatal(err)
		}
	}
}

func TestMockAppSnapshotMergeNewerKeepsLatestValues(t *testing.T) {
	a, b, c := common.Address{1}, common.Address{2}, common.Address{3}
	older := mockAppSnapshot{
		height:     5,
		appHash:    []byte("h5"),
		nonces:     map[common.Address]uint64{a: 1, b: 4},
		validators: utils.Some([]abci.ValidatorUpdate{{Power: 7}}),
	}
	older.mergeNewer(mockAppSnapshot{height: 6, appHash: []byte("h6"), nonces: map[common.Address]uint64{b: 5, c: 2}})

	require.Equal(t, int64(6), older.height)
	require.Equal(t, []byte("h6"), older.appHash)
	require.Equal(t, map[common.Address]uint64{a: 1, b: 5, c: 2}, older.nonces)
	validators, ok := older.validators.Get()
	require.True(t, ok)
	require.Equal(t, []abci.ValidatorUpdate{{Power: 7}}, validators)
}

func TestMockAppSaverMergesQueuedSnapshots(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mockapp")
	store, err := openMockAppStore(dir)
	require.NoError(t, err)
	saver := newMockAppSaver(store)
	a, b := common.Address{1}, common.Address{2}
	saver.put(mockAppSnapshot{height: 1, appHash: []byte("h1"), nonces: map[common.Address]uint64{a: 1},
		validators: utils.Some([]abci.ValidatorUpdate{{Power: 7}})})
	saver.put(mockAppSnapshot{height: 2, appHash: []byte("h2"), nonces: map[common.Address]uint64{b: 1}})
	saver.put(mockAppSnapshot{height: 3, appHash: []byte("h3"), nonces: map[common.Address]uint64{a: 2}})
	require.NoError(t, saver.Close())

	store, err = openMockAppStore(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	state := &mockAppState{nextNonce: map[common.Address]uint64{}}
	restored, err := store.load(state)
	require.NoError(t, err)
	require.True(t, restored)
	require.Equal(t, int64(3), state.lastBlockHeight)
	require.Equal(t, []byte("h3"), state.lastBlockAppHash)
	require.Equal(t, map[common.Address]uint64{a: 2, b: 1}, state.nextNonce)
	require.Equal(t, []abci.ValidatorUpdate{{Power: 7}}, state.validators)
}
