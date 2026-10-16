package evmonlyapp

import (
	"cmp"
	"context"
	"fmt"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	otelmetric "go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

const (
	// preparedBlocksMetricName counts finalized blocks by whether PrepareBlock had decoded them.
	preparedBlocksMetricName = "evmonly_finalize_prepared_blocks_total"
	prepareTimerName         = "evmonly_prepare"

	preparePhaseParse = "parse"

	// maxPreparedBlocks bounds the prepared blocks held: the block about to execute
	// and the one after it.
	maxPreparedBlocks = 2
)

// preparedBlock is the stateless part of a FinalizeBlock request, computed before the
// request arrives. FinalizeBlock uses it only for the block with this height and hash.
type preparedBlock struct {
	height int64
	hash   common.Hash
	txs    []evmonly.PreparedTx
	// txHashes are the hashes of the block's raw transactions, which key the sender cache.
	txHashes []common.Hash
}

// preparedQueue holds prepared blocks in height order, at most one per height and
// at most maxPreparedBlocks in all.
type preparedQueue []preparedBlock

// put adds b and drops every block below next, the lowest height that can still
// execute. A block of the same height is replaced. When the queue is full it keeps
// the lowest heights, since they execute first.
func (q *preparedQueue) put(b preparedBlock, next int64) {
	kept := slices.DeleteFunc(*q, func(p preparedBlock) bool { return p.height < next || p.height == b.height })
	kept = append(kept, b)
	slices.SortFunc(kept, func(x, y preparedBlock) int { return cmp.Compare(x.height, y.height) })
	n := min(len(kept), maxPreparedBlocks)
	clear(kept[n:])
	*q = kept[:n]
}

// take removes and returns the block with this height and hash, and drops every
// block below height. A prepared block for another hash at height is left in place.
func (q *preparedQueue) take(height int64, hash common.Hash) (preparedBlock, bool) {
	*q = slices.DeleteFunc(*q, func(p preparedBlock) bool { return p.height < height })
	i := slices.IndexFunc(*q, func(p preparedBlock) bool { return p.height == height && p.hash == hash })
	if i < 0 {
		return preparedBlock{}, false
	}
	b := (*q)[i]
	*q = slices.Delete(*q, i, i+1)
	return b, true
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
// executes. It may run concurrently with FinalizeBlock. A prepared block is kept,
// at most maxPreparedBlocks at a time, until FinalizeBlock takes it or its height
// can no longer execute, and FinalizeBlock uses it only for the same height and
// hash. Preparing the wrong block costs only the work, and the senders CheckTx
// cached stay cached until a prepared block is consumed. Anything that would fail
// the block is left for FinalizeBlock to report; the only error returned is ctx
// ending.
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
	txHashes := hashRawTxs(req.Txs)
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
		Senders: a.peekSenders(txHashes),
	})
	a.preparePhases.Reset()
	if err != nil {
		return ctx.Err()
	}
	next := a.LastBlockHeight() + 1
	for queue := range a.prepared.Lock() {
		queue.put(preparedBlock{
			height:   block.height,
			hash:     block.blockHash,
			txs:      prepared.Txs,
			txHashes: txHashes,
		}, next)
	}
	return nil
}

// takePrepared returns the prepared block with the given height and hash and
// removes it. A prepared block for another hash is left in place.
func (a *evmOnlyApplication) takePrepared(height int64, hash common.Hash) (preparedBlock, bool) {
	for queue := range a.prepared.Lock() {
		return queue.take(height, hash)
	}
	panic("unreachable")
}
