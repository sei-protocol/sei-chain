package slashing

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// InitGenesis initialize default parameters
// and the keeper's address to pubkey map
func InitGenesis(ctx sdk.Context, keeper keeper.Keeper, stakingKeeper types.StakingKeeper, data *types.GenesisState) {
	stakingKeeper.IterateValidators(ctx,
		func(index int64, validator stakingtypes.ValidatorI) bool {
			consPk, err := validator.ConsPubKey()
			if err != nil {
				panic(err)
			}
			if err := keeper.AddPubkey(ctx, consPk); err != nil {
				panic(err)
			}
			return false
		},
	)

	for _, info := range data.SigningInfos {
		address, err := sdk.ConsAddressFromBech32(info.Address)
		if err != nil {
			panic(err)
		}
		keeper.SetValidatorSigningInfo(ctx, address, info.ValidatorSigningInfo)
	}

	for _, array := range data.MissedBlocks {
		address, err := sdk.ConsAddressFromBech32(array.Address)
		if err != nil {
			panic(err)
		}
		keeper.SetValidatorMissedBlocks(ctx, address, array)
	}

	keeper.SetParams(ctx, data.Params)
}
