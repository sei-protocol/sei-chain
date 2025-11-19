package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

type AuthorizationPolicy interface {
	CanCreateCode(c types.AccessConfig, creator seitypes.AccAddress) bool
	CanInstantiateContract(c types.AccessConfig, actor seitypes.AccAddress) bool
	CanModifyContract(admin, actor seitypes.AccAddress) bool
}

type DefaultAuthorizationPolicy struct{}

func (p DefaultAuthorizationPolicy) CanCreateCode(config types.AccessConfig, actor seitypes.AccAddress) bool {
	return config.Allowed(actor)
}

func (p DefaultAuthorizationPolicy) CanInstantiateContract(config types.AccessConfig, actor seitypes.AccAddress) bool {
	return config.Allowed(actor)
}

func (p DefaultAuthorizationPolicy) CanModifyContract(admin, actor seitypes.AccAddress) bool {
	return admin != nil && admin.Equals(actor)
}

type GovAuthorizationPolicy struct{}

func (p GovAuthorizationPolicy) CanCreateCode(types.AccessConfig, seitypes.AccAddress) bool {
	return true
}

func (p GovAuthorizationPolicy) CanInstantiateContract(types.AccessConfig, seitypes.AccAddress) bool {
	return true
}

func (p GovAuthorizationPolicy) CanModifyContract(seitypes.AccAddress, seitypes.AccAddress) bool {
	return true
}
