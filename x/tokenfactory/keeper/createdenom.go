package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	pp "github.com/k0kubun/pp/v3"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

// ConvertToBaseToken converts a fee amount in a whitelisted fee token to the base fee token amount
func (k Keeper) CreateDenom(ctx sdk.Context, creatorAddr string, subdenom string) (newTokenDenom string, err error) {
	pp.Printf("subdenom=%s creatorAddr=%s \n", subdenom, creatorAddr)
	err = k.chargeForCreateDenom(ctx, creatorAddr)
	if err != nil {
		return "", err
	}

	denom, err := k.validateCreateDenom(ctx, creatorAddr, subdenom)
	if err != nil {
		pp.Printf("denom=%s FAILED", denom)
		return "", err
	}
	pp.Printf("denom=%s creatorAddr=%s \n", denom, creatorAddr)

	err = k.createDenomAfterValidation(ctx, creatorAddr, denom)
	pp.Printf("denom=%s err=%s \n", denom, err)
	return denom, err
}

// Runs CreateDenom logic after the charge and all denom validation has been handled.
// Made into a second function for genesis initialization.
func (k Keeper) createDenomAfterValidation(ctx sdk.Context, creatorAddr string, denom string) (err error) {
	denomMetaData := banktypes.Metadata{
		DenomUnits: []*banktypes.DenomUnit{{
			Denom:    denom,
			Exponent: 0,
		}},
		Base: denom,
	}

	k.bankKeeper.SetDenomMetaData(ctx, denomMetaData)

	authorityMetadata := types.DenomAuthorityMetadata{
		Admin: creatorAddr,
	}

	pp.Printf("denom=%s authorityMetadata=%s \n", denom, authorityMetadata)
	err = k.setAuthorityMetadata(ctx, denom, authorityMetadata)
	if err != nil {
		return err
	}

	k.addDenomFromCreator(ctx, creatorAddr, denom)
	return nil
}

func (k Keeper) validateCreateDenom(ctx sdk.Context, creatorAddr string, subdenom string) (newTokenDenom string, err error) {
	// Temporary check until IBC bug is sorted out
	if k.bankKeeper.HasSupply(ctx, subdenom) {
		return "", fmt.Errorf("temporary error until IBC bug is sorted out, " +
			"can't create subdenoms that are the same as a native denom")
	}

	denom, err := types.GetTokenDenom(creatorAddr, subdenom)
	if err != nil {
		return "", err
	}
	pp.Printf("denom=%s err=%s \n", denom, err)

	_, found := k.bankKeeper.GetDenomMetaData(ctx, denom)
	if found {
		return "", types.ErrDenomExists
	}

	return denom, nil
}

func (k Keeper) chargeForCreateDenom(ctx sdk.Context, creatorAddr string) (err error) {
	// Send creation fee to community pool
	creationFee := k.GetParams(ctx).DenomCreationFee
	accAddr, err := sdk.AccAddressFromBech32(creatorAddr)
	if err != nil {
		return err
	}
	if len(creationFee) > 0 {
		// TODO(kartik): Possibly remove community funding
		if err := k.distrKeeper.FundCommunityPool(ctx, creationFee, accAddr); err != nil {
			return err
		}
	}
	return nil
}
