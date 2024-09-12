package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

// CreateDenom creates a new token denom with the given subdenom.
func (k Keeper) CreateDenom(ctx sdk.Context, creatorAddr string, subdenom string) (newTokenDenom string, err error) {
	denom, err := k.validateCreateDenom(ctx, creatorAddr, subdenom)
	if err != nil {
		return "", err
	}

	err = k.createDenomAfterValidation(ctx, creatorAddr, denom)
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
		// The following is necessary for x/bank denom validation
		Display: denom,
		Name:    denom,
		Symbol:  denom,
	}

	k.bankKeeper.SetDenomMetaData(ctx, denomMetaData)

	authorityMetadata := types.DenomAuthorityMetadata{
		Admin: creatorAddr,
	}
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

	_, found := k.bankKeeper.GetDenomMetaData(ctx, denom)
	if found {
		return "", types.ErrDenomExists
	}

	return denom, nil
}

func (k Keeper) validateUpdateDenom(ctx sdk.Context, msg *types.MsgUpdateDenom) (tokenDenom string, err error) {
	denom, err := types.GetTokenDenom(msg.GetSender(), msg.GetSubdenom())
	if err != nil {
		return "", err
	}

	_, found := k.bankKeeper.GetDenomMetaData(ctx, denom)
	if !found {
		return "", types.ErrDenomDoesNotExist.Wrapf("denom: %s", denom)
	}

	err = k.validateAllowList(msg.AllowList)
	if err != nil {
		return "", err
	}

	return denom, nil
}

func (k Keeper) validateAllowListSize(allowList *banktypes.AllowList) error {
	if len(allowList.Addresses) > k.config.DenomAllowListMaxSize {
		return types.ErrAllowListTooLarge
	}
	return nil
}

func (k Keeper) validateAllowList(allowList *banktypes.AllowList) error {
	err := k.validateAllowListSize(allowList)
	if err != nil {
		return err
	}

	// validate all addresses in the allow list are bech32
	for _, addr := range allowList.Addresses {
		if _, err = sdk.AccAddressFromBech32(addr); err != nil {
			return fmt.Errorf("invalid address %s: %w", addr, err)
		}
	}
	return nil
}
