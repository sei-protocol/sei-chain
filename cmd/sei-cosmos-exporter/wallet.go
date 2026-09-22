package main

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	distributiontypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	"google.golang.org/grpc"
)

func WalletHandler(w http.ResponseWriter, r *http.Request, grpcConn *grpc.ClientConn) {
	requestStart := time.Now()

	sublogger := log.With().
		Str("request-id", uuid.New().String()).
		Logger()

	address := r.URL.Query().Get("address")
	myAddress, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		sublogger.Error().
			Str("address", address).
			Err(err).
			Msg("Could not get address")
		return
	}

	walletBalanceGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_wallet_balance",
			Help:        "Balance of the Cosmos-based blockchain wallet",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "denom"},
	)

	walletDelegationGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_wallet_delegations",
			Help:        "Delegations of the Cosmos-based blockchain wallet",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "denom", "delegated_to"},
	)

	walletRedelegationGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_wallet_redelegations",
			Help:        "Redlegations of the Cosmos-based blockchain wallet",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "denom", "redelegated_from", "redelegated_to"},
	)

	walletUnbondingsGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_wallet_unbondings",
			Help:        "Unbondings of the Cosmos-based blockchain wallet",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "denom", "unbonded_from"},
	)

	walletRewardsGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "cosmos_wallet_rewards",
			Help:        "Rewards of the Cosmos-based blockchain wallet",
			ConstLabels: ConstLabels,
		},
		[]string{"address", "denom", "validator_address"},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(walletBalanceGauge)
	registry.MustRegister(walletDelegationGauge)
	registry.MustRegister(walletUnbondingsGauge)
	registry.MustRegister(walletRedelegationGauge)
	registry.MustRegister(walletRewardsGauge)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		sublogger.Debug().
			Str("address", address).
			Msg("Started querying balance")
		queryStart := time.Now()

		bankClient := banktypes.NewQueryClient(grpcConn)
		bankRes, err := bankClient.AllBalances(
			context.Background(),
			&banktypes.QueryAllBalancesRequest{Address: myAddress.String()},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get balance")
			return
		}

		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying balance")

		for _, balance := range bankRes.Balances {
			// because cosmos's dec doesn't have .toFloat64() method or whatever and returns everything as int
			if value, err := strconv.ParseFloat(balance.Amount.String(), 64); err != nil {
				sublogger.Error().
					Str("address", address).
					Err(err).
					Msg("Could not parse balance")
			} else {
				walletBalanceGauge.With(prometheus.Labels{
					"address": address,
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
			Msg("Started querying delegations")
		queryStart := time.Now()

		stakingClient := stakingtypes.NewQueryClient(grpcConn)
		stakingRes, err := stakingClient.DelegatorDelegations(
			context.Background(),
			&stakingtypes.QueryDelegatorDelegationsRequest{DelegatorAddr: myAddress.String()},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get delegations")
			return
		}

		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying delegations")

		for _, delegation := range stakingRes.DelegationResponses {
			// because cosmos's dec doesn't have .toFloat64() method or whatever and returns everything as int
			if value, err := strconv.ParseFloat(delegation.Balance.Amount.String(), 64); err != nil {
				sublogger.Error().
					Str("address", address).
					Err(err).
					Msg("Could not get delegation")
			} else {
				walletDelegationGauge.With(prometheus.Labels{
					"address":      address,
					"denom":        Denom,
					"delegated_to": delegation.Delegation.ValidatorAddress,
				}).Set(value / DenomCoefficient)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		sublogger.Debug().
			Str("address", address).
			Msg("Started querying unbonding delegations")
		queryStart := time.Now()

		stakingClient := stakingtypes.NewQueryClient(grpcConn)
		stakingRes, err := stakingClient.DelegatorUnbondingDelegations(
			context.Background(),
			&stakingtypes.QueryDelegatorUnbondingDelegationsRequest{DelegatorAddr: myAddress.String()},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get unbonding delegations")
			return
		}

		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying unbonding delegations")

		for _, unbonding := range stakingRes.UnbondingResponses {
			var sum float64 = 0
			for _, entry := range unbonding.Entries {
				// because cosmos's dec doesn't have .toFloat64() method or whatever and returns everything as int
				if value, err := strconv.ParseFloat(entry.Balance.String(), 64); err != nil {
					sublogger.Error().
						Str("address", address).
						Err(err).
						Msg("Could not parse unbonding delegation")
				} else {
					sum += value
				}
			}

			walletUnbondingsGauge.With(prometheus.Labels{
				"address":       unbonding.DelegatorAddress,
				"denom":         Denom, // unbonding does not have denom in response for some reason
				"unbonded_from": unbonding.ValidatorAddress,
			}).Set(sum / DenomCoefficient)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		sublogger.Debug().
			Str("address", address).
			Msg("Started querying redelegations")
		queryStart := time.Now()

		stakingClient := stakingtypes.NewQueryClient(grpcConn)
		stakingRes, err := stakingClient.Redelegations(
			context.Background(),
			&stakingtypes.QueryRedelegationsRequest{DelegatorAddr: myAddress.String()},
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
			Msg("Finished querying redelegations")

		for _, redelegation := range stakingRes.RedelegationResponses {
			var sum float64 = 0
			for _, entry := range redelegation.Entries {
				// because cosmos's dec doesn't have .toFloat64() method or whatever and returns everything as int
				if value, err := strconv.ParseFloat(entry.Balance.String(), 64); err != nil {
					sublogger.Error().
						Str("address", address).
						Err(err).
						Msg("Could not parse redelegation")
				} else {
					sum += value
				}
			}

			walletRedelegationGauge.With(prometheus.Labels{
				"address":          redelegation.Redelegation.DelegatorAddress,
				"denom":            Denom, // redelegation does not have denom in response for some reason
				"redelegated_from": redelegation.Redelegation.ValidatorSrcAddress,
				"redelegated_to":   redelegation.Redelegation.ValidatorDstAddress,
			}).Set(sum / DenomCoefficient)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		sublogger.Debug().
			Str("address", address).
			Msg("Started querying rewards")
		queryStart := time.Now()

		distributionClient := distributiontypes.NewQueryClient(grpcConn)
		distributionRes, err := distributionClient.DelegationTotalRewards(
			context.Background(),
			&distributiontypes.QueryDelegationTotalRewardsRequest{DelegatorAddress: myAddress.String()},
		)
		if err != nil {
			sublogger.Error().
				Str("address", address).
				Err(err).
				Msg("Could not get rewards")
			return
		}
		sublogger.Debug().
			Str("address", address).
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying rewards")

		for _, reward := range distributionRes.Rewards {
			for _, entry := range reward.Reward {
				// because cosmos's dec doesn't have .toFloat64() method or whatever and returns everything as int
				if value, err := strconv.ParseFloat(entry.Amount.String(), 64); err != nil {
					sublogger.Error().
						Str("address", address).
						Err(err).
						Msg("Could not parse reward")
				} else {
					walletRewardsGauge.With(prometheus.Labels{
						"address":           address,
						"denom":             Denom,
						"validator_address": reward.ValidatorAddress,
					}).Set(value / DenomCoefficient)
				}
			}
		}
	}()

	wg.Wait()

	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
	sublogger.Info().
		Str("method", "GET").
		Str("endpoint", "/metrics/wallet?address="+address).
		Float64("request-time", time.Since(requestStart).Seconds()).
		Msg("Request processed")
}
