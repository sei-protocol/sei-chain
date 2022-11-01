package utils

import (
	aclsdktypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	epochtypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

var StoreKeyToResourceTypePrefixMap = aclsdktypes.StoreKeyToResourceTypePrefixMap{
	aclsdktypes.ParentNodeKey: {
		aclsdktypes.ResourceType_ANY: aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV:  aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_Mem: aclsdktypes.EmptyPrefix,
	},
	dextypes.StoreKey: {
		aclsdktypes.ResourceType_KV_DEX: aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_DexMem: aclsdktypes.EmptyPrefix,
	},
	banktypes.StoreKey: {
		aclsdktypes.ResourceType_KV_BANK:          aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_BANK_BALANCES: banktypes.BalancesPrefix,
		aclsdktypes.ResourceType_KV_BANK_SUPPLY:   banktypes.SupplyKey,
		aclsdktypes.ResourceType_KV_BANK_DENOM:    banktypes.DenomMetadataPrefix,
	},
	authtypes.StoreKey: {
		aclsdktypes.ResourceType_KV_AUTH:               aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_AUTH_ADDRESS_STORE: authtypes.AddressStoreKeyPrefix,
	},
	oracletypes.StoreKey: {
		aclsdktypes.ResourceType_KV_ORACLE:                 aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_ORACLE_VOTE_TARGETS:    oracletypes.VoteTargetKey,
		aclsdktypes.ResourceType_KV_ORACLE_AGGREGATE_VOTES: oracletypes.AggregateExchangeRateVoteKey,
		aclsdktypes.ResourceType_KV_ORACLE_FEEDERS:         oracletypes.FeederDelegationKey,
	},
	stakingtypes.StoreKey: {
		aclsdktypes.ResourceType_KV_STAKING:            aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_STAKING_DELEGATION: stakingtypes.DelegationKey,
		aclsdktypes.ResourceType_KV_STAKING_VALIDATOR:  stakingtypes.ValidatorsKey,
	},
	tokenfactorytypes.StoreKey: {
		aclsdktypes.ResourceType_KV_TOKENFACTORY: aclsdktypes.EmptyPrefix,
	},
	epochtypes.StoreKey: {
		aclsdktypes.ResourceType_KV_EPOCH: aclsdktypes.EmptyPrefix,
	},
}
