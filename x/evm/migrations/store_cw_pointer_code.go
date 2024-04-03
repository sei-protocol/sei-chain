package migrations

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc721"
	artifactsutils "github.com/sei-protocol/sei-chain/x/evm/artifacts/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func StoreCWPointerCode(ctx sdk.Context, k *keeper.Keeper) error {
	erc20CodeID, err := k.WasmKeeper().Create(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), erc20.GetBin(), nil)
	if err != nil {
		panic(err)
	}
	prefix.NewStore(k.PrefixStore(ctx, types.PointerCWCodePrefix), types.PointerCW20ERC20Prefix).Set(
		artifactsutils.GetVersionBz(erc20.CurrentVersion),
		artifactsutils.GetCodeIDBz(erc20CodeID),
	)

	erc721CodeID, err := k.WasmKeeper().Create(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), erc721.GetBin(), nil)
	if err != nil {
		panic(err)
	}
	prefix.NewStore(k.PrefixStore(ctx, types.PointerCWCodePrefix), types.PointerCW721ERC721Prefix).Set(
		artifactsutils.GetVersionBz(erc721.CurrentVersion),
		artifactsutils.GetCodeIDBz(erc721CodeID),
	)
	return nil
}
