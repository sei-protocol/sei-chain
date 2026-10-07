package node

import (
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
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
	state := &mockAppState{nextNonce: map[common.Address]uint64{}, lastBlockAppHash: make([]byte, 32)}
	dirty := map[common.Address]struct{}{}
	for i := range 2000 {
		addr := common.BytesToAddress([]byte{byte(i >> 8), byte(i)})
		state.nextNonce[addr] = uint64(i)
		dirty[addr] = struct{}{}
	}
	b.ResetTimer()
	for i := range b.N {
		state.lastBlockHeight = int64(i) + 1
		if err := store.save(state, dirty, false); err != nil {
			b.Fatal(err)
		}
	}
}
