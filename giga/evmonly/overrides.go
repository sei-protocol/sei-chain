package evmonly

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/export"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// CallOverrides are the account and block fields a read-only execution replaces before it runs, as
// eth_call's state and block override arguments name them. The zero value replaces nothing.
type CallOverrides struct {
	State export.StateOverride
	Block utils.Option[export.BlockOverrides]
}

// blockContext returns the go-ethereum context blockCtx executes at, with the block overrides applied.
func (o CallOverrides) blockContext(blockCtx BlockContext) (vm.BlockContext, error) {
	evmCtx := EVMBlockContext(blockCtx)
	if block, ok := o.Block.Get(); ok {
		if err := block.Apply(&evmCtx); err != nil {
			return vm.BlockContext{}, err
		}
	}
	return evmCtx, nil
}

// applyState applies the state overrides to stateDB, and has evm run a precompile an override moves
// at its new address. custom are the precompiles evm runs beside go-ethereum's.
func (o CallOverrides) applyState(evm *vm.EVM, stateDB vm.StateDB, custom map[common.Address]vm.PrecompiledContract) error {
	if len(o.State) == 0 {
		return nil
	}
	rules := evm.ChainConfig().Rules(evm.Context.BlockNumber, evm.Context.Random != nil, evm.Context.Time)
	precompiles := vm.ActivePrecompiledContracts(rules, custom)
	if err := o.State.Apply(stateDB, precompiles); err != nil {
		return err
	}
	evm.SetPrecompiles(precompiles)
	return nil
}
