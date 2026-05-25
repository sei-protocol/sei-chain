package producer
	
import (
	"context"
	"fmt"
	"time"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/ethereum/go-ethereum/common"
	ttypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

type tx struct {
	tx           ttypes.Tx
	hash         ttypes.TxHash
	gasEstimated int
	gasWanted    int
}

type evmTx struct {
	*tx
	evmNonce uint64
	evmAddress common.Address
	seiAddress []byte
}

type evmAccount struct {
	nonce uint64
	txs []*evmTx
}

// (addr,nonce) -> tx
// tracking of what is in progress
// on startup
// * read data.State and avail.State from executed until the end (even across gaps)
// * parse all of these transactions
// * consider only our lane blocks (we are guaranteed to have all of our lane blocks)
// * we are interested only in evm nonces - ignore txs with nonces after a gap
// every time execution progresses
// * we check if nonces progressed as expected.
// * if not - just drop all the non-included txs of the given address
// for testnet
// * accept only ready txs
// * don't drop ready txs (unless some tx was unexpectedly dropped)
// * drop over capacity.
// TODO: limit the lag between lane head and local execution
// TODO: make sure that we query nonce at height > expected height
//   this way our check will be an approximation from below
type Mempool struct {
	app     *proxy.Proxy
	capacity uint64
	size uint64
	cosmosTxs []*tx
	// expected evm account states after the given block
	// used to ev
	blocks queue[types.BlockNumber, map[common.Address]uint64]
	evmAccounts map[common.Address]*evmAccount
}

func NewMempool(app *proxy.Proxy, capacity uint64) *Mempool {
	return &Mempool {
		capacity: capacity,
		evmAccounts: map[common.Address]*evmAccount{},
	}
}

type ReapLimits struct {
	MaxTxs          utils.Option[uint64]
	MaxBytes        utils.Option[int64] // Max total bytes in proto representation.
	MaxGasWanted    utils.Option[int64]
	MaxGasEstimated utils.Option[int64]
}

func (m *Mempool) EvmNextPendingNonce(addr common.Address) uint64 {
	panic("TODO")
}

func (m *Mempool) Insert(ctx context.Context, tx ttypes.Tx) (*abci.ResponseCheckTx, error) {
	panic("TODO")
}

// Reaps a non-empty set of ready txs.
func (m *Mempool) ReapTxs(ctx context.Context, limits ReapLimits) (*types.Payload, error) {
	payloadTxs := make([][]byte, 0, len(txs))
	for _, tx := range txs {
		payloadTxs = append(payloadTxs, tx)
	}
	payload, err := types.PayloadBuilder{
		CreatedAt: time.Now(),
		TotalGas:  uint64(gasEstimated), // nolint:gosec // always non-negative
		Txs:       payloadTxs,
	}.Build()
	if err != nil {
		// This should never happen: we construct the payload from correctly sized data.
		panic(fmt.Errorf("PayloadBuilder{}.Build(): %w", err))
	}
	return payload, nil
}

func (m *Mempool) MarkExecuted(ctx context.Context, n types.BlockNumber) error {
	panic("TODO")
}
