package cosmosmetrics

import "github.com/prometheus/client_golang/prometheus"

func desc(name, help string, labels ...string) *prometheus.Desc {
	return prometheus.NewDesc(name, help, labels, nil)
}

var (
	descParamsMaxValidators           = desc("cosmos_params_max_validators", "Active set length")
	descParamsUnbondingTime           = desc("cosmos_params_unbonding_time", "Unbonding time, in seconds")
	descParamsDowntimeJailDuration    = desc("cosmos_params_downtail_jail_duration", "Downtime jail duration, in seconds")
	descParamsMinSignedPerWindow      = desc("cosmos_params_min_signed_per_window", "Minimal amount of blocks to sign per window to avoid slashing")
	descParamsSignedBlocksWindow      = desc("cosmos_params_signed_blocks_window", "Signed blocks window")
	descParamsSlashFractionDoubleSign = desc("cosmos_params_slash_fraction_double_sign", "% of tokens to be slashed if double signing")
	descParamsSlashFractionDowntime   = desc("cosmos_params_slash_fraction_downtime", "% of tokens to be slashed if downtime")
	descParamsBaseProposerReward      = desc("cosmos_params_base_proposer_reward", "Base proposer reward")
	descParamsBonusProposerReward     = desc("cosmos_params_bonus_proposer_reward", "Bonus proposer reward")
	descParamsCommunityTax            = desc("cosmos_params_community_tax", "Community tax")

	descGeneralBondedTokens    = desc("cosmos_general_bonded_tokens", "Bonded tokens")
	descGeneralNotBondedTokens = desc("cosmos_general_not_bonded_tokens", "Not bonded tokens")
	descGeneralCommunityPool   = desc("cosmos_general_community_pool", "Community pool", "denom")
	descGeneralSupplyTotal     = desc("cosmos_general_supply_total", "Total supply", "denom")

	descValidatorsCommission        = desc("cosmos_validators_commission", "Commission of the validator", "address", "moniker")
	descValidatorsStatus            = desc("cosmos_validators_status", "Status of the validator", "address", "moniker")
	descValidatorsJailed            = desc("cosmos_validators_jailed", "1 if the validator is jailed, 0 if no", "address", "moniker")
	descValidatorsTokens            = desc("cosmos_validators_tokens", "Tokens of the validator", "address", "moniker", "denom")
	descValidatorsDelegatorShares   = desc("cosmos_validators_delegator_shares", "Delegator shares of the validator", "address", "moniker", "denom")
	descValidatorsMinSelfDelegation = desc("cosmos_validators_min_self_delegation", "Min self delegation of the validator", "address", "moniker", "denom")
	descValidatorsMissedBlocks      = desc("cosmos_validators_missed_blocks", "Missed blocks of the validator", "address", "moniker")
	descValidatorsRank              = desc("cosmos_validators_rank", "Rank of the validator", "address", "moniker")
	descValidatorsActive            = desc("cosmos_validators_active", "1 if the validator is in the active set, 0 if no", "address", "pubkey_hash", "moniker")

	descWalletBalance       = desc("cosmos_wallet_balance", "Balance of the wallet", "address", "denom")
	descWalletDelegations   = desc("cosmos_wallet_delegations", "Delegations of the wallet", "address", "denom", "delegated_to")
	descWalletUnbondings    = desc("cosmos_wallet_unbondings", "Unbondings of the wallet", "address", "denom", "unbonded_from")
	descWalletRedelegations = desc("cosmos_wallet_redelegations", "Redelegations of the wallet", "address", "denom", "redelegated_from", "redelegated_to")
	descWalletRewards       = desc("cosmos_wallet_rewards", "Rewards of the wallet", "address", "denom", "validator_address")

	descOracleVotePenaltyCount = desc("cosmos_oracle_vote_penalty_count", "Oracle vote penalty counters of the validator", "address", "moniker", "type")
)
