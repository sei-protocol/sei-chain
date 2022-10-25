package acloraclemapping

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	utils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

var ErrorInvalidMsgType = fmt.Errorf("invalid message received for oracle module")

func GetOracleDependencyGenerator() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	// vote
	voteKey := acltypes.GenerateMessageKey(&oracletypes.MsgAggregateExchangeRateVote{})
	dependencyGeneratorMap[voteKey] = MsgVoteDependencyGenerator

	return dependencyGeneratorMap
}

func MsgVoteDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgVote, ok := msg.(*oracletypes.MsgAggregateExchangeRateVote)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}

	accessOperations := []sdkacltypes.AccessOperation{
		// validate feeder
		// read feeder delegation for val addr - READ
		{
			ResourceType:       sdkacltypes.ResourceType_KV_ORACLE_FEEDERS,
			AccessType:         sdkacltypes.AccessType_READ,
			IdentifierTemplate: msgVote.Validator,
		},
		// read validator from staking - READ
		// validator is bonded check - READ
		// (both covered by below)
		{
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING,
			AccessType:         sdkacltypes.AccessType_READ,
			IdentifierTemplate: msgVote.Validator,
		},
		// get vote target (for all exchange rate tuples) -> blanket read on that prefix - READ
		{
			ResourceType:       sdkacltypes.ResourceType_KV_ORACLE_VOTE_TARGETS,
			AccessType:         sdkacltypes.AccessType_READ,
			IdentifierTemplate: utils.DefaultIDTemplate,
		},

		// set exchange rate vote - WRITE
		{
			ResourceType:       sdkacltypes.ResourceType_KV_ORACLE_AGGREGATE_VOTES,
			AccessType:         sdkacltypes.AccessType_READ,
			IdentifierTemplate: msgVote.Validator,
		},
		// Last Operation should always be a commit
		{
			ResourceType:       sdkacltypes.ResourceType_ANY,
			AccessType:         sdkacltypes.AccessType_COMMIT,
			IdentifierTemplate: utils.DefaultIDTemplate,
		},
	}
	return accessOperations, nil
}
