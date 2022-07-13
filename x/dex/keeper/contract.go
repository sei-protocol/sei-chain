package keeper

import (
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const PrefixKey = "x-wasm-contract"

func (k Keeper) SetContract(ctx sdk.Context, contract *types.ContractInfo) error {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		[]byte(PrefixKey),
	)
	bz, err := contract.Marshal()
	if err != nil {
		return errors.New("failed to marshal contract info")
	}
	ctx.Logger().Info(fmt.Sprintf("Setting contract address %s", contract.ContractAddr))
	store.Set(contractKey(contract.ContractAddr), bz)
	return nil
}

func (k Keeper) GetAllContractInfo(ctx sdk.Context) []types.ContractInfo {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), []byte(PrefixKey))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	list := []types.ContractInfo{}
	for ; iterator.Valid(); iterator.Next() {
		contract := types.ContractInfo{}
		if err := contract.Unmarshal(iterator.Value()); err == nil {
			list = append(list, contract)
		}
	}

	return list
}

func contractKey(contractAddr string) []byte {
	return []byte(contractAddr)
}
