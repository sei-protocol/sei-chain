package keeper

import (
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const ContractPrefixKey = "x-wasm-contract"

func (k Keeper) SetContract(ctx sdk.Context, contract *types.ContractInfoV2) error {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		[]byte(ContractPrefixKey),
	)
	bz, err := contract.Marshal()
	if err != nil {
		return errors.New("failed to marshal contract info")
	}
	ctx.Logger().Info(fmt.Sprintf("Setting contract address %s", contract.ContractAddr))
	store.Set(contractKey(contract.ContractAddr), bz)
	return nil
}

func (k Keeper) DeleteContract(ctx sdk.Context, contractAddr string) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		[]byte(ContractPrefixKey),
	)
	key := contractKey(contractAddr)
	store.Delete(key)
}

func (k Keeper) GetContract(ctx sdk.Context, contractAddr string) (types.ContractInfoV2, error) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		[]byte(ContractPrefixKey),
	)
	key := contractKey(contractAddr)
	res := types.ContractInfoV2{}
	if !store.Has(key) {
		return res, errors.New("cannot find contract info")
	}
	if err := res.Unmarshal(store.Get(key)); err != nil {
		return res, errors.New("cannot parse contract info")
	}
	return res, nil
}

func (k Keeper) GetAllContractInfo(ctx sdk.Context) []types.ContractInfoV2 {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), []byte(ContractPrefixKey))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	list := []types.ContractInfoV2{}
	for ; iterator.Valid(); iterator.Next() {
		contract := types.ContractInfoV2{}
		if err := contract.Unmarshal(iterator.Value()); err == nil {
			list = append(list, contract)
		}
	}

	return list
}

func contractKey(contractAddr string) []byte {
	return []byte(contractAddr)
}
