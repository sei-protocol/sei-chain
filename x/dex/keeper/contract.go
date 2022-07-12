package keeper

import (
	"encoding/binary"
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const PrefixKey = "x-wasm-contract"

func (k Keeper) SetContractAddress(ctx sdk.Context, contractAddr string, codeID uint64) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		[]byte(PrefixKey),
	)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, codeID)
	ctx.Logger().Info(fmt.Sprintf("Setting contract address %s", contractAddr))
	store.Set(contractKey(contractAddr), bz)
}

func (k Keeper) GetAllContractAddresses(ctx sdk.Context) []string {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), []byte(PrefixKey))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	list := []string{}
	for ; iterator.Valid(); iterator.Next() {
		list = append(list, string(iterator.Key()))
	}

	return list
}

func contractKey(contractAddr string) []byte {
	return []byte(contractAddr)
}
