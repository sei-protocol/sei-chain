// original here - https://gist.github.com/jumanzii/031cfea1b2aa3c2a43b63aa62a919285
package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"google.golang.org/grpc"
)

type votePenaltyCounter struct {
	MissCount    string `json:"miss_count"`
	AbstainCount string `json:"abstain_count"`
	SuccessCount string `json:"success_count"`
}

func OracleMetricHandler(w http.ResponseWriter, r *http.Request, grpcConn *grpc.ClientConn) {
	requestStart := time.Now()

	sublogger := log.With().
		Str("request-id", uuid.New().String()).
		Logger()

	address := r.URL.Query().Get("address")

	votePenaltyCount := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "cosmos_oracle_vote_penalty_count",
			Help:        "Vote penalty miss count",
			ConstLabels: ConstLabels,
		},
		[]string{"type"},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(votePenaltyCount)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		sublogger.Debug().Msg("Started querying oracle feeder metrics")
		queryStart := time.Now()

		oracleClient := oracletypes.NewQueryClient(grpcConn)
		response, err := oracleClient.VotePenaltyCounter(context.Background(), &oracletypes.QueryVotePenaltyCounterRequest{ValidatorAddr: address})

		if err != nil {
			sublogger.Error().
				Err(err).
				Msg("Could not get oracle feeder metrics")
			return
		}

		sublogger.Debug().
			Float64("request-time", time.Since(queryStart).Seconds()).
			Msg("Finished querying oracle feeder metrics")

		missCount := float64(response.VotePenaltyCounter.MissCount)
		abstainCount := float64(response.VotePenaltyCounter.AbstainCount)
		successCount := float64(response.VotePenaltyCounter.SuccessCount)

		votePenaltyCount.WithLabelValues("miss").Add(missCount)
		votePenaltyCount.WithLabelValues("abstain").Add(abstainCount)
		votePenaltyCount.WithLabelValues("success").Add(successCount)

	}()
	wg.Wait()

	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
	sublogger.Info().
		Str("method", "GET").
		Str("endpoint", "/metrics/oracle").
		Float64("request-time", time.Since(requestStart).Seconds()).
		Msg("Request processed")
}
