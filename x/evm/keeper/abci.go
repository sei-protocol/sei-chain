package keeper

import (
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

func (k *Keeper) BeginBlock(ctx sdk.Context) {
	// clear tx/tx responses from last block
	if !ctx.IsTracing() {
		k.SetMsgs([]*types.MsgEVMTransaction{})
		k.SetTxResults([]*abci.ExecTxResult{})
	}
	// mock beacon root if replaying
	if k.EthReplayConfig.Enabled {
		if beaconRoot := k.ReplayBlock.BeaconRoot(); beaconRoot != nil {
			blockCtx, err := k.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
			if err != nil {
				panic(err)
			}
			statedb := state.NewDBImpl(ctx, k, false)
			vmenv := vm.NewEVM(*blockCtx, statedb, types.DefaultChainConfig().EthereumConfig(k.ChainID(ctx)), vm.Config{}, k.CustomPrecompiles(ctx))
			core.ProcessBeaconBlockRoot(*beaconRoot, vmenv)
			_, err = statedb.Finalize()
			if err != nil {
				panic(err)
			}
		}
	}
	if k.EthBlockTestConfig.Enabled {
		parentHash := common.BytesToHash(ctx.BlockHeader().LastBlockId.Hash)
		blockCtx, err := k.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
		if err != nil {
			panic(err)
		}
		statedb := state.NewDBImpl(ctx, k, false)
		vmenv := vm.NewEVM(*blockCtx, statedb, types.DefaultChainConfig().EthereumConfig(k.ChainID(ctx)), vm.Config{}, k.CustomPrecompiles(ctx))
		core.ProcessParentBlockHash(parentHash, vmenv)
		_, err = statedb.Finalize()
		if err != nil {
			panic(err)
		}
	}
}
