package migrations

import (
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// This migration updates contract info to match the new data format
func V5ToV6(ctx sdk.Context, storeKey sdk.StoreKey, _ codec.BinaryCodec) error {
	for i := 0; i < 256; i++ {
		if err := updateContractInfoForBytePrefix(ctx, storeKey, byte(i)); err != nil {
			return err
		}
	}
	return nil
}

// assuming no dependency exists at the time of this migration
func updateContractInfoForBytePrefix(ctx sdk.Context, storeKey sdk.StoreKey, b byte) error {
	store := prefix.NewStore(
		ctx.KVStore(storeKey),
		[]byte(keeper.ContractPrefixKey),
	)
	iterator := sdk.KVStorePrefixIterator(store, []byte{b})
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		oldContractInfoBytes := iterator.Value()
		oldContractInfo := types.LegacyContractInfo{}
		if err := oldContractInfo.Unmarshal(oldContractInfoBytes); err != nil {
			ctx.Logger().Error(fmt.Sprintf("Failed to unmarshal contract info for %s", iterator.Key()))
			return err
		}
		if oldContractInfo.DependentContractAddrs != nil {
			ctx.Logger().Error(fmt.Sprintf("Contract info of %s has dependencies!", iterator.Key()))
			return errors.New("contract has unexpected dependencies")
		}
		newContractInfo := types.ContractInfo{
			CodeId:            oldContractInfo.CodeId,
			ContractAddr:      oldContractInfo.ContractAddr,
			NeedHook:          oldContractInfo.NeedHook,
			NeedOrderMatching: oldContractInfo.NeedOrderMatching,
		}
		bz, err := newContractInfo.Marshal()
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("Failed to marshal contract info for %s", iterator.Key()))
			return err
		}
		store.Set([]byte(newContractInfo.ContractAddr), bz)
	}
	return nil
}
