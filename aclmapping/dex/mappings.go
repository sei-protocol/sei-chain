package acldexmapping

import (
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
)

var ErrPlaceOrdersGenerator = fmt.Errorf("invalid message received for dex module")

func GetDexDependencyGenerators() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	// dex place orders
	placeOrdersKey := acltypes.GenerateMessageKey(&dextypes.MsgPlaceOrders{})
	cancelOrdersKey := acltypes.GenerateMessageKey(&dextypes.MsgCancelOrders{})
	dependencyGeneratorMap[placeOrdersKey] = DexPlaceOrdersDependencyGenerator
	dependencyGeneratorMap[cancelOrdersKey] = DexCancelOrdersDependencyGenerator

	return dependencyGeneratorMap
}

func GetDexMemReadWrite(contract string) []sdkacltypes.AccessOperation {
	if contract == "" {
		return []sdkacltypes.AccessOperation{}
	}

	return []sdkacltypes.AccessOperation{
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_DexMem,
			IdentifierTemplate: hex.EncodeToString([]byte(contract)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_DexMem,
			IdentifierTemplate: hex.EncodeToString([]byte(contract)),
		},
	}
}

func GetLongShortOrderBookOps(contractAddr string, priceDenom string, assetDenom string) []sdkacltypes.AccessOperation {
	return []sdkacltypes.AccessOperation{
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX_CONTRACT_LONGBOOK,
			IdentifierTemplate: hex.EncodeToString(dextypes.OrderBookPrefix(true, contractAddr, priceDenom, assetDenom)),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX_CONTRACT_SHORTBOOK,
			IdentifierTemplate: hex.EncodeToString(dextypes.OrderBookPrefix(false, contractAddr, priceDenom, assetDenom)),
		},
	}
}

func DexPlaceOrdersDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	placeOrdersMsg, ok := msg.(*dextypes.MsgPlaceOrders)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrPlaceOrdersGenerator
	}

	contractAddr := placeOrdersMsg.ContractAddr

	aclOps := GetDexMemReadWrite(contractAddr)

	aclOps = append(aclOps, []sdkacltypes.AccessOperation{
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX_NEXT_ORDER_ID,
			IdentifierTemplate: hex.EncodeToString(dextypes.NextOrderIDPrefix(contractAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX_NEXT_ORDER_ID,
			IdentifierTemplate: hex.EncodeToString(dextypes.NextOrderIDPrefix(contractAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX_PRICE_TICK_SIZE,
			IdentifierTemplate: hex.EncodeToString(dextypes.PriceTickSizeKeyPrefix(contractAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX_QUANTITY_TICK_SIZE,
			IdentifierTemplate: hex.EncodeToString(dextypes.QuantityTickSizeKeyPrefix(contractAddr)),
		},
	}...)

	// Last Operation should always be a commit
	aclOps = append(aclOps, *acltypes.CommitAccessOp())
	return aclOps, nil
}

func DexCancelOrdersDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	cancelOrdersMsg, ok := msg.(*dextypes.MsgCancelOrders)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrPlaceOrdersGenerator
	}
	contractAddr := cancelOrdersMsg.ContractAddr

	aclOps := GetDexMemReadWrite(contractAddr)

	for _, order := range cancelOrdersMsg.GetCancellations() {
		priceDenom := order.GetPriceDenom()
		assetDenom := order.GetAssetDenom()
		aclOps = append(aclOps, GetLongShortOrderBookOps(contractAddr, priceDenom, assetDenom)...)
	}

	// Last Operation should always be a commit
	aclOps = append(aclOps, *acltypes.CommitAccessOp())
	return aclOps, nil
}
