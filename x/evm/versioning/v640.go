package v640

import sdk "github.com/cosmos/cosmos-sdk/types"

func IsAssociationDeprecated(ctx sdk.Context) bool {
	return ctx.ClosestUpgradeName() == "" || ctx.ClosestUpgradeName() >= "v6.4.0"
}
