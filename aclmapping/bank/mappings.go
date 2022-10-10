package aclbankmapping

import (
	"fmt"

	utils "github.com/cosmos/cosmos-sdk/aclmapping/aclmappingutils"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

var ErrorInvalidMsgType = fmt.Errorf("invalid message received for bank module")

func GetBankDepedencyGenerator() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	// dex place orders
	placeOrdersKey := acltypes.GenerateMessageKey(&banktypes.MsgSend{})
	dependencyGeneratorMap[placeOrdersKey] = MsgSendDependencyGenerator

	return dependencyGeneratorMap
}

func MsgSendDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgSend, ok := msg.(*banktypes.MsgSend)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}

	accessOperations := []sdkacltypes.AccessOperation{}

	accessOperations = append(accessOperations, []sdkacltypes.AccessOperation{
		// MsgSend also checks if the coin denom is enabled, but the information is from the params.
		// Changing the param would require a gov proposal, which is synchrounos by default

		// Checks balance of sender
		{
			AccessType: sdkacltypes.AccessType_READ,
			ResourceType: sdkacltypes.ResourceType_KV,
			IdentifierTemplate: utils.getIdentifierTemplatePerModule(utils.BANK, msgSend.FromAddress),
		},
		// Reduce the amount from the sender's balance
		{
			AccessType: sdkacltypes.AccessType_WRITE,
			ResourceType: sdkacltypes.ResourceType_KV,
			IdentifierTemplate: utils.getIdentifierTemplatePerModule(utils.BANK, msgSend.FromAddress),
		},

		// Checks balance for reciever
		{
			AccessType: sdkacltypes.AccessType_READ,
			ResourceType: sdkacltypes.ResourceType_KV,
			IdentifierTemplate: utils.getIdentifierTemplatePerModule(utils.BANK, msgSend.ToAddress),
		},
		{
			AccessType: sdkacltypes.AccessType_WRITE,
			ResourceType: sdkacltypes.ResourceType_KV,
			IdentifierTemplate: utils.getIdentifierTemplatePerModule(utils.BANK, msgSend.ToAddress),
		},

		// Tries to create the reciever's account if it doesn't exist
		{
			AccessType: sdkacltypes.AccessType_READ,
			ResourceType: sdkacltypes.ResourceType_KV,
			IdentifierTemplate: utils.getIdentifierTemplatePerModule(utils.AUTH, msgSend.ToAddress),
		},
		{
			AccessType: sdkacltypes.AccessType_WRITE,
			ResourceType: sdkacltypes.ResourceType_KV,
			IdentifierTemplate: utils.getIdentifierTemplatePerModule(utils.AUTH, msgSend.ToAddress),
		},

	}...)
	return accessOperations, nil
}
