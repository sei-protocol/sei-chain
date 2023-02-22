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

	aclOps := []sdkacltypes.AccessOperation{
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
			ResourceType:       sdkacltypes.ResourceType_KV_DEX_REGISTERED_PAIR,
			IdentifierTemplate: hex.EncodeToString(dextypes.RegisteredPairPrefix(contractAddr)),
		},
		{
			AccessType:   sdkacltypes.AccessType_READ,
			ResourceType: sdkacltypes.ResourceType_KV_DEX_MEM_DEPOSIT,
			IdentifierTemplate: hex.EncodeToString(append(
				dextypes.MemDepositPrefix(contractAddr),
				[]byte(placeOrdersMsg.Creator)...,
			)),
		},
		{
			AccessType:   sdkacltypes.AccessType_WRITE,
			ResourceType: sdkacltypes.ResourceType_KV_DEX_MEM_DEPOSIT,
			IdentifierTemplate: hex.EncodeToString(append(
				dextypes.MemDepositPrefix(contractAddr),
				[]byte(placeOrdersMsg.Creator)...,
			)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX_MEM_ORDER,
			IdentifierTemplate: hex.EncodeToString(dextypes.MemOrderPrefix(contractAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX_MEM_ORDER,
			IdentifierTemplate: hex.EncodeToString(dextypes.MemOrderPrefix(contractAddr)),
		},
	}

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

	aclOps := []sdkacltypes.AccessOperation{
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX_MEM_CANCEL,
			IdentifierTemplate: hex.EncodeToString(dextypes.MemCancelPrefix(contractAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DEX_MEM_CANCEL,
			IdentifierTemplate: hex.EncodeToString(dextypes.MemCancelPrefix(contractAddr)),
		},
	}

	for _, order := range cancelOrdersMsg.GetCancellations() {
		priceDenom := order.GetPriceDenom()
		assetDenom := order.GetAssetDenom()
		aclOps = append(aclOps, GetLongShortOrderBookOps(contractAddr, priceDenom, assetDenom)...)
	}

	// Last Operation should always be a commit
	aclOps = append(aclOps, *acltypes.CommitAccessOp())
	return aclOps, nil
}
