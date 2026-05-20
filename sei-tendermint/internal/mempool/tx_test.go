package mempool

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type txStoreTestApp struct {
	abci.BaseApplication
}

func (txStoreTestApp) EvmNonce(common.Address) uint64 {
	return 0
}

func (txStoreTestApp) EvmBalance(common.Address, []byte) *big.Int {
	return big.NewInt(0)
}

func newTxStoreForTest() *txStore {
	return NewTxStore(TestConfig(), proxy.New(txStoreTestApp{}, proxy.NopMetrics()))
}

func TestTxStore_GetTxByHash(t *testing.T) {
	txs := newTxStoreForTest()
	wtx := &WrappedTx{
		hashedTx:  newHashedTx(types.Tx("test_tx")),
		priority:  1,
		timestamp: time.Now(),
	}

	key := wtx.Hash()
	res := txs.ByHash(key)
	require.Nil(t, res)

	txs.Insert(wtx)

	res = txs.ByHash(key)
	require.NotNil(t, res)
	require.Equal(t, wtx, res)
}

func TestTxStore_SetTx(t *testing.T) {
	txs := newTxStoreForTest()
	wtx := &WrappedTx{
		hashedTx:  newHashedTx(types.Tx("test_tx")),
		priority:  1,
		timestamp: time.Now(),
	}

	key := wtx.Hash()
	txs.Insert(wtx)

	res := txs.ByHash(key)
	require.NotNil(t, res)
	require.Equal(t, wtx, res)
}

func TestTxStore_Size(t *testing.T) {
	txStore := newTxStoreForTest()
	numTxs := 1000

	for i := range numTxs {
		txStore.Insert(&WrappedTx{
			hashedTx:  newHashedTx(fmt.Appendf(nil, "test_tx_%d", i)),
			priority:  int64(i),
			timestamp: time.Now(),
		})
	}

	require.Equal(t, numTxs, txStore.State().total.count)
}
