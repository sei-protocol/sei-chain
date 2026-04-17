package migrations

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func MigrateERCNativePointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerERC20NativePrefix...)).ReverseIterator(nil, nil)
	defer func() { _ = iter.Close() }()
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
			logger.Error("Failed to upgrade pointer for token due to failed name query", "token", token, "err", err)
			continue
		}
		oSymbol, err := k.QueryERCSingleOutput(ctx, "native", addr, "symbol")
		if err != nil {
			logger.Error("Failed to upgrade pointer for token due to failed symbol query", "token", token, "err", err)
			continue
		}
		oDecimals, err := k.QueryERCSingleOutput(ctx, "native", addr, "decimals")
		if err != nil {
			logger.Error("Failed to upgrade pointer for token due to failed decimal query", "token", token, "err", err)
			continue
		}
		_ = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
			_, err := k.UpsertERCNativePointer(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)), e, token, utils.ERCMetadata{
				Name:     oName.(string),
				Symbol:   oSymbol.(string),
				Decimals: oDecimals.(uint8),
			})
			return err
		}, func(s1, s2 string) {
			logger.Error("Failed to upgrade pointer for token at step", "token", token, "from-step", s1, "to-step", s2)
		})
	}
	return nil
}

func MigrateERCCW20Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerERC20CW20Prefix...)).ReverseIterator(nil, nil)
	defer func() { _ = iter.Close() }()
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
			logger.Error("Failed to upgrade pointer due to failed name query", "pointer", cwAddr, "err", err)
			continue
		}
		oSymbol, err := k.QueryERCSingleOutput(ctx, "cw20", addr, "symbol")
		if err != nil {
			logger.Error("Failed to upgrade pointer due to failed symbol query", "pointer", cwAddr, "err", err)
			continue
		}
		_ = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
			_, err := k.UpsertERCCW20Pointer(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)), e, cwAddr, utils.ERCMetadata{
				Name:   oName.(string),
				Symbol: oSymbol.(string),
			})
			return err
		}, func(s1, s2 string) {
			logger.Error("Failed to upgrade pointer at step", "pointer", cwAddr, "from-step", s1, "to-step", s2)
		})
	}
	return nil
}

func MigrateERCCW721Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerERC721CW721Prefix...)).ReverseIterator(nil, nil)
	defer func() { _ = iter.Close() }()
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
			logger.Error("Failed to upgrade pointer due to failed name query", "pointer", cwAddr, "err", err)
			continue
		}
		oSymbol, err := k.QueryERCSingleOutput(ctx, "cw721", addr, "symbol")
		if err != nil {
			logger.Error("Failed to upgrade pointer due to failed symbol query", "pointer", cwAddr, "err", err)
			continue
		}
		_ = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
			_, err := k.UpsertERCCW721Pointer(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)), e, cwAddr, utils.ERCMetadata{
				Name:   oName.(string),
				Symbol: oSymbol.(string),
			})
			return err
		}, func(s1, s2 string) {
			logger.Error("Failed to upgrade pointer at step", "pointer", cwAddr, "from-step", s1, "to-step", s2)
		})
	}
	return nil
}

func MigrateERCCW1155Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerERC1155CW1155Prefix...)).ReverseIterator(nil, nil)
	defer func() { _ = iter.Close() }()
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
			logger.Error("Failed to upgrade pointer due to failed name query", "pointer", cwAddr, "err", err)
			continue
		}
		oSymbol, err := k.QueryERCSingleOutput(ctx, "cw1155", addr, "symbol")
		if err != nil {
			logger.Error("Failed to upgrade pointer due to failed symbol query", "pointer", cwAddr, "err", err)
			continue
		}
		_ = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
			_, err := k.UpsertERCCW1155Pointer(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)), e, cwAddr, utils.ERCMetadata{
				Name:   oName.(string),
				Symbol: oSymbol.(string),
			})
			return err
		}, func(s1, s2 string) {
			logger.Error("Failed to upgrade pointer at step", "pointer", cwAddr, "from-step", s1, "to-step", s2)
		})
	}
	return nil
}

func MigrateCWERC20Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerCW20ERC20Prefix...)).ReverseIterator(nil, nil)
	defer func() { _ = iter.Close() }()
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
			logger.Error("error parsing cw-erc20 pointer address", "pointer", string(iter.Value()), "err", err)
			return err
		}
		_, err = k.WasmKeeper().Migrate(ctx, addr, moduleAcct, codeID, bz)
		if err != nil {
			logger.Error("error migrating cw-erc20 pointer to code ID", "pointer", addr, "code-id", codeID, "err", err)
			return err
		}
	}
	return nil
}

func MigrateCWERC721Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerCW721ERC721Prefix...)).ReverseIterator(nil, nil)
	defer func() { _ = iter.Close() }()
	bz, _ := json.Marshal(map[string]interface{}{})
	moduleAcct := k.AccountKeeper().GetModuleAddress(types.ModuleName)
	codeID := k.GetStoredPointerCodeID(ctx, types.PointerType_ERC721)
	seen := map[string]struct{}{}
	for ; iter.Valid(); iter.Next() {
		evmAddr := string(iter.Key()[:len(iter.Key())-2]) // last two bytes are version
		if _, ok := seen[evmAddr]; ok {
			continue
		}
		seen[evmAddr] = struct{}{}
		addr, err := sdk.AccAddressFromBech32(string(iter.Value()))
		if err != nil {
			logger.Error("error parsing cw-erc721 pointer address", "pointer", string(iter.Value()), "err", err)
			return err
		}
		_, err = k.WasmKeeper().Migrate(ctx, addr, moduleAcct, codeID, bz)
		if err != nil {
			logger.Error("error migrating cw-erc721 pointer to code ID", "pointer", addr, "code-id", codeID, "err", err)
			return err
		}
	}
	return nil
}

func MigrateCWERC1155Pointers(ctx sdk.Context, k *keeper.Keeper) error {
	iter := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), append(types.PointerRegistryPrefix, types.PointerCW1155ERC1155Prefix...)).ReverseIterator(nil, nil)
	defer func() { _ = iter.Close() }()
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
			logger.Error("error parsing cw-erc1155 pointer address", "pointer", string(iter.Value()), "err", err)
			return err
		}
		_, err = k.WasmKeeper().Migrate(ctx, addr, moduleAcct, codeID, bz)
		if err != nil {
			logger.Error("error migrating cw-erc1155 pointer to code ID", "pointer", addr, "code-id", codeID, "err", err)
			return err
		}
	}
	return nil
}
