package evmonly

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// ReplayState returns the state view holds with req's transactions applied in order, as a block
// applies them: a transaction the block rejected changes nothing. The result is in memory only,
// reads through view, and belongs to the caller; it must not be used once view is closed. Its Error
// reports the first read of view that failed, even after a reverted snapshot.
func (e *Executor) ReplayState(ctx context.Context, view gigatypes.EVMStateView, req BlockRequest) (vm.StateDB, error) {
	reads := &failedRead{}
	stateDB := newNativeStateDB(failedReadRecorder{
		reader:  gigaSnapshotStateReader{snapshot: view, missingState: e.missingState},
		failure: reads,
	})
	replayed := &replayedStateDB{nativeStateDB: stateDB, reads: reads}
	if len(req.Txs) == 0 {
		return replayed, nil
	}
	prepared, err := e.PrepareBlock(ctx, req)
	if err != nil {
		return nil, err
	}
	chainConfig := e.chainConfig(prepared.Context)
	evm := vm.NewEVM(EVMBlockContext(prepared.Context), stateDB, chainConfig, vm.Config{}, e.customContracts)
	stateDB.SetEVM(evm)
	// executeTx does not watch ctx, so the EVM is stopped mid-transaction once ctx ends.
	stop := context.AfterFunc(ctx, evm.Cancel)
	defer stop()
	gasPool := new(core.GasPool).AddGas(prepared.Context.GasLimit)
	baseFee := cloneOptionalBig(prepared.Context.BaseFee)
	for txIndex, p := range prepared.Txs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, _, err := e.executeTx(evm, stateDB, gasPool, prepared.Context, p, txIndex, uint(txIndex), baseFee); err != nil { //nolint:gosec // G115: txIndex is non-negative.
			return nil, fmt.Errorf("replay tx %d %s: %w", txIndex, p.Tx.Hash(), err)
		}
	}
	// A canceled EVM stops a transaction part way, leaving a state no block produced.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return replayed, nil
}

// replayedStateDB is a replayed state whose Error also reports a failed read of the state under it.
type replayedStateDB struct {
	*nativeStateDB
	reads *failedRead
}

// Error returns the first read of the underlying state that failed, or else the state's own error.
func (s *replayedStateDB) Error() error {
	if s.reads.err != nil {
		return s.reads.err
	}
	return s.nativeStateDB.Error()
}

// failedRead holds the first read of a state that failed. Unlike nativeStateDB's own error, a
// reverted snapshot does not clear it.
type failedRead struct {
	err error
}

// failedReadRecorder serves reads from reader and records in failure the first one that panics. The
// panic continues, so the execution making the read stops where it would have.
type failedReadRecorder struct {
	reader  gigaSnapshotStateReader
	failure *failedRead
}

func (r failedReadRecorder) GetBalance(addr common.Address) *big.Int {
	defer r.record()
	return r.reader.GetBalance(addr)
}

func (r failedReadRecorder) GetNonce(addr common.Address) uint64 {
	defer r.record()
	return r.reader.GetNonce(addr)
}

func (r failedReadRecorder) GetCode(addr common.Address) []byte {
	defer r.record()
	return r.reader.GetCode(addr)
}

func (r failedReadRecorder) GetState(addr common.Address, key common.Hash) common.Hash {
	defer r.record()
	return r.reader.GetState(addr, key)
}

// ReadAccount satisfies baseAccountReader, as reader does.
func (r failedReadRecorder) ReadAccount(addr common.Address) (AccountState, bool) {
	defer r.record()
	return r.reader.ReadAccount(addr)
}

// record records a read that panicked and lets the panic continue. It must be deferred by the read.
func (r failedReadRecorder) record() {
	if p := recover(); p != nil {
		if r.failure.err == nil {
			r.failure.err = fmt.Errorf("read the EVM state: %v", p)
		}
		panic(p)
	}
}
