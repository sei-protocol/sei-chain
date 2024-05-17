package utils

import (
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/logging"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const ErrWasmModuleInstCPUFeatureLiteral = "Error instantiating module: CpuFeature"
const SudoGasEventKey = "sudo-gas"
const LogAfter = 5 * time.Second

func getMsgType(msg interface{}) string {
	switch msg.(type) {
	case types.SudoSettlementMsg:
		return "settlement"
	case types.SudoOrderPlacementMsg:
		return "bulk_order_placements"
	case types.SudoOrderCancellationMsg:
		return "bulk_order_cancellations"
	default:
		return "unknown"
	}
}

func sudo(sdkCtx sdk.Context, k *keeper.Keeper, contractAddress sdk.AccAddress, wasmMsg []byte, msgType string) ([]byte, uint64, error) {
	defer utils.PanicHandler(func(err any) {
		utils.MetricsPanicCallback(err, sdkCtx, fmt.Sprintf("%s|%s", contractAddress, msgType))
	})()

	// Measure the time it takes to execute the contract in WASM
	defer metrics.MeasureSudoExecutionDuration(time.Now(), msgType)
	// set up a tmp context to prevent race condition in reading gas consumed
	// Note that the limit will effectively serve as a soft limit since it's
	// possible for the actual computation to go above the specified limit, but
	// the associated contract would be charged corresponding rent.
	gasLimit, err := k.GetContractGasLimit(sdkCtx, contractAddress)
	if err != nil {
		return nil, 0, err
	}
	tmpCtx := sdkCtx.WithGasMeter(sdk.NewGasMeterWithMultiplier(sdkCtx, gasLimit))
	data, err := sudoWithoutOutOfGasPanic(tmpCtx, k, contractAddress, wasmMsg, msgType)
	gasConsumed := tmpCtx.GasMeter().GasConsumed()
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			SudoGasEventKey,
			sdk.NewAttribute("consumed", fmt.Sprintf("%d", gasConsumed)),
			sdk.NewAttribute("type", msgType),
			sdk.NewAttribute("contract", contractAddress.String()),
			sdk.NewAttribute("height", fmt.Sprintf("%d", sdkCtx.BlockHeight())),
		),
	)
	if gasConsumed > 0 {
		sdkCtx.GasMeter().ConsumeGas(gasConsumed, "sudo")
	}

	return data, gasConsumed, err
}

func sudoWithoutOutOfGasPanic(ctx sdk.Context, k *keeper.Keeper, contractAddress []byte, wasmMsg []byte, logName string) ([]byte, error) {
	defer func() {
		if err := recover(); err != nil {
			// only propagate panic if the error is NOT out of gas
			if _, ok := err.(sdk.ErrorOutOfGas); !ok {
				panic(err)
			}
			ctx.Logger().Error(fmt.Sprintf("%s %s is out of gas", sdk.AccAddress(contractAddress).String(), logName))
		}
	}()
	return logging.LogIfNotDoneAfter(ctx.Logger(), func() ([]byte, error) {
		return k.WasmKeeper.Sudo(ctx, contractAddress, wasmMsg)
	}, LogAfter, fmt.Sprintf("wasm_sudo_%s", logName))
}

func CallContractSudo(sdkCtx sdk.Context, k *keeper.Keeper, contractAddr string, msg interface{}, gasAllowance uint64) ([]byte, error) {
	contractAddress, err := sdk.AccAddressFromBech32(contractAddr)
	if err != nil {
		sdkCtx.Logger().Error(err.Error())
		return []byte{}, err
	}
	wasmMsg, err := json.Marshal(msg)
	if err != nil {
		sdkCtx.Logger().Error(err.Error())
		return []byte{}, err
	}
	msgType := getMsgType(msg)
	data, gasUsed, suderr := sudo(sdkCtx, k, contractAddress, wasmMsg, msgType)
	if err := k.ChargeRentForGas(sdkCtx, contractAddr, gasUsed, gasAllowance); err != nil {
		metrics.IncrementSudoFailCount(msgType)
		sdkCtx.Logger().Error(err.Error())
		return []byte{}, err
	}
	if suderr != nil {
		metrics.IncrementSudoFailCount(msgType)
		sdkCtx.Logger().Error(suderr.Error())
		return []byte{}, suderr
	}
	return data, nil
}
