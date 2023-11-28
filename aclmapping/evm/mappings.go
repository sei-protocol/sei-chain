package evm

import (
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

var ErrInvalidMessageType = fmt.Errorf("invalid message received for EVM Module")

func GetEVMDependencyGenerators(evmKeeper evmkeeper.Keeper) aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)
	EVMTransactionMsgKey := acltypes.GenerateMessageKey(&evmtypes.MsgEVMTransaction{})
	dependencyGeneratorMap[EVMTransactionMsgKey] = func(k aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
		return TransactionDependencyGenerator(k, evmKeeper, ctx, msg)
	}

	return dependencyGeneratorMap
}

func TransactionDependencyGenerator(_ aclkeeper.Keeper, evmKeeper evmkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	evmMsg, ok := msg.(*evmtypes.MsgEVMTransaction)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrInvalidMessageType
	}
	if evmMsg.IsAssociateTx() {
		// msg server will be noop for AssociateTx; all work are done in ante
		return []sdkacltypes.AccessOperation{*acltypes.CommitAccessOp()}, nil
	}

	tx, _ := evmMsg.AsTransaction()
	// Only specifying accesses to `to` address since `from` has to be derived via signature,
	// which happens in the ante handler (i.e. after this generator is called) and is quite heavy
	// so we don't want to repeat it.
	toOperations := []sdkacltypes.AccessOperation{}
	toAddress := tx.To()
	if toAddress != nil {
		seiAddress, associated := evmKeeper.GetSeiAddress(ctx, *toAddress)
		if !associated {
			seiAddress = sdk.AccAddress((*toAddress)[:])
		}
		idTempl := hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(seiAddress))
		toOperations = []sdkacltypes.AccessOperation{
			{
				AccessType:         sdkacltypes.AccessType_READ,
				ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
				IdentifierTemplate: idTempl,
			},
			{
				AccessType:         sdkacltypes.AccessType_WRITE,
				ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
				IdentifierTemplate: idTempl,
			},
		}
	}

	return append(toOperations, []sdkacltypes.AccessOperation{

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: "*", // no way to derive what fields a contract might read
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: "*", // no way to derive what fields a contract might write
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: "*", // from address access (reasoning described above)
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: "*", // from address access (reasoning described above)
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH,
			IdentifierTemplate: "*", // from address access (reasoning described above)
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH,
			IdentifierTemplate: "*", // from address access (reasoning described above)
		},

		// Last Operation should always be a commit
		*acltypes.CommitAccessOp(),
	}...), nil
}
