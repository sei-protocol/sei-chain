package evm

import (
	"encoding/hex"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
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

	ctx, err := ante.Preprocess(ctx, evmMsg, evmKeeper.GetParams(ctx))
	if err != nil {
		return []sdkacltypes.AccessOperation{}, err
	}
	ops := []sdkacltypes.AccessOperation{}
	ops = appendRWBalanceOps(ops, state.GetMiddleManAddress(ctx))
	ops = appendRWBalanceOps(ops, state.GetCoinbaseAddress(ctx))
	sender, found := evmtypes.GetContextSeiAddress(ctx)
	if !found {
		return []sdkacltypes.AccessOperation{}, errors.New("unable to find sei address in context when generating dependencies")
	}
	ops = appendRWBalanceOps(ops, sender)

	tx, found := evmtypes.GetContextEthTx(ctx)
	if !found {
		return []sdkacltypes.AccessOperation{}, errors.New("unable to find eth tx in context when generating dependencies")
	}
	toAddress := tx.To()
	if toAddress != nil {
		seiAddress, associated := evmKeeper.GetSeiAddress(ctx, *toAddress)
		if !associated {
			seiAddress = sdk.AccAddress((*toAddress)[:])
		}
		ops = appendRWBalanceOps(ops, seiAddress)
		ops = append(ops, sdkacltypes.AccessOperation{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(evmtypes.EVMAddressToSeiAddressKey(*toAddress)),
		})
	}

	evmAddr, found := evmtypes.GetContextEVMAddress(ctx)
	if !found {
		return []sdkacltypes.AccessOperation{}, errors.New("unable to find evm address in context when generating dependencies")
	}
	return append(ops, []sdkacltypes.AccessOperation{

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(evmtypes.TransientStateKey(ctx)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(evmtypes.TransientStateKey(ctx)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(evmtypes.AccountTransientStateKey(ctx)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(evmtypes.AccountTransientStateKey(ctx)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(evmtypes.TransientModuleStateKey(ctx)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(evmtypes.TransientModuleStateKey(ctx)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(append(evmtypes.NonceKeyPrefix, evmAddr[:]...)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(append(evmtypes.NonceKeyPrefix, evmAddr[:]...)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(evmtypes.ReceiptKey(tx.Hash())),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(evmtypes.SeiAddressToEVMAddressKey(evmKeeper.AccountKeeper().GetModuleAddress(authtypes.FeeCollectorName))),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_EVM,
			IdentifierTemplate: hex.EncodeToString(evmtypes.EVMAddressToSeiAddressKey(evmAddr)),
		},

		// Last Operation should always be a commit
		*acltypes.CommitAccessOp(),
	}...), nil
}

func appendRWBalanceOps(ops []sdkacltypes.AccessOperation, addr sdk.AccAddress) []sdkacltypes.AccessOperation {
	idTempl := hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(addr))
	return append(ops,
		sdkacltypes.AccessOperation{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: idTempl,
		},
		sdkacltypes.AccessOperation{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: idTempl,
		})
}
