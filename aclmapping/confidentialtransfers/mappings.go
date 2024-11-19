package confidentialtransfers

import (
	"encoding/hex"
	//"encoding/hex"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
)

var ErrorInvalidMsgType = fmt.Errorf("invalid message received for confidential treansfers module")

func GetConfidentialTransfersDependencyGenerators() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	transferMsgKey := acltypes.GenerateMessageKey(&types.MsgTransfer{})
	dependencyGeneratorMap[transferMsgKey] = MsgTransferDependencyGenerator
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
