package aclstakingmapping

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
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

	bondedModuleAdr := keeper.AccountKeeper.GetModuleAddress(stakingtypes.BondedPoolName)
	notBondedModuleAdr := keeper.AccountKeeper.GetModuleAddress(stakingtypes.NotBondedPoolName)

	delegateAddr, _ := sdk.AccAddressFromBech32(msgDelegate.DelegatorAddress)
	validatorAddr, _ := sdk.ValAddressFromBech32(msgDelegate.ValidatorAddress)

	validator, _ := keeper.StakingKeeper.GetValidator(ctx, validatorAddr)
	validatorCons, _ := validator.GetConsAddr()
	validatorAddrCons := string(stakingtypes.GetValidatorByConsAddrKey(validatorCons))
	// validatorOperatorAddr := validator.GetOperator().String()

	delegationKey := string(stakingtypes.GetDelegationKey(delegateAddr, validatorAddr))
	validatorKey := string(stakingtypes.GetValidatorKey(validatorAddr))
	delegatorBalanceKey := string(banktypes.CreateAccountBalancesPrefixFromBech32(msgDelegate.DelegatorAddress))
	validatorBalanceKey := string(banktypes.CreateAccountBalancesPrefixFromBech32(msgDelegate.ValidatorAddress))
	// validatorOperatorBalanceKey := string(banktypes.CreateAccountBalancesPrefixFromBech32(validatorOperatorAddr))

	accessOperations := []sdkacltypes.AccessOperation{
		// Treat Delegations and Undelegations to have the same ACL since they are highly coupled, no point in finer granularization

		// Get delegation/redelegations and error checking
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: delegationKey,
		},
		// Update/delete delegation and update redelegation
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: delegationKey,
		},

		// Check Unbonding
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION,
			IdentifierTemplate: string(stakingtypes.GetUBDKey(delegateAddr, validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION,
			IdentifierTemplate: string(stakingtypes.GetUBDKey(delegateAddr, validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION_VAL,
			IdentifierTemplate: string(stakingtypes.GetUBDsByValIndexKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION_VAL,
			IdentifierTemplate: string(stakingtypes.GetUBDsByValIndexKey(validatorAddr)),
		},

		// Testing
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION,
			IdentifierTemplate: delegationKey,
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING,
			IdentifierTemplate: string(stakingtypes.UnbondingQueueKey),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING,
			IdentifierTemplate: string(stakingtypes.UnbondingQueueKey),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_CON_ADDR,
			IdentifierTemplate: validatorAddrCons,
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_CON_ADDR,
			IdentifierTemplate: string(stakingtypes.GetUBDsByValIndexKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: string(stakingtypes.GetValidatorsByPowerIndexKey(validator, keeper.StakingKeeper.PowerReduction(ctx))),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: string(stakingtypes.GetValidatorsByPowerIndexKey(validator, keeper.StakingKeeper.PowerReduction(ctx))),
		},

		// Before Unbond Distribution Hook
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: string(distributiontypes.GetDelegatorStartingInfoKey(validatorAddr, delegateAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: string(distributiontypes.GetDelegatorStartingInfoKey(validatorAddr, delegateAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorCurrentRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorCurrentRewardsKey(validatorAddr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorOutstandingRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorOutstandingRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorCurrentRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_FEE_POOL,
			IdentifierTemplate: string(distributiontypes.FeePoolKey),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_FEE_POOL,
			IdentifierTemplate: string(distributiontypes.FeePoolKey),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorHistoricalRewardsPrefix(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorHistoricalRewardsPrefix(validatorAddr)),
		},

		// Gets Module Account information
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: string(authtypes.AddressStoreKey(bondedModuleAdr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: string(authtypes.AddressStoreKey(notBondedModuleAdr)),
		},

		// Get Delegator Acc Info
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: string(authtypes.AddressStoreKey(delegateAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: string(authtypes.AddressStoreKey(delegateAddr)),
		},


		// Update the delegator and validator account balances
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: delegatorBalanceKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: delegatorBalanceKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: validatorBalanceKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: validatorBalanceKey,
		},

		// Checks if the validators exchange rate is valid
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: validatorKey,
		},
		// Update validator shares and power index
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: validatorKey,
		},

		// Last Operation should always be a commit
		*acltypes.CommitAccessOp(),
	}
	return accessOperations, nil
}

func MsgUndelegateDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	msgUndelegate, ok := msg.(*stakingtypes.MsgUndelegate)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrorInvalidMsgType
	}
	bondedModuleAdr := keeper.AccountKeeper.GetModuleAddress(stakingtypes.BondedPoolName)
	notBondedModuleAdr := keeper.AccountKeeper.GetModuleAddress(stakingtypes.NotBondedPoolName)

	delegateAddr, _ := sdk.AccAddressFromBech32(msgUndelegate.DelegatorAddress)
	validatorAddr, _ := sdk.ValAddressFromBech32(msgUndelegate.ValidatorAddress)

	validator, _ := keeper.StakingKeeper.GetValidator(ctx, validatorAddr)
	validatorCons, _ := validator.GetConsAddr()
	validatorAddrCons := string(stakingtypes.GetValidatorByConsAddrKey(validatorCons))

	delegationKey := string(stakingtypes.GetDelegationKey(delegateAddr, validatorAddr))
	validatorKey := string(stakingtypes.GetValidatorKey(validatorAddr))
	delegatorBalanceKey := string(banktypes.CreateAccountBalancesPrefixFromBech32(msgUndelegate.DelegatorAddress))
	validatorBalanceKey := string(banktypes.CreateAccountBalancesPrefixFromBech32(msgUndelegate.ValidatorAddress))

	accessOperations := []sdkacltypes.AccessOperation{
		// Treat Delegations and Undelegations to have the same ACL since they are highly coupled, no point in finer granularization

		// Get delegation/redelegations and error checking
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: delegationKey,
		},
		// Update/delete delegation and update redelegation
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: delegationKey,
		},

		// Check Unbonding
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION,
			IdentifierTemplate: string(stakingtypes.GetUBDKey(delegateAddr, validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION,
			IdentifierTemplate: string(stakingtypes.GetUBDKey(delegateAddr, validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION_VAL,
			IdentifierTemplate: string(stakingtypes.GetUBDsByValIndexKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION_VAL,
			IdentifierTemplate: string(stakingtypes.GetUBDsByValIndexKey(validatorAddr)),
		},

		// Testing
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION,
			IdentifierTemplate: delegationKey,
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING,
			IdentifierTemplate: string(stakingtypes.UnbondingQueueKey),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING,
			IdentifierTemplate: string(stakingtypes.UnbondingQueueKey),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_CON_ADDR,
			IdentifierTemplate: validatorAddrCons,
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_CON_ADDR,
			IdentifierTemplate: string(stakingtypes.GetUBDsByValIndexKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: string(stakingtypes.GetValidatorsByPowerIndexKey(validator, keeper.StakingKeeper.PowerReduction(ctx))),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: string(stakingtypes.GetValidatorsByPowerIndexKey(validator, keeper.StakingKeeper.PowerReduction(ctx))),
		},

		// Before Unbond Distribution Hook
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: string(distributiontypes.GetDelegatorStartingInfoKey(validatorAddr, delegateAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorCurrentRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorCurrentRewardsKey(validatorAddr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorOutstandingRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorOutstandingRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_FEE_POOL,
			IdentifierTemplate: string(distributiontypes.FeePoolKey),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_FEE_POOL,
			IdentifierTemplate: string(distributiontypes.FeePoolKey),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorHistoricalRewardsPrefix(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: string(distributiontypes.GetValidatorHistoricalRewardsPrefix(validatorAddr)),
		},

		// Gets Module Account information
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: string(authtypes.AddressStoreKey(bondedModuleAdr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: string(authtypes.AddressStoreKey(notBondedModuleAdr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: string(distributiontypes.GetDelegatorStartingInfoKey(validatorAddr, delegateAddr)),
		},

		// Update the delegator and validator account balances
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: delegatorBalanceKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: delegatorBalanceKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: validatorBalanceKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: validatorBalanceKey,
		},

		// Checks if the validators exchange rate is valid
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: validatorKey,
		},
		// Update validator shares and power index
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: validatorKey,
		},

		// Last Operation should always be a commit
		*acltypes.CommitAccessOp(),
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
		*acltypes.CommitAccessOp(),
	}

	return accessOperations, nil
}
