package migrations

import (
	"encoding/json"
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
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerERC20NativePrefix...)).ReverseIterator(nil, nil)
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
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s due to failed name query: %s", token, err))
			continue
		}
		oSymbol, err := k.QueryERCSingleOutput(ctx, "native", addr, "symbol")
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s due to failed symbol query: %s", token, err))
			continue
		}
		oDecimals, err := k.QueryERCSingleOutput(ctx, "native", addr, "decimals")
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s due to failed decimal query: %s", token, err))
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
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerERC20CW20Prefix...)).ReverseIterator(nil, nil)
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
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s due to failed name query: %s", cwAddr, err))
			continue
		}
		oSymbol, err := k.QueryERCSingleOutput(ctx, "cw20", addr, "symbol")
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s due to failed symbol query: %s", cwAddr, err))
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
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerERC721CW721Prefix...)).ReverseIterator(nil, nil)
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
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s due to failed name query: %s", cwAddr, err))
			continue
		}
		oSymbol, err := k.QueryERCSingleOutput(ctx, "cw721", addr, "symbol")
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s due to failed symbol query: %s", cwAddr, err))
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

func MigrateERCCW1155Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerERC1155CW1155Prefix...)).ReverseIterator(nil, nil)
	defer iter.Close()
	seen := map[string]struct{}{}
	for ; iter.Valid(); iter.Next() {
		cwAddr := string(iter.Key()[:len(iter.Key())-2]) // last two bytes are version
		if _, ok := seen[cwAddr]; ok {
			continue
		}
		seen[cwAddr] = struct{}{}
		addr := common.BytesToAddress(iter.Value())
		oName, err := k.QueryERCSingleOutput(ctx, "cw1155", addr, "name")
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s due to failed name query: %s", cwAddr, err))
			continue
		}
		oSymbol, err := k.QueryERCSingleOutput(ctx, "cw1155", addr, "symbol")
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("Failed to upgrade pointer for %s due to failed symbol query: %s", cwAddr, err))
			continue
		}
		_ = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
			_, _, err := k.UpsertERCCW1155Pointer(ctx, e, math.MaxUint64, cwAddr, utils.ERCMetadata{
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

func MigrateCWERC20Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerCW20ERC20Prefix...)).ReverseIterator(nil, nil)
	defer iter.Close()
	bz, _ := json.Marshal(map[string]interface{}{})
	moduleAcct := k.AccountKeeper().GetModuleAddress(types.ModuleName)
	codeID := k.GetStoredPointerCodeID(ctx, types.PointerType_ERC20)
	seen := map[string]struct{}{}
	for ; iter.Valid(); iter.Next() {
		evmAddr := string(iter.Key()[:len(iter.Key())-2]) // last two bytes are version
		if _, ok := seen[evmAddr]; ok {
			continue
		}
		seen[evmAddr] = struct{}{}
		addr, err := sdk.AccAddressFromBech32(string(iter.Value()))
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error parsing cw-erc20 pointer %s address %s", string(iter.Value()), err))
			return err
		}
		_, err = k.WasmKeeper().Migrate(ctx, addr, moduleAcct, codeID, bz)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error migrating cw-erc20 pointer %s to code ID %d due to %s", addr.String(), codeID, err))
			return err
		}
	}
	return nil
}

func MigrateCWERC721Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerCW721ERC721Prefix...)).ReverseIterator(nil, nil)
	defer iter.Close()
	bz, _ := json.Marshal(map[string]interface{}{})
	moduleAcct := k.AccountKeeper().GetModuleAddress(types.ModuleName)
	codeID := k.GetStoredPointerCodeID(ctx, types.PointerType_ERC721)
	seen := map[string]struct{}{}
	fmt.Println("about to iterate")
	for ; iter.Valid(); iter.Next() {
		fmt.Println(iter.Key())
		evmAddr := string(iter.Key()[:len(iter.Key())-2]) // last two bytes are version
		if _, ok := seen[evmAddr]; ok {
			continue
		}
		fmt.Println(evmAddr)
		seen[evmAddr] = struct{}{}
		addr, err := sdk.AccAddressFromBech32(string(iter.Value()))
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error parsing cw-erc721 pointer %s address %s", string(iter.Value()), err))
			return err
		}
		fmt.Println(addr)
		_, err = k.WasmKeeper().Migrate(ctx, addr, moduleAcct, codeID, bz)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error migrating cw-erc721 pointer %s to code ID %d due to %s", addr.String(), codeID, err))
			return err
		}
		fmt.Println("no error from wasmkeeper-migrate")
	}
	return nil
}

func MigrateCWERC1155Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerCW1155ERC1155Prefix...)).ReverseIterator(nil, nil)
	defer iter.Close()
	bz, _ := json.Marshal(map[string]interface{}{})
	moduleAcct := k.AccountKeeper().GetModuleAddress(types.ModuleName)
	codeID := k.GetStoredPointerCodeID(ctx, types.PointerType_ERC1155)
	seen := map[string]struct{}{}
	for ; iter.Valid(); iter.Next() {
		evmAddr := string(iter.Key()[:len(iter.Key())-2]) // last two bytes are version
		if _, ok := seen[evmAddr]; ok {
			continue
		}
		seen[evmAddr] = struct{}{}
		addr, err := sdk.AccAddressFromBech32(string(iter.Value()))
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error parsing cw-erc1155 pointer %s address %s", string(iter.Value()), err))
			return err
		}
		_, err = k.WasmKeeper().Migrate(ctx, addr, moduleAcct, codeID, bz)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error migrating cw-erc1155 pointer %s to code ID %d due to %s", addr.String(), codeID, err))
			return err
		}
	}
	return nil
}
