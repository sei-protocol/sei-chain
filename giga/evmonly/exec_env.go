package evmonly

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// blockExecEnv is the block-constant input to transaction execution: the chain
// rules in force, the EVM block context, and the precompiles resolvable at this
// height.
type blockExecEnv struct {
	chainConfig *params.ChainConfig
	blockCtx    vm.BlockContext
	rules       params.Rules
	precompiles map[common.Address]vm.PrecompiledContract
	customOnly  map[common.Address]vm.PrecompiledContract
}

func (e *Executor) newBlockExecEnv(ctx BlockContext, custom map[common.Address]vm.PrecompiledContract) *blockExecEnv {
	chainConfig := e.chainConfig(ctx)
	blockCtx := buildBlockContext(ctx)
	rules := chainConfig.Rules(blockCtx.BlockNumber, blockCtx.Random != nil, blockCtx.Time)
	return &blockExecEnv{
		chainConfig: chainConfig,
		blockCtx:    blockCtx,
		rules:       rules,
		precompiles: vm.ActivePrecompiledContracts(rules, custom),
		customOnly:  custom,
	}
}

// txEVM hands out the EVM for a state database, building it on first use. A
// transaction that takes the plain-transfer path never asks for one.
type txEVM struct {
	env     *blockExecEnv
	stateDB *nativeStateDB
	evm     *vm.EVM
}

func newTxEVM(env *blockExecEnv, stateDB *nativeStateDB) *txEVM {
	return &txEVM{env: env, stateDB: stateDB}
}

func (t *txEVM) get() *vm.EVM {
	if t.evm == nil {
		t.evm = vm.NewEVM(t.env.blockCtx, t.stateDB, t.env.chainConfig, vm.Config{}, t.env.customOnly)
		t.stateDB.SetEVM(t.evm)
	}
	return t.evm
}
