package app_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
)

const cosmosMetricsProbe = "sei_chain_cosmos_params_max_validators"

type cosmosMetricsAppOpts struct{ app.TestAppOpts }

func (o cosmosMetricsAppOpts) Get(s string) interface{} {
	switch s {
	case "cosmos_metrics.enabled":
		return true
	case "cosmos_metrics.refresh_interval":
		return "10ms"
	}
	return o.TestAppOpts.Get(s)
}

func TestCosmosMetricsStartAfterLoadAndStopOnClose(t *testing.T) {
	encodingConfig := app.MakeEncodingConfig()
	testApp := app.New(dbm.NewMemDB(), nil, true, map[int64]bool{}, t.TempDir(), 1, false,
		config.TestConfig(), encodingConfig, wasm.EnableAllProposals, cosmosMetricsAppOpts{}, app.EmptyWasmOpts, nil)
	stateBytes, err := json.Marshal(app.NewDefaultGenesisState(encodingConfig.Marshaler))
	require.NoError(t, err)
	_, err = testApp.InitChain(&types.RequestInitChain{
		ConsensusParams: app.DefaultConsensusParams,
		ChainId:         "sei-test",
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)
	_, err = testApp.FinalizeBlock(context.Background(), &types.RequestFinalizeBlock{Header: &tmproto.Header{ChainID: "sei-test", Height: 1}})
	require.NoError(t, err)
	testApp.Commit(context.Background())

	requireMetricGathered(t, cosmosMetricsProbe)

	require.NoError(t, testApp.Close())
	require.NotContains(t, gatheredMetricNames(t), cosmosMetricsProbe, "cosmos metrics were not stopped")
}

// requireMetricGathered fails unless name is gathered within 30s. Each gather collects from every
// exporter this process has created, which takes seconds under the race detector, so the deadline
// is only checked between gathers rather than cutting one short.
func requireMetricGathered(t *testing.T, name string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if slices.Contains(gatheredMetricNames(t), name) {
			return
		}
		require.False(t, time.Now().After(deadline), "cosmos metrics were not started")
		time.Sleep(10 * time.Millisecond)
	}
}

func gatheredMetricNames(t *testing.T) []string {
	t.Helper()
	// Every app constructed in this process registers its own exporter on the default registry,
	// so Gather reports duplicate target_info families alongside the metrics themselves.
	families, _ := prometheus.DefaultGatherer.Gather()
	names := make([]string, 0, len(families))
	for _, f := range families {
		names = append(names, f.GetName())
	}
	return names
}
