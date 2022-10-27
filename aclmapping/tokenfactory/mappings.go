package aclTokenFactorymapping

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	utils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	tokenfactorymoduletypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

var ErrInvalidMessageType = fmt.Errorf("invalid message received for TokenFactory Module")

func GetTokenFactoryDependencyGenerators() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)
	MintMsgKey := acltypes.GenerateMessageKey(&tokenfactorymoduletypes.MsgMint{})
	dependencyGeneratorMap[MintMsgKey] = TokenFactoryMintDependencyGenerator

	BurnMsgKey := acltypes.GenerateMessageKey(&tokenfactorymoduletypes.MsgBurn{})
	dependencyGeneratorMap[BurnMsgKey] = TokenFactoryBurnDependencyGenerator

	return dependencyGeneratorMap
}

func TokenFactoryMintDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	mintMsg, ok := msg.(*tokenfactorymoduletypes.MsgMint)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrInvalidMessageType
	}

	denom := mintMsg.GetAmount().Denom
	return []sdkacltypes.AccessOperation{
		// Reads denom data From BankKeeper
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: denom,
		},

		// Gets Authoritity data related to the denom
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.TOKENFACTORY, denom),
		},

		// Gets Module Account information
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.AUTH, tokenfactorymoduletypes.ModuleName),
		},

		// Sends coins to module account - deferred deposit

		// Updates Supply of the denom
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetPrefixedIdentifierTemplatePerModule(utils.BANK, "supply", denom),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetPrefixedIdentifierTemplatePerModule(utils.BANK, "supply", denom),
		},

		// Sends coins to the msgSender from the Module Account (deferred withdrawal)
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, mintMsg.Sender),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, mintMsg.Sender),
		},
	}, nil
}

func TokenFactoryBurnDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	burnMsg, ok := msg.(*tokenfactorymoduletypes.MsgBurn)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrInvalidMessageType
	}

	denom := burnMsg.GetAmount().Denom
	return []sdkacltypes.AccessOperation{
		// Reads denom data From BankKeeper
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: denom,
		},

		// Gets Authoritity data related to the denom
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_TOKENFACTORY,
			IdentifierTemplate: denom,
		},

		// Gets Module Account information
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.AUTH, tokenfactorymoduletypes.ModuleName),
		},

		// Sends from Sender to Module account (deferred deposit)
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, burnMsg.Sender),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, burnMsg.Sender),
		},

		// Sends coins to the msgSender
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, burnMsg.Sender),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, burnMsg.Sender),
		},

		// Coins removed from Module account (Deferred)

		// Updates Supply of the denom - they should be under the supply prefix - this should always be
		// synchronous
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule("supply", denom),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule("supply", denom),
		},
	}, nil
}
