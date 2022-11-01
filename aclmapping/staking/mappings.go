package aclstakingmapping

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	utils "github.com/sei-protocol/sei-chain/aclmapping/utils"
)

var ErrorInvalidMsgType = fmt.Errorf("invalid message received for staking module")

func GetStakingDependencyGenerator() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	delegateKey := acltypes.GenerateMessageKey(&stakingtypes.MsgDelegate{})
	undelegateKey := acltypes.GenerateMessageKey(&stakingtypes.MsgUndelegate{})
	beginRedelegateKey := acltypes.GenerateMessageKey(&stakingtypes.MsgBeginRedelegate{})
	dependencyGeneratorMap[delegateKey] = MsgDelegateDependencyGenerator
	dependencyGeneratorMap[undelegateKey] = MsgUndelegateDependencyGenerator
	dependencyGeneratorMap[beginRedelegateKey] = MsgBeginRedelegateDependencyGenerator

	return dependencyGeneratorMap
}

func MsgDelegateDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgDelegate, ok := msg.(*stakingtypes.MsgDelegate)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}

	accessOperations := []sdkacltypes.AccessOperation{
		// Checks if the delegator exists
		// Checks if there is a delegation object that already exists for (delegatorAddr, validatorAddr)
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgDelegate.DelegatorAddress+msgDelegate.ValidatorAddress),
		},
		// Store new delegator for (delegator, validator)
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgDelegate.DelegatorAddress+msgDelegate.ValidatorAddress),
		},

		// delegate coins from account validator account
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgDelegate.DelegatorAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgDelegate.DelegatorAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgDelegate.ValidatorAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgDelegate.ValidatorAddress),
		},

		// Checks if the validators exchange rate is valid
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgDelegate.ValidatorAddress),
		},
		// Update validator shares and power index
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgDelegate.ValidatorAddress),
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

func MsgUndelegateDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgUndelegate, ok := msg.(*stakingtypes.MsgUndelegate)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}

	accessOperations := []sdkacltypes.AccessOperation{
		// Treat Delegations and Undelegations to have the same ACL since they are highly coupled, no point in finer granularization

		// Get delegation/redelegations and error checking
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgUndelegate.DelegatorAddress+msgUndelegate.ValidatorAddress),
		},
		// Update/delete delegation and update redelegation
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgUndelegate.DelegatorAddress+msgUndelegate.ValidatorAddress),
		},

		// Update the delegator and validator account balances
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgUndelegate.DelegatorAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgUndelegate.DelegatorAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgUndelegate.ValidatorAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgUndelegate.ValidatorAddress),
		},

		// Checks if the validators exchange rate is valid
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgUndelegate.ValidatorAddress),
		},
		// Update validator shares and power index
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgUndelegate.ValidatorAddress),
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

func MsgBeginRedelegateDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgBeingRedelegate, ok := msg.(*stakingtypes.MsgBeginRedelegate)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}

	accessOperations := []sdkacltypes.AccessOperation{
		// Treat Delegations and Redelegations to have the same ACL since they are highly coupled, no point in finer granularization

		// Get src delegation to verify it has sufficient funds to undelegate
		// Get dest delegation to see if it already exists
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgBeingRedelegate.DelegatorAddress+msgBeingRedelegate.ValidatorSrcAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgBeingRedelegate.DelegatorAddress+msgBeingRedelegate.ValidatorDstAddress),
		},
		// Update/delete src and destination delegation after tokens have been unbonded
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgBeingRedelegate.DelegatorAddress+msgBeingRedelegate.ValidatorSrcAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgBeingRedelegate.DelegatorAddress+msgBeingRedelegate.ValidatorDstAddress),
		},

		// Update the delegator, src validator and dest validator account balances
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgBeingRedelegate.DelegatorAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgBeingRedelegate.DelegatorAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgBeingRedelegate.ValidatorSrcAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgBeingRedelegate.ValidatorSrcAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgBeingRedelegate.ValidatorDstAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.BANK, msgBeingRedelegate.ValidatorDstAddress),
		},

		// Update validators staking shares and power index
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgBeingRedelegate.ValidatorSrcAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgBeingRedelegate.ValidatorSrcAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgBeingRedelegate.ValidatorDstAddress),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: utils.GetIdentifierTemplatePerModule(utils.STAKING, msgBeingRedelegate.ValidatorDstAddress),
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
