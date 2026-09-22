package cosmosmetrics

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("cosmosmetrics")

	cosmosMetrics = newInstruments()
)

// instruments are the gauges the reporter reports, all under one meter.
type instruments struct {
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

	generalBondedTokens    metric.Float64ObservableGauge
	generalNotBondedTokens metric.Float64ObservableGauge
	generalCommunityPool   metric.Float64ObservableGauge
	generalSupplyTotal     metric.Float64ObservableGauge

	validatorsCommission        metric.Float64ObservableGauge
	validatorsStatus            metric.Float64ObservableGauge
	validatorsJailed            metric.Float64ObservableGauge
	validatorsTokens            metric.Float64ObservableGauge
	validatorsDelegatorShares   metric.Float64ObservableGauge
	validatorsMinSelfDelegation metric.Float64ObservableGauge
	validatorsMissedBlocks      metric.Float64ObservableGauge
	validatorsRank              metric.Float64ObservableGauge
	validatorsActive            metric.Float64ObservableGauge

	walletBalance       metric.Float64ObservableGauge
	walletDelegations   metric.Float64ObservableGauge
	walletUnbondings    metric.Float64ObservableGauge
	walletRedelegations metric.Float64ObservableGauge
	walletRewards       metric.Float64ObservableGauge

	bankTransferAmount metric.Float64Gauge
}

func newInstruments() *instruments {
	gauge := func(name, description string, opts ...metric.Float64ObservableGaugeOption) metric.Float64ObservableGauge {
		return must(meter.Float64ObservableGauge(name, append([]metric.Float64ObservableGaugeOption{metric.WithDescription(description)}, opts...)...))
	}
	seconds := metric.WithUnit("s")
	return &instruments{
		paramsMaxValidators:           gauge("cosmos_params_max_validators", "Active set length"),
		paramsUnbondingTime:           gauge("cosmos_params_unbonding_time", "Unbonding time", seconds),
		paramsDowntimeJailDuration:    gauge("cosmos_params_downtime_jail_duration", "Downtime jail duration", seconds),
		paramsMinSignedPerWindow:      gauge("cosmos_params_min_signed_per_window", "Minimal fraction of blocks to sign per window to avoid slashing"),
		paramsSignedBlocksWindow:      gauge("cosmos_params_signed_blocks_window", "Signed blocks window"),
		paramsSlashFractionDoubleSign: gauge("cosmos_params_slash_fraction_double_sign", "Fraction of tokens slashed for double signing"),
		paramsSlashFractionDowntime:   gauge("cosmos_params_slash_fraction_downtime", "Fraction of tokens slashed for downtime"),
		paramsBaseProposerReward:      gauge("cosmos_params_base_proposer_reward", "Base proposer reward"),
		paramsBonusProposerReward:     gauge("cosmos_params_bonus_proposer_reward", "Bonus proposer reward"),
		paramsCommunityTax:            gauge("cosmos_params_community_tax", "Community tax"),

		generalBondedTokens:    gauge("cosmos_general_bonded_tokens", "Bonded tokens, in base units"),
		generalNotBondedTokens: gauge("cosmos_general_not_bonded_tokens", "Not bonded tokens, in base units"),
		generalCommunityPool:   gauge("cosmos_general_community_pool", "Community pool by denom"),
		generalSupplyTotal:     gauge("cosmos_general_supply_total", "Total supply of the bond denom"),

		validatorsCommission:        gauge("cosmos_validators_commission", "Commission rate of the validator"),
		validatorsStatus:            gauge("cosmos_validators_status", "Bond status of the validator"),
		validatorsJailed:            gauge("cosmos_validators_jailed", "1 if the validator is jailed"),
		validatorsTokens:            gauge("cosmos_validators_tokens", "Tokens of the validator"),
		validatorsDelegatorShares:   gauge("cosmos_validators_delegator_shares", "Delegator shares of the validator"),
		validatorsMinSelfDelegation: gauge("cosmos_validators_min_self_delegation", "Min self delegation of the validator"),
		validatorsMissedBlocks:      gauge("cosmos_validators_missed_blocks", "Missed blocks of the validator in the current window"),
		validatorsRank:              gauge("cosmos_validators_rank", "Rank of the validator by tokens"),
		validatorsActive:            gauge("cosmos_validators_active", "1 if the validator is in the active set"),

		walletBalance:       gauge("cosmos_wallet_balance", "Balance of the wallet"),
		walletDelegations:   gauge("cosmos_wallet_delegations", "Delegations of the wallet by validator"),
		walletUnbondings:    gauge("cosmos_wallet_unbondings", "Unbondings of the wallet by validator"),
		walletRedelegations: gauge("cosmos_wallet_redelegations", "Redelegations of the wallet by validator pair"),
		walletRewards:       gauge("cosmos_wallet_rewards", "Pending rewards of the wallet by validator and denom"),

		bankTransferAmount: must(meter.Float64Gauge(
			"cosmos_bank_transfer_amount",
			metric.WithDescription("Amount of the last bank transfer at or above the configured threshold, in base units"),
		)),
	}
}

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

// observables lists every asynchronous instrument, for RegisterCallback.
func (i *instruments) observables() []metric.Observable {
	return []metric.Observable{
		i.paramsMaxValidators, i.paramsUnbondingTime, i.paramsDowntimeJailDuration, i.paramsMinSignedPerWindow,
		i.paramsSignedBlocksWindow, i.paramsSlashFractionDoubleSign, i.paramsSlashFractionDowntime,
		i.paramsBaseProposerReward, i.paramsBonusProposerReward, i.paramsCommunityTax,
		i.generalBondedTokens, i.generalNotBondedTokens, i.generalCommunityPool, i.generalSupplyTotal,
		i.validatorsCommission, i.validatorsStatus, i.validatorsJailed, i.validatorsTokens,
		i.validatorsDelegatorShares, i.validatorsMinSelfDelegation, i.validatorsMissedBlocks,
		i.validatorsRank, i.validatorsActive,
		i.walletBalance, i.walletDelegations, i.walletUnbondings, i.walletRedelegations, i.walletRewards,
	}
}
