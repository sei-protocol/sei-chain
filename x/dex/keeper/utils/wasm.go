package utils

import (
	"encoding/json"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
)

func CallContractSudo(sdkCtx sdk.Context, k *keeper.Keeper, contractAddr string, msg interface{}, tracer *otrace.Tracer) ([]byte, error) {
	executionStart := time.Now()
	if tracer != nil {
		_, span := (*tracer).Start(sdkCtx.Context(), "CallContractSudo")
		span.SetAttributes(attribute.String("contract", contractAddr))
		defer span.End()
	}
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
	data, err := k.WasmKeeper.Sudo(
		sdkCtx, contractAddress, wasmMsg,
	)
	if err != nil {
		sdkCtx.Logger().Error(err.Error())
		return []byte{}, err
	}
	telemetry.ModuleSetGauge(types.ModuleName, float32(time.Now().Sub(executionStart).Milliseconds()), "call_contract_sudo_ms")
	return data, nil
}
