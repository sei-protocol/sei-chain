package keeper

import (
	"encoding/json"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k *Keeper) CallContractSudo(sdkCtx sdk.Context, contractAddr string, msg interface{}) ([]byte, error) {
	contractAddress, err := sdk.AccAddressFromBech32(contractAddr)
	if err != nil {
		sdkCtx.Logger().Error(err.Error())
		return []byte{}, err
	}
	fmt.Println("add ", contractAddress)

	wasmMsg, err := json.Marshal(msg)
	if err != nil {
		sdkCtx.Logger().Error(err.Error())
		return []byte{}, err
	}
	fmt.Println("wasmmsg", wasmMsg)
	data, err := k.WasmKeeper.Sudo(
		sdkCtx, contractAddress, wasmMsg,
	)
	fmt.Println("data", data)

	if err != nil {
		sdkCtx.Logger().Error(err.Error())
		return []byte{}, err
	}
	return data, nil
}
