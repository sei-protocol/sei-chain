package migrations

import (
	"fmt"
	"math"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func MigrateERCNativePointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerERC20NativePrefix...)).Iterator(nil, nil)
	defer iter.Close()
	seen := map[string]struct{}{}
	for ; iter.Valid(); iter.Next() {
		token := string(iter.Key()[:len(iter.Key())-2]) // last two bytes are version
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		addr := common.BytesToAddress(iter.Value())
		oName, err := k.QueryERCSingleOutput(ctx, "native", addr, "name")
		if err != nil {
			continue
		}
		oSymbol, err := k.QueryERCSingleOutput(ctx, "native", addr, "symbol")
		if err != nil {
			continue
		}
		oDecimals, err := k.QueryERCSingleOutput(ctx, "native", addr, "oDecimals")
		if err != nil {
			continue
		}
		_ = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
			_, _, err := k.UpsertERCNativePointer(ctx, e, math.MaxUint64, token, utils.ERCMetadata{
				Name:     oName.(string),
				Symbol:   oSymbol.(string),
				Decimals: oDecimals.(uint8),
			})
			return err
		}, func(s1, s2 string) {
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s at step %s due to %s", token, s1, s2))
		})
	}
	return nil
}

func MigrateERCCW20Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerERC20CW20Prefix...)).Iterator(nil, nil)
	defer iter.Close()
	seen := map[string]struct{}{}
	for ; iter.Valid(); iter.Next() {
		cwAddr := string(iter.Key()[:len(iter.Key())-2]) // last two bytes are version
		if _, ok := seen[cwAddr]; ok {
			continue
		}
		seen[cwAddr] = struct{}{}
		addr := common.BytesToAddress(iter.Value())
		oName, err := k.QueryERCSingleOutput(ctx, "cw20", addr, "name")
		if err != nil {
			continue
		}
		oSymbol, err := k.QueryERCSingleOutput(ctx, "cw20", addr, "symbol")
		if err != nil {
			continue
		}
		_ = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
			_, _, err := k.UpsertERCCW20Pointer(ctx, e, math.MaxUint64, cwAddr, utils.ERCMetadata{
				Name:   oName.(string),
				Symbol: oSymbol.(string),
			})
			return err
		}, func(s1, s2 string) {
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s at step %s due to %s", cwAddr, s1, s2))
		})
	}
	return nil
}

func MigrateERCCW721Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerERC721CW721Prefix...)).Iterator(nil, nil)
	defer iter.Close()
	seen := map[string]struct{}{}
	for ; iter.Valid(); iter.Next() {
		cwAddr := string(iter.Key()[:len(iter.Key())-2]) // last two bytes are version
		if _, ok := seen[cwAddr]; ok {
			continue
		}
		seen[cwAddr] = struct{}{}
		addr := common.BytesToAddress(iter.Value())
		oName, err := k.QueryERCSingleOutput(ctx, "cw721", addr, "name")
		if err != nil {
			continue
		}
		oSymbol, err := k.QueryERCSingleOutput(ctx, "cw721", addr, "symbol")
		if err != nil {
			continue
		}
		_ = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
			_, _, err := k.UpsertERCCW721Pointer(ctx, e, math.MaxUint64, cwAddr, utils.ERCMetadata{
				Name:   oName.(string),
				Symbol: oSymbol.(string),
			})
			return err
		}, func(s1, s2 string) {
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s at step %s due to %s", cwAddr, s1, s2))
		})
	}
	return nil
}
