package cosmosmetrics

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("cosmosmetrics")

	cosmosMetrics = struct {
		paramsMaxValidators           metric.Float64ObservableGauge
		paramsUnbondingTime           metric.Float64ObservableGauge
		paramsDowntimeJailDuration    metric.Float64ObservableGauge
		paramsMinSignedPerWindow      metric.Float64ObservableGauge
		paramsSignedBlocksWindow      metric.Float64ObservableGauge
		paramsSlashFractionDoubleSign metric.Float64ObservableGauge
		paramsSlashFractionDowntime   metric.Float64ObservableGauge
		paramsBaseProposerReward      metric.Float64ObservableGauge
		paramsBonusProposerReward     metric.Float64ObservableGauge
		paramsCommunityTax            metric.Float64ObservableGauge
		generalBondedTokens           metric.Float64ObservableGauge
		generalNotBondedTokens        metric.Float64ObservableGauge
		generalCommunityPool          metric.Float64ObservableGauge
		generalSupplyTotal            metric.Float64ObservableGauge
		validatorsCommission          metric.Float64ObservableGauge
		validatorsStatus              metric.Float64ObservableGauge
		validatorsJailed              metric.Float64ObservableGauge
		validatorsTokens              metric.Float64ObservableGauge
		validatorsDelegatorShares     metric.Float64ObservableGauge
		validatorsMinSelfDelegation   metric.Float64ObservableGauge
		validatorsMissedBlocks        metric.Float64ObservableGauge
		validatorsRank                metric.Float64ObservableGauge
		validatorsActive              metric.Float64ObservableGauge
		walletBalance                 metric.Float64ObservableGauge
		walletDelegations             metric.Float64ObservableGauge
		walletUnbondings              metric.Float64ObservableGauge
		walletRedelegations           metric.Float64ObservableGauge
		walletRewards                 metric.Float64ObservableGauge
		bankTransfersTotal            metric.Int64Counter
		bankTransferAmountTotal       metric.Float64Counter
	}{
		paramsMaxValidators: must(meter.Float64ObservableGauge(
			"cosmos_params_max_validators",
			metric.WithDescription("Active set length"),
		)),
		paramsUnbondingTime: must(meter.Float64ObservableGauge(
			"cosmos_params_unbonding_time",
			metric.WithDescription("Unbonding time"),
			metric.WithUnit("s"),
		)),
		paramsDowntimeJailDuration: must(meter.Float64ObservableGauge(
			"cosmos_params_downtime_jail_duration",
			metric.WithDescription("Downtime jail duration"),
			metric.WithUnit("s"),
		)),
		paramsMinSignedPerWindow: must(meter.Float64ObservableGauge(
			"cosmos_params_min_signed_per_window",
			metric.WithDescription("Minimal fraction of blocks to sign per window to avoid slashing"),
		)),
		paramsSignedBlocksWindow: must(meter.Float64ObservableGauge(
			"cosmos_params_signed_blocks_window",
			metric.WithDescription("Signed blocks window"),
		)),
		paramsSlashFractionDoubleSign: must(meter.Float64ObservableGauge(
			"cosmos_params_slash_fraction_double_sign",
			metric.WithDescription("Fraction of tokens slashed for double signing"),
		)),
		paramsSlashFractionDowntime: must(meter.Float64ObservableGauge(
			"cosmos_params_slash_fraction_downtime",
			metric.WithDescription("Fraction of tokens slashed for downtime"),
		)),
		paramsBaseProposerReward: must(meter.Float64ObservableGauge(
			"cosmos_params_base_proposer_reward",
			metric.WithDescription("Base proposer reward"),
		)),
		paramsBonusProposerReward: must(meter.Float64ObservableGauge(
			"cosmos_params_bonus_proposer_reward",
			metric.WithDescription("Bonus proposer reward"),
		)),
		paramsCommunityTax: must(meter.Float64ObservableGauge(
			"cosmos_params_community_tax",
			metric.WithDescription("Community tax"),
		)),

		generalBondedTokens: must(meter.Float64ObservableGauge(
			"cosmos_general_bonded_tokens",
			metric.WithDescription("Bonded tokens, in base units"),
		)),
		generalNotBondedTokens: must(meter.Float64ObservableGauge(
			"cosmos_general_not_bonded_tokens",
			metric.WithDescription("Not bonded tokens, in base units"),
		)),
		generalCommunityPool: must(meter.Float64ObservableGauge(
			"cosmos_general_community_pool",
			metric.WithDescription("Community pool by denom"),
		)),
		generalSupplyTotal: must(meter.Float64ObservableGauge(
			"cosmos_general_supply_total",
			metric.WithDescription("Total supply of the bond denom"),
		)),

		validatorsCommission: must(meter.Float64ObservableGauge(
			"cosmos_validators_commission",
			metric.WithDescription("Commission rate of the validator"),
		)),
		validatorsStatus: must(meter.Float64ObservableGauge(
			"cosmos_validators_status",
			metric.WithDescription("Bond status of the validator"),
		)),
		validatorsJailed: must(meter.Float64ObservableGauge(
			"cosmos_validators_jailed",
			metric.WithDescription("1 if the validator is jailed"),
		)),
		validatorsTokens: must(meter.Float64ObservableGauge(
			"cosmos_validators_tokens",
			metric.WithDescription("Tokens of the validator"),
		)),
		validatorsDelegatorShares: must(meter.Float64ObservableGauge(
			"cosmos_validators_delegator_shares",
			metric.WithDescription("Delegator shares of the validator"),
		)),
		validatorsMinSelfDelegation: must(meter.Float64ObservableGauge(
			"cosmos_validators_min_self_delegation",
			metric.WithDescription("Min self delegation of the validator"),
		)),
		validatorsMissedBlocks: must(meter.Float64ObservableGauge(
			"cosmos_validators_missed_blocks",
			metric.WithDescription("Missed blocks of the validator in the current window"),
		)),
		validatorsRank: must(meter.Float64ObservableGauge(
			"cosmos_validators_rank",
			metric.WithDescription("Rank of the validator by tokens"),
		)),
		validatorsActive: must(meter.Float64ObservableGauge(
			"cosmos_validators_active",
			metric.WithDescription("1 if the validator is in the active set"),
		)),

		walletBalance: must(meter.Float64ObservableGauge(
			"cosmos_wallet_balance",
			metric.WithDescription("Balance of the wallet"),
		)),
		walletDelegations: must(meter.Float64ObservableGauge(
			"cosmos_wallet_delegations",
			metric.WithDescription("Delegations of the wallet by validator"),
		)),
		walletUnbondings: must(meter.Float64ObservableGauge(
			"cosmos_wallet_unbondings",
			metric.WithDescription("Unbondings of the wallet by validator"),
		)),
		walletRedelegations: must(meter.Float64ObservableGauge(
			"cosmos_wallet_redelegations",
			metric.WithDescription("Redelegations of the wallet by validator pair"),
		)),
		walletRewards: must(meter.Float64ObservableGauge(
			"cosmos_wallet_rewards",
			metric.WithDescription("Pending rewards of the wallet by validator and denom"),
		)),

		bankTransfersTotal: must(meter.Int64Counter(
			"cosmos_bank_transfers_total",
			metric.WithDescription("Bond-denom bank transfers at or above the configured threshold"),
		)),
		bankTransferAmountTotal: must(meter.Float64Counter(
			"cosmos_bank_transfer_amount_total",
			metric.WithDescription("Summed bond-denom amount of bank transfers at or above the configured threshold, in base units"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

// observables lists every asynchronous instrument, for RegisterCallback.
func observables() []metric.Observable {
	m := &cosmosMetrics
	return []metric.Observable{
		m.paramsMaxValidators,
		m.paramsUnbondingTime,
		m.paramsDowntimeJailDuration,
		m.paramsMinSignedPerWindow,
		m.paramsSignedBlocksWindow,
		m.paramsSlashFractionDoubleSign,
		m.paramsSlashFractionDowntime,
		m.paramsBaseProposerReward,
		m.paramsBonusProposerReward,
		m.paramsCommunityTax,
		m.generalBondedTokens,
		m.generalNotBondedTokens,
		m.generalCommunityPool,
		m.generalSupplyTotal,
		m.validatorsCommission,
		m.validatorsStatus,
		m.validatorsJailed,
		m.validatorsTokens,
		m.validatorsDelegatorShares,
		m.validatorsMinSelfDelegation,
		m.validatorsMissedBlocks,
		m.validatorsRank,
		m.validatorsActive,
		m.walletBalance,
		m.walletDelegations,
		m.walletUnbondings,
		m.walletRedelegations,
		m.walletRewards,
	}
}
