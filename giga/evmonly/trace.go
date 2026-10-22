package evmonly

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// ReplayState returns the state view holds with req's transactions applied in order, as a block
// applies them: a transaction the block rejected changes nothing. The result is in memory only,
// reads through view, and belongs to the caller; it must not be used once view is closed.
func (e *Executor) ReplayState(ctx context.Context, view gigatypes.EVMStateView, req BlockRequest) (vm.StateDB, error) {
	stateDB := newNativeStateDB(gigaSnapshotStateReader{snapshot: view, missingState: e.missingState})
	if len(req.Txs) == 0 {
		return stateDB, nil
	}
	prepared, err := e.PrepareBlock(ctx, req)
	if err != nil {
		return nil, err
	}
	chainConfig := e.chainConfig(prepared.Context)
	evm := vm.NewEVM(EVMBlockContext(prepared.Context), stateDB, chainConfig, vm.Config{}, customPrecompileMap(e.cfg.CustomPrecompiles))
	stateDB.SetEVM(evm)
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
	return stateDB, nil
}
