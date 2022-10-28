package utils

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
)

const (
	ACCOUNT           = "acc"
	BANK              = "bank"
	AUTH              = "auth"
	STAKING           = "staking"
	TOKENFACTORY      = "tokenfactory"
	DefaultIDTemplate = "*"
)

func GetIdentifierTemplatePerModule(module string, identifier string) string {
	return fmt.Sprintf("%s/%s", module, identifier)
}

func GetPrefixedIdentifierTemplatePerModule(module string, identifier string, prefix string) string {
	return fmt.Sprintf("%s/%s/%s", module, prefix, identifier)
}

func GetOracleReadAccessOpsForValAndFeeder(feederAddr sdk.Address, valAddr sdk.Address) []sdkacltypes.AccessOperation {
	return []sdkacltypes.AccessOperation{
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_ORACLE,
			IdentifierTemplate: feederAddr.String(),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_ORACLE,
			IdentifierTemplate: valAddr.String(),
		},
	}
}
