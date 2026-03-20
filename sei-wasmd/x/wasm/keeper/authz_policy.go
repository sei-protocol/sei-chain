package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

type AuthorizationPolicy interface {
	CanCreateCode(c types.AccessConfig, creator sdk.AccAddress) bool
	CanInstantiateContract(c types.AccessConfig, actor sdk.AccAddress) bool
	CanModifyContract(admin, actor sdk.AccAddress) bool
}

type DefaultAuthorizationPolicy struct{}

func (p DefaultAuthorizationPolicy) CanCreateCode(config types.AccessConfig, actor sdk.AccAddress) bool {
	return config.Allowed(actor)
}

func (p DefaultAuthorizationPolicy) CanInstantiateContract(config types.AccessConfig, actor sdk.AccAddress) bool {
	return config.Allowed(actor)
}

func (p DefaultAuthorizationPolicy) CanModifyContract(admin, actor sdk.AccAddress) bool {
	return admin != nil && admin.Equals(actor)
}

type GovAuthorizationPolicy struct{}

func (p GovAuthorizationPolicy) CanCreateCode(types.AccessConfig, sdk.AccAddress) bool {
	return true
}

func (p GovAuthorizationPolicy) CanInstantiateContract(types.AccessConfig, sdk.AccAddress) bool {
	return true
}

func (p GovAuthorizationPolicy) CanModifyContract(sdk.AccAddress, sdk.AccAddress) bool {
	return true
}
