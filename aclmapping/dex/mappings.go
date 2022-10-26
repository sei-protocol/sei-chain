package acldexmapping

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	utils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	dexmoduletypes "github.com/sei-protocol/sei-chain/x/dex/types"
)

var ErrPlaceOrdersGenerator = fmt.Errorf("invalid message received for dex module")

func GetDexDependencyGenerators() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	// dex place orders
	placeOrdersKey := acltypes.GenerateMessageKey(&dexmoduletypes.MsgPlaceOrders{})
	cancelOrdersKey := acltypes.GenerateMessageKey(&dexmoduletypes.MsgCancelOrders{})
	dependencyGeneratorMap[placeOrdersKey] = DexPlaceOrdersDependencyGenerator
	dependencyGeneratorMap[cancelOrdersKey] = DexCancelOrdersDependencyGenerator

	return dependencyGeneratorMap
}

func DexPlaceOrdersDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	placeOrdersMsg, ok := msg.(*dexmoduletypes.MsgPlaceOrders)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrPlaceOrdersGenerator
	}

	return []sdkacltypes.AccessOperation{
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.DEX, placeOrdersMsg.ContractAddr),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.DEX, placeOrdersMsg.ContractAddr),
		},

		// Last Operation should always be a commit
		{
			ResourceType:       sdkacltypes.ResourceType_ANY,
			AccessType:         sdkacltypes.AccessType_COMMIT,
			IdentifierTemplate: utils.DefaultIDTemplate,
		},
	}, nil
}

func DexCancelOrdersDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	cancelOrdersMsg, ok := msg.(*dexmoduletypes.MsgCancelOrders)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrPlaceOrdersGenerator
	}

	return []sdkacltypes.AccessOperation{
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.DEX, cancelOrdersMsg.ContractAddr),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.DEX, cancelOrdersMsg.ContractAddr),
		},

		// Last Operation should always be a commit
		{
			ResourceType:       sdkacltypes.ResourceType_ANY,
			AccessType:         sdkacltypes.AccessType_COMMIT,
			IdentifierTemplate: utils.DefaultIDTemplate,
		},
	}, nil
}
