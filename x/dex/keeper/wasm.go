package keeper

import (
	"encoding/json"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k *Keeper) CallContractSudo(sdkCtx sdk.Context, contractAddr string, msg interface{}) ([]byte, error) {
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
	return data, nil
}
