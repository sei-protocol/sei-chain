package depdecorators

import (
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

type GasMeterSetterDecorator struct {
}

func (d GasMeterSetterDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, next sdk.AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	for _, msg := range tx.GetMsgs() {
		if wasmExecuteMsg, ok := msg.(*wasmtypes.MsgExecuteContract); ok {
			// if we have a wasm execute message, we need to declare the dependency to read accesscontrol for giving gas discount
			accAddr, err := sdk.AccAddressFromBech32(wasmExecuteMsg.Contract)
			if err != nil {
				return txDeps, err
			}
			txDeps = append(txDeps, sdkacltypes.AccessOperation{
				AccessType:         sdkacltypes.AccessType_READ,
				ResourceType:       sdkacltypes.ResourceType_KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING,
				IdentifierTemplate: string(acltypes.GetWasmContractAddressKey(accAddr)),
			})
		}
	}
	return next(txDeps, tx)
}
