package confidentialtransfers

import (
	"encoding/hex"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	//"encoding/hex"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
)

var ErrorInvalidMsgType = fmt.Errorf("invalid message received for confidential transfers module")

func GetConfidentialTransfersDependencyGenerators() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	transferMsgKey := acltypes.GenerateMessageKey(&types.MsgTransfer{})
	initializeAccountMsgKey := acltypes.GenerateMessageKey(&types.MsgInitializeAccount{})
	depositMsgKey := acltypes.GenerateMessageKey(&types.MsgDeposit{})
	withdrawMsgKey := acltypes.GenerateMessageKey(&types.MsgWithdraw{})
	applyPendingBalanceMsgKey := acltypes.GenerateMessageKey(&types.MsgApplyPendingBalance{})
	dependencyGeneratorMap[transferMsgKey] = MsgTransferDependencyGenerator
	dependencyGeneratorMap[initializeAccountMsgKey] = MsgInitializeAccountDependencyGenerator
	dependencyGeneratorMap[depositMsgKey] = MsgDepositDependencyGenerator
	dependencyGeneratorMap[withdrawMsgKey] = MsgWithdrawDependencyGenerator
	dependencyGeneratorMap[applyPendingBalanceMsgKey] = MsgApplyPendingBalanceDependencyGenerator

	return dependencyGeneratorMap
}

func MsgTransferDependencyGenerator(_ aclkeeper.Keeper, _ sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgTransfer, ok := msg.(*types.MsgTransfer)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}

	fromAddrIdentifier := hex.EncodeToString(types.GetAccountPrefixFromBech32(msgTransfer.FromAddress))
	toAddrIdentifier := hex.EncodeToString(types.GetAccountPrefixFromBech32(msgTransfer.ToAddress))

	accessOperations := []sdkacltypes.AccessOperation{
		// Checks balance of sender
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: fromAddrIdentifier,
		},
		// Reduce the amount from the sender's balance
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: fromAddrIdentifier,
		},

		// Checks balance for receiver
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: toAddrIdentifier,
		},
		// Increase the amount to the receiver's balance
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: toAddrIdentifier,
		},

		*acltypes.CommitAccessOp(),
	}
	return accessOperations, nil
}

func MsgInitializeAccountDependencyGenerator(_ aclkeeper.Keeper, _ sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgInitializeAccount, ok := msg.(*types.MsgInitializeAccount)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}

	fromAddrIdentifier := hex.EncodeToString(types.GetAccountPrefixFromBech32(msgInitializeAccount.FromAddress))

	accessOperations := []sdkacltypes.AccessOperation{
		// Checks if the account already exists
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: fromAddrIdentifier,
		},
		// Created new account
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: fromAddrIdentifier,
		},

		*acltypes.CommitAccessOp(),
	}
	return accessOperations, nil
}

func MsgDepositDependencyGenerator(aclkeeper aclkeeper.Keeper, _ sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgDeposit, ok := msg.(*types.MsgDeposit)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}

	moduleAddress := aclkeeper.AccountKeeper.GetModuleAddress(types.ModuleName)

	fromAddrIdentifier := hex.EncodeToString(types.GetAccountPrefixFromBech32(msgDeposit.FromAddress))

	accessOperations := []sdkacltypes.AccessOperation{
		// Checks if the account already exists
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: fromAddrIdentifier,
		},
		// Withdraws from sender's bank Balance
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(msgDeposit.FromAddress)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(msgDeposit.FromAddress)),
		},

		// Transfer to module account
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(moduleAddress.String())),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(moduleAddress.String())),
		},

		// Modifies account state
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: fromAddrIdentifier,
		},

		*acltypes.CommitAccessOp(),
	}
	return accessOperations, nil
}

func MsgWithdrawDependencyGenerator(aclkeeper aclkeeper.Keeper, _ sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgWithdraw, ok := msg.(*types.MsgWithdraw)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}

	moduleAddress := aclkeeper.AccountKeeper.GetModuleAddress(types.ModuleName)

	fromAddrIdentifier := hex.EncodeToString(types.GetAccountPrefixFromBech32(msgWithdraw.FromAddress))

	accessOperations := []sdkacltypes.AccessOperation{
		// Get account state
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: fromAddrIdentifier,
		},
		// Withdraws from module's bank Balance
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(moduleAddress.String())),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(moduleAddress.String())),
		},
		// Transfer to sender's bank Balance
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(msgWithdraw.FromAddress)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(msgWithdraw.FromAddress)),
		},

		// Modifies account state
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: fromAddrIdentifier,
		},

		*acltypes.CommitAccessOp(),
	}
	return accessOperations, nil
}

func MsgApplyPendingBalanceDependencyGenerator(_ aclkeeper.Keeper, _ sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgApplyPendingBalance, ok := msg.(*types.MsgApplyPendingBalance)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}

	fromAddrIdentifier := hex.EncodeToString(types.GetAccountPrefixFromBech32(msgApplyPendingBalance.Address))

	accessOperations := []sdkacltypes.AccessOperation{
		// Get account state
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: fromAddrIdentifier,
		},
		// Apply pending balance
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_CT_ACCOUNT,
			IdentifierTemplate: fromAddrIdentifier,
		},

		*acltypes.CommitAccessOp(),
	}
	return accessOperations, nil
}
