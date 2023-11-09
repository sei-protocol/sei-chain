package utils

import (
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	aclsdktypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	feegranttypes "github.com/cosmos/cosmos-sdk/x/feegrant"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	epochtypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

const (
	DefaultIDTemplate = "*"
)

var StoreKeyToResourceTypePrefixMap = aclsdktypes.StoreKeyToResourceTypePrefixMap{
	aclsdktypes.ParentNodeKey: {
		aclsdktypes.ResourceType_ANY: aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV:  aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_Mem: aclsdktypes.EmptyPrefix,
	},
	dextypes.StoreKey: {
		aclsdktypes.ResourceType_KV_DEX:                    aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_DexMem:                    aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_DEX_CONTRACT_LONGBOOK:  dextypes.KeyPrefix(dextypes.LongBookKey),
		aclsdktypes.ResourceType_KV_DEX_CONTRACT_SHORTBOOK: dextypes.KeyPrefix(dextypes.ShortBookKey),
		// pricedenom and assetdenoms are the prefixes
		aclsdktypes.ResourceType_KV_DEX_PAIR_PREFIX:           aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_DEX_TWAP:                  dextypes.KeyPrefix(dextypes.TwapKey),
		aclsdktypes.ResourceType_KV_DEX_PRICE:                 dextypes.KeyPrefix(dextypes.PriceKey),
		aclsdktypes.ResourceType_KV_DEX_SETTLEMENT_ENTRY:      dextypes.KeyPrefix(dextypes.SettlementEntryKey),
		aclsdktypes.ResourceType_KV_DEX_REGISTERED_PAIR:       dextypes.KeyPrefix(dextypes.RegisteredPairKey),
		aclsdktypes.ResourceType_KV_DEX_ORDER:                 dextypes.KeyPrefix(dextypes.OrderKey),
		aclsdktypes.ResourceType_KV_DEX_CANCEL:                dextypes.KeyPrefix(dextypes.CancelKey),
		aclsdktypes.ResourceType_KV_DEX_ACCOUNT_ACTIVE_ORDERS: dextypes.KeyPrefix(dextypes.AccountActiveOrdersKey),
		aclsdktypes.ResourceType_KV_DEX_ASSET_LIST:            dextypes.KeyPrefix(dextypes.AssetListKey),
		aclsdktypes.ResourceType_KV_DEX_NEXT_ORDER_ID:         dextypes.KeyPrefix(dextypes.NextOrderIDKey),
		aclsdktypes.ResourceType_KV_DEX_NEXT_SETTLEMENT_ID:    dextypes.KeyPrefix(dextypes.NextSettlementIDKey),
		aclsdktypes.ResourceType_KV_DEX_MATCH_RESULT:          dextypes.KeyPrefix(dextypes.MatchResultKey),
		aclsdktypes.ResourceType_KV_DEX_CONTRACT:              dextypes.KeyPrefix(dexkeeper.ContractPrefixKey),
		aclsdktypes.ResourceType_KV_DEX_ORDER_BOOK:            dextypes.KeyPrefix(dextypes.NextOrderIDKey),
		aclsdktypes.ResourceType_KV_DEX_LONG_ORDER_COUNT:      dextypes.KeyPrefix(dextypes.LongOrderCountKey),
		aclsdktypes.ResourceType_KV_DEX_SHORT_ORDER_COUNT:     dextypes.KeyPrefix(dextypes.ShortOrderCountKey),
		// SETTLEMENT keys are prefixed with account and order id
		aclsdktypes.ResourceType_KV_DEX_SETTLEMENT_ORDER_ID: aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_DEX_SETTLEMENT:          aclsdktypes.EmptyPrefix,
	},
	dextypes.MemStoreKey: {
		// mem
		aclsdktypes.ResourceType_KV_DEX_MEM_ORDER:   dextypes.KeyPrefix(dextypes.MemOrderKey),
		aclsdktypes.ResourceType_KV_DEX_MEM_CANCEL:  dextypes.KeyPrefix(dextypes.MemCancelKey),
		aclsdktypes.ResourceType_KV_DEX_MEM_DEPOSIT: dextypes.KeyPrefix(dextypes.MemDepositKey),
	},
	banktypes.StoreKey: {
		aclsdktypes.ResourceType_KV_BANK:          aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_BANK_BALANCES: banktypes.BalancesPrefix,
		aclsdktypes.ResourceType_KV_BANK_SUPPLY:   banktypes.SupplyKey,
		aclsdktypes.ResourceType_KV_BANK_DENOM:    banktypes.DenomMetadataPrefix,
	},
	banktypes.DeferredCacheStoreKey: {
		aclsdktypes.ResourceType_KV_BANK_DEFERRED:                 aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_BANK_DEFERRED_MODULE_TX_INDEX: banktypes.DeferredCachePrefix,
	},
	authtypes.StoreKey: {
		aclsdktypes.ResourceType_KV_AUTH:                       aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_AUTH_ADDRESS_STORE:         authtypes.AddressStoreKeyPrefix,
		aclsdktypes.ResourceType_KV_AUTH_GLOBAL_ACCOUNT_NUMBER: authtypes.GlobalAccountNumberKey,
	},
	authztypes.StoreKey: {
		aclsdktypes.ResourceType_KV_AUTHZ: aclsdktypes.EmptyPrefix,
	},
	distributiontypes.StoreKey: {
		aclsdktypes.ResourceType_KV_DISTRIBUTION:                         aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_DISTRIBUTION_FEE_POOL:                distributiontypes.FeePoolKey,
		aclsdktypes.ResourceType_KV_DISTRIBUTION_PROPOSER_KEY:            distributiontypes.ProposerKey,
		aclsdktypes.ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS:     distributiontypes.ValidatorOutstandingRewardsPrefix,
		aclsdktypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR: distributiontypes.DelegatorWithdrawAddrPrefix,
		aclsdktypes.ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO: distributiontypes.DelegatorStartingInfoPrefix,
		aclsdktypes.ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS:  distributiontypes.ValidatorHistoricalRewardsPrefix,
		aclsdktypes.ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS:     distributiontypes.ValidatorCurrentRewardsPrefix,
		aclsdktypes.ResourceType_KV_DISTRIBUTION_VAL_ACCUM_COMMISSION:    distributiontypes.ValidatorAccumulatedCommissionPrefix,
		aclsdktypes.ResourceType_KV_DISTRIBUTION_SLASH_EVENT:             distributiontypes.ValidatorSlashEventPrefix,
	},
	feegranttypes.StoreKey: {
		aclsdktypes.ResourceType_KV_FEEGRANT:           aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_FEEGRANT_ALLOWANCE: feegranttypes.FeeAllowanceKeyPrefix,
	},
	oracletypes.StoreKey: {
		aclsdktypes.ResourceType_KV_ORACLE:                      aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_ORACLE_VOTE_TARGETS:         oracletypes.VoteTargetKey,
		aclsdktypes.ResourceType_KV_ORACLE_AGGREGATE_VOTES:      oracletypes.AggregateExchangeRateVoteKey,
		aclsdktypes.ResourceType_KV_ORACLE_FEEDERS:              oracletypes.FeederDelegationKey,
		aclsdktypes.ResourceType_KV_ORACLE_PRICE_SNAPSHOT:       oracletypes.PriceSnapshotKey,
		aclsdktypes.ResourceType_KV_ORACLE_EXCHANGE_RATE:        oracletypes.ExchangeRateKey,
		aclsdktypes.ResourceType_KV_ORACLE_VOTE_PENALTY_COUNTER: oracletypes.VotePenaltyCounterKey,
	},
	stakingtypes.StoreKey: {
		aclsdktypes.ResourceType_KV_STAKING:                          aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_STAKING_VALIDATION_POWER:         stakingtypes.LastValidatorPowerKey,
		aclsdktypes.ResourceType_KV_STAKING_TOTAL_POWER:              stakingtypes.LastTotalPowerKey,
		aclsdktypes.ResourceType_KV_STAKING_VALIDATOR:                stakingtypes.ValidatorsKey,
		aclsdktypes.ResourceType_KV_STAKING_VALIDATORS_CON_ADDR:      stakingtypes.ValidatorsByConsAddrKey,
		aclsdktypes.ResourceType_KV_STAKING_VALIDATORS_BY_POWER:      stakingtypes.ValidatorsByPowerIndexKey,
		aclsdktypes.ResourceType_KV_STAKING_DELEGATION:               stakingtypes.DelegationKey,
		aclsdktypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION:     stakingtypes.UnbondingDelegationKey,
		aclsdktypes.ResourceType_KV_STAKING_UNBONDING_DELEGATION_VAL: stakingtypes.UnbondingDelegationByValIndexKey,
		aclsdktypes.ResourceType_KV_STAKING_REDELEGATION:             stakingtypes.RedelegationKey,
		aclsdktypes.ResourceType_KV_STAKING_REDELEGATION_VAL_SRC:     stakingtypes.RedelegationByValSrcIndexKey,
		aclsdktypes.ResourceType_KV_STAKING_REDELEGATION_VAL_DST:     stakingtypes.RedelegationByValDstIndexKey,
		aclsdktypes.ResourceType_KV_STAKING_UNBONDING:                stakingtypes.UnbondingQueueKey,
		aclsdktypes.ResourceType_KV_STAKING_REDELEGATION_QUEUE:       stakingtypes.RedelegationQueueKey,
		aclsdktypes.ResourceType_KV_STAKING_VALIDATOR_QUEUE:          stakingtypes.ValidatorQueueKey,
		aclsdktypes.ResourceType_KV_STAKING_HISTORICAL_INFO:          stakingtypes.HistoricalInfoKey,
	},
	slashingtypes.StoreKey: {
		aclsdktypes.ResourceType_KV_SLASHING:                          aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_SLASHING_VAL_SIGNING_INFO:         slashingtypes.ValidatorSigningInfoKeyPrefix,
		aclsdktypes.ResourceType_KV_SLASHING_ADDR_PUBKEY_RELATION_KEY: slashingtypes.AddrPubkeyRelationKeyPrefix,
	},
	tokenfactorytypes.StoreKey: {
		aclsdktypes.ResourceType_KV_TOKENFACTORY:          aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_TOKENFACTORY_DENOM:    []byte(tokenfactorytypes.DenomsPrefixKey),
		aclsdktypes.ResourceType_KV_TOKENFACTORY_METADATA: []byte(tokenfactorytypes.DenomAuthorityMetadataKey),
		aclsdktypes.ResourceType_KV_TOKENFACTORY_ADMIN:    []byte(tokenfactorytypes.AdminPrefixKey),
		aclsdktypes.ResourceType_KV_TOKENFACTORY_CREATOR:  []byte(tokenfactorytypes.AdminPrefixKey),
	},
	epochtypes.StoreKey: {
		aclsdktypes.ResourceType_KV_EPOCH: aclsdktypes.EmptyPrefix,
	},
	acltypes.StoreKey: {
		aclsdktypes.ResourceType_KV_ACCESSCONTROL:                         aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING: acltypes.GetWasmMappingKey(),
	},
	wasmtypes.StoreKey: {
		aclsdktypes.ResourceType_KV_WASM:                       aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_WASM_CODE:                  wasmtypes.CodeKeyPrefix,
		aclsdktypes.ResourceType_KV_WASM_CONTRACT_ADDRESS:      wasmtypes.ContractKeyPrefix,
		aclsdktypes.ResourceType_KV_WASM_CONTRACT_STORE:        wasmtypes.ContractStorePrefix,
		aclsdktypes.ResourceType_KV_WASM_SEQUENCE_KEY:          wasmtypes.SequenceKeyPrefix,
		aclsdktypes.ResourceType_KV_WASM_CONTRACT_CODE_HISTORY: wasmtypes.ContractCodeHistoryElementPrefix,
		aclsdktypes.ResourceType_KV_WASM_CONTRACT_BY_CODE_ID:   wasmtypes.ContractByCodeIDAndCreatedSecondaryIndexPrefix,
		aclsdktypes.ResourceType_KV_WASM_PINNED_CODE_INDEX:     wasmtypes.PinnedCodeIndexPrefix,
	},
	evmtypes.StoreKey: {
		aclsdktypes.ResourceType_KV_EVM:         aclsdktypes.EmptyPrefix,
		aclsdktypes.ResourceType_KV_EVM_BALANCE: evmtypes.BalanceKeyPrefix,
	},
}
