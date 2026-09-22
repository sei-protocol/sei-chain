package main

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	querytypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	distributiontypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	"google.golang.org/grpc"
)

func ValidatorHandler(w http.ResponseWriter, r *http.Request, grpcConn *grpc.ClientConn) {
	requestStart := time.Now()
	sublogger := log.With().
		Str("request-id", uuid.New().String()).
		Logger()

	address := r.URL.Query().Get("address")
	myAddress, err := sdk.ValAddressFromBech32(address)
	if err != nil {
		sublogger.Error().
			Str("address", address).
			Err(err).
			Msg("Could not get address")
		return
	}

	validatorDelegationsGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_delegations",
			Help:        "Delegations of the Cosmos-based blockchain validator",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker", "denom", "delegated_by"},
	)

	validatorTokensGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_tokens",
			Help:        "Tokens of the Cosmos-based blockchain validator",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker", "denom"},
	)

	validatorDelegatorSharesGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_delegators_shares",
			Help:        "Delegators shares of the Cosmos-based blockchain validator",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker", "denom"},
	)

	validatorCommissionRateGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_commission_rate",
			Help:        "Commission rate of the Cosmos-based blockchain validator",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker"},
	)
	validatorCommissionGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_commission",
			Help:        "Commission of the Cosmos-based blockchain validator",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker", "denom"},
	)

	validatorRewardsGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_rewards",
			Help:        "Rewards of the Cosmos-based blockchain validator",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker", "denom"},
	)

	validatorUnbondingsGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_unbondings",
			Help:        "Unbondings of the Cosmos-based blockchain validator",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker", "denom", "unbonded_by"},
	)

	validatorRedelegationsGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_redelegations",
			Help:        "Redelegations of the Cosmos-based blockchain validator",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker", "denom", "redelegated_by", "redelegated_to"},
	)

	validatorMissedBlocksGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_missed_blocks",
			Help:        "Missed blocks of the Cosmos-based blockchain validator",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker"},
	)

	validatorRankGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_rank",
			Help:        "Rank of the Cosmos-based blockchain validator",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker"},
	)

	validatorIsActiveGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_active",
			Help:        "1 if the Cosmos-based blockchain validator is in active set, 0 if no",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker"},
	)

	validatorStatusGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_status",
			Help:        "Status of the Cosmos-based blockchain validator",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker"},
	)

	validatorJailedGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_validator_jailed",
			Help:        "1 if the Cosmos-based blockchain validator is jailed, 0 if no",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "moniker"},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(validatorDelegationsGauge)
	registry.MustRegister(validatorTokensGauge)
	registry.MustRegister(validatorDelegatorSharesGauge)
	registry.MustRegister(validatorCommissionRateGauge)
	registry.MustRegister(validatorCommissionGauge)
	registry.MustRegister(validatorRewardsGauge)
	registry.MustRegister(validatorUnbondingsGauge)
	registry.MustRegister(validatorRedelegationsGauge)
	registry.MustRegister(validatorMissedBlocksGauge)
	registry.MustRegister(validatorRankGauge)
	registry.MustRegister(validatorIsActiveGauge)
	registry.MustRegister(validatorStatusGauge)
	registry.MustRegister(validatorJailedGauge)

	// doing this not in goroutine as we'll need the moniker value later
	sublogger.Debug().
		Str("address", address).
		Msg("Started querying validator")
	validatorQueryStart := time.Now()

	stakingClient := stakingtypes.NewQueryClient(grpcConn)
	validator, err := stakingClient.Validator(
		context.Background(),
		&stakingtypes.QueryValidatorRequest{ValidatorAddr: myAddress.String()},
	)
	if err != nil {
		sublogger.Error().
			Str("address", address).
			Err(err).
			Msg("Could not get validator")
		return
	}

	sublogger.Debug().
		Str("address", address).
		Float64("request-time", time.Since(validatorQueryStart).Seconds()).
		Msg("Finished querying validator")

	if value, err := strconv.ParseFloat(validator.Validator.Tokens.String(), 64); err != nil {
		sublogger.Error().
			Str("address", address).
			Err(err).
			Msg("Could not parse validator tokens")
	} else {
		validatorTokensGauge.With(prometheus.Labels{
			"address": validator.Validator.OperatorAddress,
			"moniker": validator.Validator.Description.Moniker,
			"denom":   Denom,
		}).Set(value / DenomCoefficient)
	}

	// because cosmos's dec doesn't have .toFloat64() method or whatever and returns everything as int
	if value, err := strconv.ParseFloat(validator.Validator.DelegatorShares.String(), 64); err != nil {
		sublogger.Error().
			Str("address", address).
			Err(err).
			Msg("Could not parse delegator shares")
	} else {
		validatorDelegatorSharesGauge.With(prometheus.Labels{
			"address": validator.Validator.OperatorAddress,
			"moniker": validator.Validator.Description.Moniker,
			"denom":   Denom,
		}).Set(value / DenomCoefficient)
	}

	// because cosmos's dec doesn't have .toFloat64() method or whatever and returns everything as int
	if rate, err := strconv.ParseFloat(validator.Validator.Commission.Rate.String(), 64); err != nil {
		sublogger.Error().
			Str("address", address).
			Err(err).
			Msg("Could not parse commission rate")
	} else {
		validatorCommissionRateGauge.With(prometheus.Labels{
			"address": validator.Validator.OperatorAddress,
			"moniker": validator.Validator.Description.Moniker,
		}).Set(rate)
	}

	validatorStatusGauge.With(prometheus.Labels{
		"address": validator.Validator.OperatorAddress,
		"moniker": validator.Validator.Description.Moniker,
	}).Set(float64(validator.Validator.Status))

	// golang doesn't have a ternary operator, so we have to stick with this ugly solution
	var jailed float64

	if validator.Validator.Jailed {
		jailed = 1
	} else {
		jailed = 0
	}
	validatorJailedGauge.With(prometheus.Labels{
		"address": validator.Validator.OperatorAddress,
		"moniker": validator.Validator.Description.Moniker,
	}).Set(jailed)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		sublogger.Debug().
			Str("address", address).
			Msg("Started querying validator delegations")
		queryStart := time.Now()

		stakingClient := stakingtypes.NewQueryClient(grpcConn)
		stakingRes, err := stakingClient.ValidatorDelegations(
			context.Background(),
			&stakingtypes.QueryValidatorDelegationsRequest{
				ValidatorAddr: myAddress.String(),
				Pagination: &querytypes.PageRequest{
					Limit: Limit,
				},
			},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get validator delegations")
			return
		}

		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying validator delegations")

		for _, delegation := range stakingRes.DelegationResponses {
			value, err := strconv.ParseFloat(delegation.Balance.Amount.String(), 64)
			if err != nil {
				log.Error().
					Err(err).
					Str("address", address).
					Msg("Could not convert delegation entry")
			} else {
				validatorDelegationsGauge.With(prometheus.Labels{
					"moniker":      validator.Validator.Description.Moniker,
					"address":      delegation.Delegation.ValidatorAddress,
					"denom":        Denom,
					"delegated_by": delegation.Delegation.DelegatorAddress,
				}).Set(value / DenomCoefficient)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		sublogger.Debug().
			Str("address", address).
			Msg("Started querying validator commission")
		queryStart := time.Now()

		distributionClient := distributiontypes.NewQueryClient(grpcConn)
		distributionRes, err := distributionClient.ValidatorCommission(
			context.Background(),
			&distributiontypes.QueryValidatorCommissionRequest{ValidatorAddress: myAddress.String()},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get validator commission")
			return
		}

		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying validator commission")

		for _, commission := range distributionRes.Commission.Commission {
			// because cosmos's dec doesn't have .toFloat64() method or whatever and returns everything as int
			value, err := strconv.ParseFloat(commission.Amount.String(), 64)
			if err != nil {
				log.Error().
					Err(err).
					Str("address", address).
					Msg("Could not get validator commission")
			} else {
				validatorCommissionGauge.With(prometheus.Labels{
					"address": address,
					"moniker": validator.Validator.Description.Moniker,
					"denom":   Denom,
				}).Set(value / DenomCoefficient)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		sublogger.Debug().
			Str("address", address).
			Msg("Started querying validator rewards")
		queryStart := time.Now()

		distributionClient := distributiontypes.NewQueryClient(grpcConn)
		distributionRes, err := distributionClient.ValidatorOutstandingRewards(
			context.Background(),
			&distributiontypes.QueryValidatorOutstandingRewardsRequest{ValidatorAddress: myAddress.String()},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get validator rewards")
			return
		}

		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying validator rewards")

		for _, reward := range distributionRes.Rewards.Rewards {
			// because cosmos's dec doesn't have .toFloat64() method or whatever and returns everything as int
			if value, err := strconv.ParseFloat(reward.Amount.String(), 64); err != nil {
				sublogger.Error().
					Str("address", address).
					Err(err).
					Msg("Could not get reward")
			} else {
				validatorRewardsGauge.With(prometheus.Labels{
					"address": address,
					"moniker": validator.Validator.Description.Moniker,
					"denom":   Denom,
				}).Set(value / DenomCoefficient)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		sublogger.Debug().
			Str("address", address).
			Msg("Started querying validator unbonding delegations")
		queryStart := time.Now()

		stakingClient := stakingtypes.NewQueryClient(grpcConn)
		stakingRes, err := stakingClient.ValidatorUnbondingDelegations(
			context.Background(),
			&stakingtypes.QueryValidatorUnbondingDelegationsRequest{ValidatorAddr: myAddress.String()},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get validator unbonding delegations")
			return
		}

		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying validator unbonding delegations")

		for _, unbonding := range stakingRes.UnbondingResponses {
			var sum float64 = 0
			for _, entry := range unbonding.Entries {
				value, err := strconv.ParseFloat(entry.Balance.String(), 64)
				if err != nil {
					log.Error().
						Err(err).
						Str("address", address).
						Msg("Could not convert unbonding delegation entry")
				} else {
					sum += value
				}
			}

			validatorUnbondingsGauge.With(prometheus.Labels{
				"address":     unbonding.ValidatorAddress,
				"moniker":     validator.Validator.Description.Moniker,
				"denom":       Denom, // unbonding does not have denom in response for some reason
				"unbonded_by": unbonding.DelegatorAddress,
			}).Set(sum / DenomCoefficient)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		sublogger.Debug().
			Str("address", address).
			Msg("Started querying validator redelegations")
		queryStart := time.Now()

		stakingClient := stakingtypes.NewQueryClient(grpcConn)
		stakingRes, err := stakingClient.Redelegations(
			context.Background(),
			&stakingtypes.QueryRedelegationsRequest{SrcValidatorAddr: myAddress.String()},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get redelegations")
			return
		}

		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying validator redelegations")

		for _, redelegation := range stakingRes.RedelegationResponses {
			var sum float64 = 0
			for _, entry := range redelegation.Entries {
				value, err := strconv.ParseFloat(entry.Balance.String(), 64)
				if err != nil {
					log.Error().
						Err(err).
						Str("address", address).
						Msg("Could not convert redelegation entry")
				} else {
					sum += value
				}
			}

			validatorRedelegationsGauge.With(prometheus.Labels{
				"address":        redelegation.Redelegation.ValidatorSrcAddress,
				"moniker":        validator.Validator.Description.Moniker,
				"denom":          Denom, // redelegation does not have denom in response for some reason
				"redelegated_by": redelegation.Redelegation.DelegatorAddress,
				"redelegated_to": redelegation.Redelegation.ValidatorDstAddress,
			}).Set(sum / DenomCoefficient)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		sublogger.Debug().
			Str("address", address).
			Msg("Started querying validator signing info")
		queryStart := time.Now()

		interfaceRegistry := newInterfaceRegistry()

		err := validator.Validator.UnpackInterfaces(interfaceRegistry) // Unpack interfaces, to populate the Anys' cached values
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get unpack validator inferfaces")
		}

		pubKey, err := validator.Validator.GetConsAddr()
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get validator pubkey")
		}

		slashingClient := slashingtypes.NewQueryClient(grpcConn)
		slashingRes, err := slashingClient.SigningInfo(
			context.Background(),
			&slashingtypes.QuerySigningInfoRequest{ConsAddress: pubKey.String()},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get validator signing info")
			return
		}

		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying validator signing info")

		sublogger.Debug().
			Str("address", address).
			Int64("missedBlocks", slashingRes.ValSigningInfo.MissedBlocksCounter).
			Msg("Finished querying validator signing info")

		validatorMissedBlocksGauge.With(prometheus.Labels{
			"moniker": validator.Validator.Description.Moniker,
			"address": address,
		}).Set(float64(slashingRes.ValSigningInfo.MissedBlocksCounter))
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		sublogger.Debug().
			Str("address", address).
			Msg("Started querying validator other validators")
		queryStart := time.Now()

		stakingClient := stakingtypes.NewQueryClient(grpcConn)
		stakingRes, err := stakingClient.Validators(
			context.Background(),
			&stakingtypes.QueryValidatorsRequest{
				Pagination: &querytypes.PageRequest{
					Limit: Limit,
				},
			},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get other validators")
			return
		}

		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying validator other validators")

		validators := stakingRes.Validators

		// sorting by delegator shares to display rankings
		sort.Slice(validators, func(i, j int) bool {
			firstShares, firstErr := strconv.ParseFloat(validators[i].DelegatorShares.String(), 64)
			secondShares, secondErr := strconv.ParseFloat(validators[j].DelegatorShares.String(), 64)

			if firstErr != nil || secondErr != nil {
				sublogger.Error().
					Err(err).
					Msg("Error converting delegator shares for sorting")
				return true
			}

			return firstShares > secondShares
		})

		var validatorRank int

		for index, validatorIterated := range validators {
			if validatorIterated.OperatorAddress == validator.Validator.OperatorAddress {
				validatorRank = index + 1
				break
			}
		}

		if validatorRank == 0 {
			sublogger.Warn().
				Str("address", address).
				Msg("Could not find validator in validators list")
			return
		}

		validatorRankGauge.With(prometheus.Labels{
			"moniker": validator.Validator.Description.Moniker,
			"address": address,
		}).Set(float64(validatorRank))

		sublogger.Debug().
			Str("address", address).
			Msg("Started querying validator params")
		queryStart = time.Now()

		paramsRes, err := stakingClient.Params(
			context.Background(),
			&stakingtypes.QueryParamsRequest{},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get params")
			return
		}

		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying validator params")

		// golang doesn't have a ternary operator, so we have to stick with this ugly solution
		var active float64

		if validatorRank <= int(paramsRes.Params.MaxValidators) {
			active = 1
		} else {
			active = 0
		}

		validatorIsActiveGauge.With(prometheus.Labels{
			"address": validator.Validator.OperatorAddress,
			"moniker": validator.Validator.Description.Moniker,
		}).Set(active)
	}()

	wg.Wait()

	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
	sublogger.Info().
		Str("method", "GET").
		Str("endpoint", "/metrics/validator?address="+address).
		Float64("request-time", time.Since(requestStart).Seconds()).
		Msg("Request processed")
}
