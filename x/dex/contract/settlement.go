package contract

import (
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	dexkeeperutils "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dextypesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	dextypeswasm "github.com/sei-protocol/sei-chain/x/dex/types/wasm"
)

func HandleSettlements(
	ctx sdk.Context,
	contractAddr string,
	dexkeeper *keeper.Keeper,
	settlements []*types.SettlementEntry,
) error {
	setSettlementStates(ctx, contractAddr, dexkeeper, settlements)
	return callSettlementHook(ctx, contractAddr, dexkeeper, settlements)
}

func setSettlementStates(
	ctx sdk.Context,
	contractAddr string,
	dexkeeper *keeper.Keeper,
	settlementEntries []*types.SettlementEntry,
) {
	executionStart := time.Now()
	defer telemetry.ModuleSetGauge(types.ModuleName, float32(time.Now().Sub(executionStart).Milliseconds()), "set_settlement_states_ms")
	_, currentEpoch := dexkeeper.IsNewEpoch(ctx)
	settlementMap := map[dextypesutils.PairString]*types.Settlements{}
	for _, settlementEntry := range settlementEntries {
		priceDenom := settlementEntry.PriceDenom
		assetDenom := settlementEntry.AssetDenom
		pair := types.Pair{
			PriceDenom: priceDenom,
			AssetDenom: assetDenom,
		}
		if settlements, ok := settlementMap[dextypesutils.GetPairString(&pair)]; ok {
			settlements.Entries = append(settlements.Entries, settlementEntry)
		} else {
			settlementMap[dextypesutils.GetPairString(&pair)] = &types.Settlements{
				Epoch:   int64(currentEpoch),
				Entries: []*types.SettlementEntry{settlementEntry},
			}
		}
	}
	// The size of this metric grows with # of contracts which shouldn't be too large.
	// If # of contracts becomes large, we may need to disable this metric
	telemetry.ModuleSetGauge(types.ModuleName, float32(len(settlementEntries)), contractAddr+"_settlement_size")
	for _, settlements := range settlementMap {
		if len(settlements.Entries) == 0 {
			continue
		}
		dexkeeper.SetSettlements(ctx, contractAddr, settlements.Entries[0].PriceDenom, settlements.Entries[0].AssetDenom, *settlements)
	}
}

func callSettlementHook(
	ctx sdk.Context,
	contractAddr string,
	dexkeeper *keeper.Keeper,
	settlementEntries []*types.SettlementEntry,
) error {
	_, currentEpoch := dexkeeper.IsNewEpoch(ctx)
	nativeSettlementMsg := dextypeswasm.SudoSettlementMsg{
		Settlement: types.Settlements{
			Epoch:   int64(currentEpoch),
			Entries: settlementEntries,
		},
	}
	if _, err := dexkeeperutils.CallContractSudo(ctx, dexkeeper, contractAddr, nativeSettlementMsg); err != nil {
		return err
	}
	return nil
}
