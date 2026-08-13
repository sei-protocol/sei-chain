package testutil

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	feegranttypes "github.com/cosmos/cosmos-sdk/x/feegrant"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

var TestingStoreKeyToResourceTypePrefixMap = acltypes.StoreKeyToResourceTypePrefixMap{
	acltypes.ParentNodeKey: {
		acltypes.ResourceType_ANY: acltypes.EmptyPrefix,
		acltypes.ResourceType_KV:  acltypes.EmptyPrefix,
		acltypes.ResourceType_Mem: acltypes.EmptyPrefix,
	},
	banktypes.StoreKey: {
		acltypes.ResourceType_KV_BANK:             acltypes.EmptyPrefix,
		acltypes.ResourceType_KV_BANK_BALANCES:    banktypes.BalancesPrefix,
		acltypes.ResourceType_KV_BANK_SUPPLY:      banktypes.SupplyKey,
		acltypes.ResourceType_KV_BANK_DENOM:       banktypes.DenomMetadataPrefix,
		acltypes.ResourceType_KV_BANK_WEI_BALANCE: banktypes.WeiBalancesPrefix,
	},
	banktypes.DeferredCacheStoreKey: {
		acltypes.ResourceType_KV_BANK_DEFERRED:                 acltypes.EmptyPrefix,
		acltypes.ResourceType_KV_BANK_DEFERRED_MODULE_TX_INDEX: banktypes.DeferredCachePrefix,
	},
	authtypes.StoreKey: {
		acltypes.ResourceType_KV_AUTH:                       acltypes.EmptyPrefix,
		acltypes.ResourceType_KV_AUTH_ADDRESS_STORE:         authtypes.AddressStoreKeyPrefix,
		acltypes.ResourceType_KV_AUTH_GLOBAL_ACCOUNT_NUMBER: authtypes.GlobalAccountNumberKey,
	},
	authztypes.StoreKey: {
		acltypes.ResourceType_KV_AUTHZ: acltypes.EmptyPrefix,
	},
	distributiontypes.StoreKey: {
		acltypes.ResourceType_KV_DISTRIBUTION:                         acltypes.EmptyPrefix,
		acltypes.ResourceType_KV_DISTRIBUTION_FEE_POOL:                distributiontypes.FeePoolKey,
		acltypes.ResourceType_KV_DISTRIBUTION_PROPOSER_KEY:            distributiontypes.ProposerKey,
		acltypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS:     distributiontypes.ValidatorOutstandingRewardsPrefix,
		acltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR: distributiontypes.DelegatorWithdrawAddrPrefix,
		acltypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO: distributiontypes.DelegatorStartingInfoPrefix,
		acltypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS:  distributiontypes.ValidatorHistoricalRewardsPrefix,
		acltypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS:     distributiontypes.ValidatorCurrentRewardsPrefix,
		acltypes.ResourceType_KV_DISTRIBUTION_VAL_ACCUM_COMMISSION:    distributiontypes.ValidatorAccumulatedCommissionPrefix,
		acltypes.ResourceType_KV_DISTRIBUTION_SLASH_EVENT:             distributiontypes.ValidatorSlashEventPrefix,
	},
	feegranttypes.StoreKey: {
		acltypes.ResourceType_KV_FEEGRANT:           acltypes.EmptyPrefix,
		acltypes.ResourceType_KV_FEEGRANT_ALLOWANCE: feegranttypes.FeeAllowanceKeyPrefix,
	},
	stakingtypes.StoreKey: {
		acltypes.ResourceType_KV_STAKING:                          acltypes.EmptyPrefix,
		acltypes.ResourceType_KV_STAKING_VALIDATION_POWER:         stakingtypes.LastValidatorPowerKey,
		acltypes.ResourceType_KV_STAKING_TOTAL_POWER:              stakingtypes.LastTotalPowerKey,
		acltypes.ResourceType_KV_STAKING_VALIDATOR:                stakingtypes.ValidatorsKey,
		acltypes.ResourceType_KV_STAKING_VALIDATORS_CON_ADDR:      stakingtypes.ValidatorsByConsAddrKey,
		acltypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER:      stakingtypes.ValidatorsByPowerIndexKey,
		acltypes.ResourceType_KV_STAKING_DELEGATION:               stakingtypes.DelegationKey,
		acltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION:     stakingtypes.UnbondingDelegationKey,
		acltypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION_VAL: stakingtypes.UnbondingDelegationByValIndexKey,
		acltypes.ResourceType_KV_STAKING_REDELEGATION:             stakingtypes.RedelegationKey,
		acltypes.ResourceType_KV_STAKING_REDELEGATION_VAL_SRC:     stakingtypes.RedelegationByValSrcIndexKey,
		acltypes.ResourceType_KV_STAKING_REDELEGATION_VAL_DST:     stakingtypes.RedelegationByValDstIndexKey,
		acltypes.ResourceType_KV_STAKING_UNBONDING:                stakingtypes.UnbondingQueueKey,
		acltypes.ResourceType_KV_STAKING_REDELEGATION_QUEUE:       stakingtypes.RedelegationQueueKey,
		acltypes.ResourceType_KV_STAKING_VALIDATOR_QUEUE:          stakingtypes.ValidatorQueueKey,
		acltypes.ResourceType_KV_STAKING_HISTORICAL_INFO:          stakingtypes.HistoricalInfoKey,
	},
	slashingtypes.StoreKey: {
		acltypes.ResourceType_KV_SLASHING:                          acltypes.EmptyPrefix,
		acltypes.ResourceType_KV_SLASHING_VAL_SIGNING_INFO:         slashingtypes.ValidatorSigningInfoKeyPrefix,
		acltypes.ResourceType_KV_SLASHING_ADDR_PUBKEY_RELATION_KEY: slashingtypes.AddrPubkeyRelationKeyPrefix,
	},
	types.StoreKey: {
		acltypes.ResourceType_KV_ACCESSCONTROL:                         acltypes.EmptyPrefix,
		acltypes.ResourceType_KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING: types.GetWasmMappingKey(),
	},
}

func MessageDependencyGeneratorTestHelper() aclkeeper.DependencyGeneratorMap {
	return aclkeeper.DependencyGeneratorMap{
		types.GenerateMessageKey(&banktypes.MsgSend{}):        BankSendDepGenerator,
		types.GenerateMessageKey(&stakingtypes.MsgDelegate{}): StakingDelegateDepGenerator,
	}
}

func BankSendDepGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]acltypes.AccessOperation, error) {
	bankSend, ok := msg.(*banktypes.MsgSend)
	if !ok {
		return []acltypes.AccessOperation{}, fmt.Errorf("invalid message received for BankMsgSend")
	}
	accessOps := []acltypes.AccessOperation{
		{ResourceType: acltypes.ResourceType_KV_BANK_BALANCES, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: bankSend.FromAddress},
		{ResourceType: acltypes.ResourceType_KV_BANK_BALANCES, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: bankSend.ToAddress},
		*types.CommitAccessOp(),
	}
	return accessOps, nil
}

// this is intentionally missing a commit so it fails validation
func StakingDelegateDepGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]acltypes.AccessOperation, error) {
	stakingDelegate, ok := msg.(*stakingtypes.MsgDelegate)
	if !ok {
		return []acltypes.AccessOperation{}, fmt.Errorf("invalid message received for StakingDelegate")
	}
	accessOps := []acltypes.AccessOperation{
		{ResourceType: acltypes.ResourceType_KV_STAKING, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: stakingDelegate.DelegatorAddress},
	}
	return accessOps, nil
}
