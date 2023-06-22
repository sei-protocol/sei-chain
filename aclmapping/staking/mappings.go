package aclstakingmapping

import (
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

var ErrorInvalidMsgType = fmt.Errorf("invalid message received for staking module")

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
	// validatorCons, _ := validator.GetConsAddr()
	// validatorAddrCons := string(stakingtypes.GetValidatorByConsAddrKey(validatorCons))
	// validatorOperatorAddr := validator.GetOperator().String()

	delegationKey := hex.EncodeToString(stakingtypes.GetDelegationKey(delegateAddr, validatorAddr))
	validatorKey := hex.EncodeToString(stakingtypes.GetValidatorKey(validatorAddr))
	delegatorBalanceKey := hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(msgDelegate.DelegatorAddress))
	validatorBalanceKey := hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(msgDelegate.ValidatorAddress))
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
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetValidatorsByPowerIndexKey(validator, keeper.StakingKeeper.PowerReduction(ctx))),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetValidatorsByPowerIndexKey(validator, keeper.StakingKeeper.PowerReduction(ctx))),
		},

		// Before Unbond Distribution Hook
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetDelegatorStartingInfoKey(validatorAddr, delegateAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetDelegatorStartingInfoKey(validatorAddr, delegateAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorCurrentRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorCurrentRewardsKey(validatorAddr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorOutstandingRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorOutstandingRewardsKey(validatorAddr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_FEE_POOL,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.FeePoolKey),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_FEE_POOL,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.FeePoolKey),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorHistoricalRewardsPrefix(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorHistoricalRewardsPrefix(validatorAddr)),
		},

		// Gets Module Account information
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(bondedModuleAdr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(notBondedModuleAdr)),
		},

		// Get Delegator Acc Info
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(delegateAddr)),
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

		// Get last total power for max voting power check
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_TOTAL_POWER,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.LastTotalPowerKey),
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
	validatorAddrCons := hex.EncodeToString(stakingtypes.GetValidatorByConsAddrKey(validatorCons))

	delegationKey := hex.EncodeToString(stakingtypes.GetDelegationKey(delegateAddr, validatorAddr))
	validatorKey := hex.EncodeToString(stakingtypes.GetValidatorKey(validatorAddr))
	delegatorBalanceKey := hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(msgUndelegate.DelegatorAddress))
	validatorBalanceKey := hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(msgUndelegate.ValidatorAddress))

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
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetUBDKey(delegateAddr, validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetUBDKey(delegateAddr, validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION_VAL,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetUBDsByValIndexKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION_VAL,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetUBDsByValIndexKey(validatorAddr)),
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
			IdentifierTemplate: hex.EncodeToString(stakingtypes.UnbondingQueueKey),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_UNBONDING,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.UnbondingQueueKey),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_CON_ADDR,
			IdentifierTemplate: validatorAddrCons,
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_CON_ADDR,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetUBDsByValIndexKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetValidatorsByPowerIndexKey(validator, keeper.StakingKeeper.PowerReduction(ctx))),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetValidatorsByPowerIndexKey(validator, keeper.StakingKeeper.PowerReduction(ctx))),
		},

		// Before Unbond Distribution Hook
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetDelegatorStartingInfoKey(validatorAddr, delegateAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorCurrentRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorCurrentRewardsKey(validatorAddr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorOutstandingRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorOutstandingRewardsKey(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_FEE_POOL,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.FeePoolKey),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_FEE_POOL,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.FeePoolKey),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorHistoricalRewardsPrefix(validatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorHistoricalRewardsPrefix(validatorAddr)),
		},

		// Gets Module Account information
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(bondedModuleAdr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(notBondedModuleAdr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetDelegatorStartingInfoKey(validatorAddr, delegateAddr)),
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
	bondedModuleAdr := keeper.AccountKeeper.GetModuleAddress(stakingtypes.BondedPoolName)
	notBondedModuleAdr := keeper.AccountKeeper.GetModuleAddress(stakingtypes.NotBondedPoolName)

	delegateAddr, _ := sdk.AccAddressFromBech32(msgBeingRedelegate.DelegatorAddress)
	srcValidatorAddr, _ := sdk.ValAddressFromBech32(msgBeingRedelegate.ValidatorSrcAddress)
	dstValidatorAddr, _ := sdk.ValAddressFromBech32(msgBeingRedelegate.ValidatorDstAddress)

	srcValidator, _ := keeper.StakingKeeper.GetValidator(ctx, srcValidatorAddr)
	dstValidator, _ := keeper.StakingKeeper.GetValidator(ctx, dstValidatorAddr)

	srcDelegationKey := hex.EncodeToString(stakingtypes.GetDelegationKey(delegateAddr, srcValidatorAddr))
	dstDelegationKey := hex.EncodeToString(stakingtypes.GetDelegationKey(delegateAddr, dstValidatorAddr))

	srcValidatorKey := hex.EncodeToString(stakingtypes.GetValidatorKey(srcValidatorAddr))
	dstValidatorKey := hex.EncodeToString(stakingtypes.GetValidatorKey(dstValidatorAddr))

	dstValidatorBalanceKey := hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(msgBeingRedelegate.ValidatorDstAddress))

	accessOperations := []sdkacltypes.AccessOperation{
		// Treat Delegations and Undelegations to have the same ACL since they are highly coupled, no point in finer granularization

		// Get delegation/redelegations and error checking
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: srcDelegationKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: dstDelegationKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_REDELEGATION,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetREDsKey(delegateAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_REDELEGATION,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetREDsKey(delegateAddr)),
		},

		// Update/delete delegation and update redelegation
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: srcDelegationKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_DELEGATION,
			IdentifierTemplate: dstDelegationKey,
		},

		// Check Unbonding
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetValidatorsByPowerIndexKey(srcValidator, keeper.StakingKeeper.PowerReduction(ctx))),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetValidatorsByPowerIndexKey(srcValidator, keeper.StakingKeeper.PowerReduction(ctx))),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetValidatorsByPowerIndexKey(dstValidator, keeper.StakingKeeper.PowerReduction(ctx))),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetValidatorsByPowerIndexKey(dstValidator, keeper.StakingKeeper.PowerReduction(ctx))),
		},

		// Before Unbond Distribution Hook
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetDelegatorStartingInfoKey(srcValidatorAddr, delegateAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetDelegatorStartingInfoKey(dstValidatorAddr, delegateAddr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorCurrentRewardsKey(srcValidatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorCurrentRewardsKey(srcValidatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorCurrentRewardsKey(dstValidatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorCurrentRewardsKey(dstValidatorAddr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorOutstandingRewardsKey(srcValidatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorOutstandingRewardsKey(srcValidatorAddr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_FEE_POOL,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.FeePoolKey),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_FEE_POOL,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.FeePoolKey),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorHistoricalRewardsPrefix(srcValidatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorHistoricalRewardsPrefix(srcValidatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorHistoricalRewardsPrefix(dstValidatorAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetValidatorHistoricalRewardsPrefix(dstValidatorAddr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_REDELEGATION_QUEUE,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.RedelegationQueueKey),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_REDELEGATION_QUEUE,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.RedelegationQueueKey),
		},

		// Gets Module Account information
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(bondedModuleAdr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(notBondedModuleAdr)),
		},

		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetDelegatorStartingInfoKey(srcValidatorAddr, delegateAddr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
			IdentifierTemplate: hex.EncodeToString(distributiontypes.GetDelegatorStartingInfoKey(dstValidatorAddr, delegateAddr)),
		},

		// Update the delegator and validator account balances
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: dstValidatorBalanceKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: dstValidatorBalanceKey,
		},

		// Checks if the validators exchange rate is valid
		{
			AccessType:   sdkacltypes.AccessType_WRITE,
			ResourceType: sdkacltypes.ResourceType_KV_STAKING_REDELEGATION_VAL_SRC,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetREDByValSrcIndexKey(
				delegateAddr,
				srcValidatorAddr,
				dstValidatorAddr,
			)),
		},
		{
			AccessType:   sdkacltypes.AccessType_WRITE,
			ResourceType: sdkacltypes.ResourceType_KV_STAKING_REDELEGATION_VAL_DST,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.GetREDByValDstIndexKey(
				delegateAddr,
				srcValidatorAddr,
				dstValidatorAddr,
			)),
		},

		// Checks if the validators exchange rate is valid
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: srcValidatorKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: dstValidatorKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: srcValidatorKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
			IdentifierTemplate: dstValidatorKey,
		},

		// Get last total power for max voting power check
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_STAKING_TOTAL_POWER,
			IdentifierTemplate: hex.EncodeToString(stakingtypes.LastTotalPowerKey),
		},

		// Last Operation should always be a commit
		*acltypes.CommitAccessOp(),
	}
	return accessOperations, nil
}
