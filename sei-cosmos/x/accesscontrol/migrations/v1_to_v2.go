package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

func V1ToV2(ctx sdk.Context, storeKey sdk.StoreKey) error {
	store := ctx.KVStore(storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.GetWasmMappingKey())

	defer iterator.Close()
	keysToSet := [][]byte{}
	valsToSet := [][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		legacyMapping := acltypes.LegacyWasmDependencyMapping{}
		if err := legacyMapping.Unmarshal(iterator.Value()); err != nil {
			return err
		}
		newMapping := acltypes.WasmDependencyMapping{}
		for _, legacyOp := range legacyMapping.AccessOps {
			newMapping.BaseAccessOps = append(newMapping.BaseAccessOps, &acltypes.WasmAccessOperation{
				Operation:    legacyOp.Operation,
				SelectorType: legacyOp.SelectorType,
				Selector:     legacyOp.Selector,
			})
		}
		newMapping.ResetReason = legacyMapping.ResetReason
		newMapping.ContractAddress = legacyMapping.ContractAddress
		val, err := newMapping.Marshal()
		if err != nil {
			return err
		}
		keysToSet = append(keysToSet, iterator.Key())
		valsToSet = append(valsToSet, val)
	}
	for i, key := range keysToSet {
		store.Set(key, valsToSet[i])
	}
	return nil
}
