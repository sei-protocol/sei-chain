package keeper

import (
	"fmt"
	"math"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) BeginBlock(ctx sdk.Context) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)
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

func (k *Keeper) EndBlock(ctx sdk.Context, height int64, blockGasUsed int64) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyEndBlocker)
	// TODO: remove after all TxHashes have been removed
	k.RemoveFirstNTxHashes(ctx, DefaultTxHashesToRemove)

	// Migrate legacy EVM receipts to receipt.db in small batches every N blocks
	if ctx.BlockHeight()%LegacyReceiptMigrationInterval == 0 {
		if migrated, err := k.MigrateLegacyReceiptsBatch(ctx, LegacyReceiptMigrationBatchSize); err != nil {
			ctx.Logger().Error(fmt.Sprintf("failed migrating legacy receipts: %s", err))
		} else if migrated > 0 {
			ctx.Logger().Info(fmt.Sprintf("migrated %d legacy EVM receipts to receipt.db", migrated))
		}
	}

	if scanned, deleted := k.PruneZeroStorageSlots(ctx, ZeroStorageCleanupBatchSize); deleted > 0 {
		ctx.Logger().Info(fmt.Sprintf("pruned %d zero-value contract storage slots while scanning %d keys", deleted, scanned))
	}

	newBaseFee := k.AdjustDynamicBaseFeePerGas(ctx, uint64(blockGasUsed)) // nolint:gosec
	if newBaseFee != nil {
		metrics.GaugeEvmBlockBaseFee(newBaseFee.TruncateInt().BigInt(), height)
	}
	var coinbase sdk.AccAddress
	if k.EthBlockTestConfig.Enabled {
		blocks := k.BlockTest.Json.Blocks
		block, err := blocks[ctx.BlockHeight()-1].Decode()
		if err != nil {
			panic(err)
		}
		coinbase = k.GetSeiAddressOrDefault(ctx, block.Header_.Coinbase)
	} else if k.EthReplayConfig.Enabled {
		coinbase = k.GetSeiAddressOrDefault(ctx, k.ReplayBlock.Header_.Coinbase)
		k.SetReplayedHeight(ctx)
	} else {
		coinbase = k.AccountKeeper().GetModuleAddress(authtypes.FeeCollectorName)
	}
	evmTxDeferredInfoList := k.GetAllEVMTxDeferredInfo(ctx)
	denom := k.GetBaseDenom(ctx)
	surplus := k.GetAnteSurplusSum(ctx)
	for _, deferredInfo := range evmTxDeferredInfoList {
		txHash := common.BytesToHash(deferredInfo.TxHash)
		if deferredInfo.Error != "" && txHash.Cmp(ethtypes.EmptyTxsHash) != 0 {
			if !k.GetNonceBumped(ctx, deferredInfo.TxIndex) {
				continue
			}
			_ = k.SetTransientReceipt(ctx, txHash, &types.Receipt{
				TxHashHex:        txHash.Hex(),
				TransactionIndex: deferredInfo.TxIndex,
				VmError:          deferredInfo.Error,
				BlockNumber:      uint64(ctx.BlockHeight()), // nolint:gosec
			})
			continue
		}
		idx := int(deferredInfo.TxIndex)
		coinbaseAddress := state.GetCoinbaseAddress(idx)
		useiBalance := k.BankKeeper().GetBalance(ctx, coinbaseAddress, denom).Amount
		lockedUseiBalance := k.BankKeeper().LockedCoins(ctx, coinbaseAddress).AmountOf(denom)
		balance := useiBalance.Sub(lockedUseiBalance)
		weiBalance := k.BankKeeper().GetWeiBalance(ctx, coinbaseAddress)
		if !balance.IsZero() || !weiBalance.IsZero() {
			if err := k.BankKeeper().SendCoinsAndWei(ctx, coinbaseAddress, coinbase, balance, weiBalance); err != nil {
				ctx.Logger().Error(fmt.Sprintf("failed to send usei surplus from %s to coinbase account due to %s", coinbaseAddress.String(), err))
			}
		}
		surplus = surplus.Add(deferredInfo.Surplus)
	}
	if surplus.IsPositive() {
		surplusUsei, surplusWei := state.SplitUseiWeiAmount(surplus.BigInt())
		if surplusUsei.GT(sdk.ZeroInt()) {
			if err := k.BankKeeper().AddCoins(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), surplusUsei)), true); err != nil {
				ctx.Logger().Error("failed to send usei surplus of %s to EVM module account", surplusUsei)
			}
		}
		if surplusWei.GT(sdk.ZeroInt()) {
			if err := k.BankKeeper().AddWei(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), surplusWei); err != nil {
				ctx.Logger().Error("failed to send wei surplus of %s to EVM module account", surplusWei)
			}
		}
	}
	allBlooms := utils.Map(evmTxDeferredInfoList, func(i *types.DeferredInfo) ethtypes.Bloom { return ethtypes.BytesToBloom(i.TxBloom) })
	evmOnlyBlooms := make([]ethtypes.Bloom, 0, len(evmTxDeferredInfoList))
	for _, di := range evmTxDeferredInfoList {
		if len(di.TxHash) == 0 {
			continue
		}
		r, err := k.GetTransientReceipt(ctx, common.BytesToHash(di.TxHash), uint64(di.TxIndex))
		if err != nil {
			continue
		}
		// Only EVM receipts in this block that are not synthetic
		if r.TxType == types.ShellEVMTxType || r.BlockNumber != uint64(ctx.BlockHeight()) { //nolint:gosec
			continue
		}
		if len(r.Logs) == 0 {
			continue
		}
		// Re-create a per-tx bloom from EVM-only logs (exclude synthetic receipts but not synthetic logs)
		evmOnlyBloom := ethtypes.CreateBloom(&ethtypes.Receipt{
			Logs: GetLogsForTx(r, 0),
		})
		evmOnlyBlooms = append(evmOnlyBlooms, evmOnlyBloom)
	}
	k.SetBlockBloom(ctx, allBlooms)
	k.SetEvmOnlyBlockBloom(ctx, evmOnlyBlooms)
}
