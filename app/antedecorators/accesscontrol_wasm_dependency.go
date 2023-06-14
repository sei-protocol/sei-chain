package antedecorators

import (
	"encoding/hex"

	wasm "github.com/CosmWasm/wasmd/x/wasm"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	acl "github.com/cosmos/cosmos-sdk/x/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

type ACLWasmDependencyDecorator struct {
	aclKeeper  aclkeeper.Keeper
	wasmKeeper wasm.Keeper
}

func NewACLWasmDependencyDecorator(aclKeeper aclkeeper.Keeper, wasmKeeper wasm.Keeper) ACLWasmDependencyDecorator {
	return ACLWasmDependencyDecorator{aclKeeper: aclKeeper, wasmKeeper: wasmKeeper}
}

func (ad ACLWasmDependencyDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {

	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {
		case *acltypes.MsgRegisterWasmDependency:
			// check if the FromAddress for the contract matches up with the admin for the contract
			matches, err := ad.SenderMatchesContractAdmin(ctx, m)
			if err != nil {
				return ctx, err
			}
			if !matches {
				return ctx, sdkerrors.Wrap(acl.ErrWasmDependencyRegistrationFailed, "permission denied, sender doesn't match contract admin")
			}
		default:
			continue
		}
	}

	return next(ctx, tx, simulate)
}

func (ad ACLWasmDependencyDecorator) SenderMatchesContractAdmin(ctx sdk.Context, msg *acltypes.MsgRegisterWasmDependency) (bool, error) {
	contractAddr, err := sdk.AccAddressFromBech32(msg.WasmDependencyMapping.ContractAddress)
	if err != nil {
		return false, err
	}

	contractInfo := ad.wasmKeeper.GetContractInfo(ctx, contractAddr)

	return contractInfo.Admin == msg.FromAddress, nil
}

func (ad ACLWasmDependencyDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	deps := []sdkacltypes.AccessOperation{}

	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {
		case *acltypes.MsgRegisterWasmDependency:
			contractAddr, err := sdk.AccAddressFromBech32(m.WasmDependencyMapping.ContractAddress)
			if err != nil {
				return txDeps, err
			}
			dependencies := []sdkacltypes.AccessOperation{
				{
					AccessType:         sdkacltypes.AccessType_READ,
					ResourceType:       sdkacltypes.ResourceType_KV_WASM_CONTRACT_ADDRESS,
					IdentifierTemplate: hex.EncodeToString(wasmtypes.GetContractAddressKey(contractAddr)),
				},
			}

			deps = append(deps, dependencies...)
		default:
			continue
		}
	}

	return next(append(txDeps, deps...), tx, txIndex)
}
