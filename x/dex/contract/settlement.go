package contract

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	dexkeeperutils "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dextypeswasm "github.com/sei-protocol/sei-chain/x/dex/types/wasm"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	otrace "go.opentelemetry.io/otel/trace"
)

func HandleSettlements(
	ctx2 context.Context,
	ctx sdk.Context,
	contractAddr string,
	dexkeeper *keeper.Keeper,
	settlements []*types.SettlementEntry,
	tracer *otrace.Tracer,
) error {
	_, span := (*tracer).Start(ctx2, "DEBUGHandleSettlements")
	defer span.End()
	return callSettlementHook(ctx, contractAddr, dexkeeper, settlements)
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
	if _, err := dexkeeperutils.CallContractSudo(ctx, dexkeeper, contractAddr, nativeSettlementMsg, dexutils.ZeroUserProvidedGas); err != nil {
		return err
	}
	return nil
}
