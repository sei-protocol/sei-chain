package acldexmapping

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	dexmoduletypes "github.com/sei-protocol/sei-chain/x/dex/types"
)

var ErrPlaceOrdersGenerator = fmt.Errorf("invalid message received for type DexPlaceOrders")

func GetDexDependencyGenerators() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	// dex place orders
	placeOrdersKey := acltypes.GenerateMessageKey(&dexmoduletypes.MsgPlaceOrders{})
	dependencyGeneratorMap[placeOrdersKey] = DexPlaceOrdersDependencyGenerator

	return dependencyGeneratorMap
}

func DexPlaceOrdersDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	placeOrdersMsg, ok := msg.(*dexmoduletypes.MsgPlaceOrders)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrPlaceOrdersGenerator
	}
	// TODO: This is not final, JUST AN EXAMPLE
	return []sdkacltypes.AccessOperation{
		{AccessType: sdkacltypes.AccessType_WRITE, ResourceType: sdkacltypes.ResourceType_KV, IdentifierTemplate: placeOrdersMsg.ContractAddr},
	}, nil
}
