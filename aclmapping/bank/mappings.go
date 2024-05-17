package aclbankmapping

import (
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	utils "github.com/sei-protocol/sei-chain/aclmapping/utils"
)

var ErrorInvalidMsgType = fmt.Errorf("invalid message received for bank module")

func GetBankDepedencyGenerator() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	placeOrdersKey := acltypes.GenerateMessageKey(&banktypes.MsgSend{})
	dependencyGeneratorMap[placeOrdersKey] = MsgSendDependencyGenerator

	return dependencyGeneratorMap
}

func MsgSendDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgSend, ok := msg.(*banktypes.MsgSend)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}
	fromAddrIdentifier := hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(msgSend.FromAddress))
	toAddrIdentifier := hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(msgSend.ToAddress))

	accessOperations := []sdkacltypes.AccessOperation{
		// MsgSend also checks if the coin denom is enabled, but the information is from the params.
		// Changing the param would require a gov proposal, which is synchrounos by default

		// Checks balance of sender
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: fromAddrIdentifier,
		},
		// Reduce the amount from the sender's balance
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: fromAddrIdentifier,
		},

		// Checks balance for receiver
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: toAddrIdentifier,
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: toAddrIdentifier,
		},

		// Tries to create the reciever's account if it doesn't exist
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.CreateAddressStoreKeyFromBech32(msgSend.ToAddress)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.CreateAddressStoreKeyFromBech32(msgSend.ToAddress)),
		},

		// Gets Account Info for the sender
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.CreateAddressStoreKeyFromBech32(msgSend.FromAddress)),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_GLOBAL_ACCOUNT_NUMBER,
			IdentifierTemplate: hex.EncodeToString(authtypes.GlobalAccountNumberKey),
		},

		{
			ResourceType:       sdkacltypes.ResourceType_ANY,
			AccessType:         sdkacltypes.AccessType_COMMIT,
			IdentifierTemplate: utils.DefaultIDTemplate,
		},
	}

	// check if the account exists and add additional write dependency if it doesn't
	toAddr, err := sdk.AccAddressFromBech32(msgSend.ToAddress)
	if err != nil {
		// let msg server handle it
		accessOperations = append(accessOperations, sdkacltypes.AccessOperation{
			ResourceType:       sdkacltypes.ResourceType_ANY,
			AccessType:         sdkacltypes.AccessType_COMMIT,
			IdentifierTemplate: utils.DefaultIDTemplate,
		})
		return accessOperations, nil
	}
	if !keeper.AccountKeeper.HasAccount(ctx, toAddr) {
		accessOperations = append(accessOperations, sdkacltypes.AccessOperation{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_GLOBAL_ACCOUNT_NUMBER,
			IdentifierTemplate: hex.EncodeToString(authtypes.GlobalAccountNumberKey),
		})
	}
	accessOperations = append(accessOperations, sdkacltypes.AccessOperation{
		ResourceType:       sdkacltypes.ResourceType_ANY,
		AccessType:         sdkacltypes.AccessType_COMMIT,
		IdentifierTemplate: utils.DefaultIDTemplate,
	})

	return accessOperations, nil
}
