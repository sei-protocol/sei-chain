package evmonlyapp

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	otelmetric "go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const (
	// preparedBlocksMetricName counts finalized blocks by whether PrepareBlock had decoded them.
	preparedBlocksMetricName = "evmonly_finalize_prepared_blocks_total"
	prepareTimerName         = "evmonly_prepare"

	preparePhaseParse = "parse"
)

// preparedBlock is the stateless part of a FinalizeBlock request, computed before the
// request arrives. FinalizeBlock uses it only for the block with this height and hash.
type preparedBlock struct {
	height int64
	hash   common.Hash
	txs    []evmonly.PreparedTx
}

func newPreparedBlocksCounter(meter otelmetric.Meter) otelmetric.Int64Counter {
	counter, err := meter.Int64Counter(
		preparedBlocksMetricName,
		otelmetric.WithDescription("Finalized blocks by whether PrepareBlock had already decoded them"),
	)
	if err != nil {
		panic(fmt.Sprintf("%s: %v", preparedBlocksMetricName, err))
	}
	return counter
}

// PrepareBlock decodes a block's transactions and recovers their senders before
// FinalizeBlock is called for it, so that work runs while the previous block
// executes. It may run concurrently with FinalizeBlock. Only the most recent
// prepared block is kept, and FinalizeBlock uses it only for the same height and
// hash, so preparing the wrong block costs nothing but the work: the senders
// CheckTx cached stay cached until a prepared block is consumed. Anything that
// would fail the block is left for FinalizeBlock to report; the only error
// returned is ctx ending.
func (a *evmOnlyApplication) PrepareBlock(ctx context.Context, req *abci.RequestFinalizeBlock) error {
	executor, ok := a.settler.Load().Get()
	if !ok {
		return nil
	}
	block, err := parseFinalizeRequest(req)
	if err != nil {
		return nil
	}
	// Only Number and Time reach the decoded transactions (through the signer); the
	// parent-derived fields are filled in by FinalizeBlock.
	a.preparePhases.SetPhase(preparePhaseParse)
	prepared, err := executor.PrepareBlock(ctx, evmonly.BlockRequest{
		Context: evmonly.BlockContext{
			Number:      block.number,
			Time:        block.timestamp,
			GasLimit:    a.EvmGasLimit(),
			ChainID:     new(big.Int).Set(a.chainID),
			BaseFee:     evmOnlyBaseFee(),
			BlobBaseFee: new(big.Int),
		},
		Txs:     req.Txs,
		Senders: a.peekSenders(req.Txs),
	})
	a.preparePhases.Reset()
	if err != nil {
		return ctx.Err()
	}
	for slot := range a.prepared.Lock() {
		*slot = utils.Some(preparedBlock{
			height: block.height,
			hash:   block.blockHash,
			txs:    prepared.Txs,
		})
	}
	return nil
}

// takePrepared returns the prepared transactions of the given block and removes
// them. A prepared block for another block is left in place.
func (a *evmOnlyApplication) takePrepared(height int64, hash common.Hash) ([]evmonly.PreparedTx, bool) {
	for slot := range a.prepared.Lock() {
		prepared, ok := slot.Get()
		if !ok || prepared.height != height || prepared.hash != hash {
			return nil, false
		}
		*slot = utils.None[preparedBlock]()
		return prepared.txs, true
	}
	panic("unreachable")
}
