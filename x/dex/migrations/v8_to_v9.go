package migrations

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func V8ToV9(ctx sdk.Context, dexkeeper keeper.Keeper) error {
	dexkeeper.CreateModuleAccount(ctx)

	contractStore := prefix.NewStore(ctx.KVStore(dexkeeper.GetStoreKey()), []byte(keeper.ContractPrefixKey))
	iterator := sdk.KVStorePrefixIterator(contractStore, []byte{})

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		contract := types.ContractInfo{}
		if err := contract.Unmarshal(iterator.Value()); err != nil {
			return err
		}
		contractV2 := types.ContractInfoV2{
			CodeId:                  contract.CodeId,
			ContractAddr:            contract.ContractAddr,
			NeedHook:                contract.NeedHook,
			NeedOrderMatching:       contract.NeedOrderMatching,
			Dependencies:            contract.Dependencies,
			NumIncomingDependencies: contract.NumIncomingDependencies,
		}
		bz, err := contractV2.Marshal()
		if err != nil {
			return err
		}
		contractStore.Set(iterator.Key(), bz)
	}

	return nil
}
