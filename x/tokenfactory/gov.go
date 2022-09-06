package tokenfactory

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/keeper"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

func HandleAddCreatorsToDenomFeeWhitelistProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddCreatorsToDenomFeeWhitelistProposal) error {
	for _, creator := range p.CreatorList {
		k.AddCreatorToWhitelist(ctx, creator)
	}
	return nil
}
