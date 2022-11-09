package dex

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func HandleUpdateTickSizeProposal(ctx sdk.Context, k *keeper.Keeper, p *types.UpdateTickSizeProposal) error {
	for _, ticksize := range p.TickSizeList {
		k.SetTickSizeForPair(ctx, ticksize.ContractAddr, *ticksize.Pair, ticksize.Ticksize)
	}
	return nil
}

func HandleAddAssetMetadataProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddAssetMetadataProposal) error {
	for _, asset := range p.AssetList {
		k.SetAssetMetadata(ctx, asset)
	}
	return nil
}
