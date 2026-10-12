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

	// Eventually fails on its timer even while the condition is still running, and one Gather
	// collects from every exporter this process has created, which takes seconds under the race
	// detector. The wait is sized well above a single gather so it fails only on a probe that
	// never appears.
	require.Eventually(t, func() bool {
		return slices.Contains(gatheredMetricNames(t), cosmosMetricsProbe)
	}, time.Minute, 10*time.Millisecond, "cosmos metrics were not started")

	require.NoError(t, testApp.Close())
	require.NotContains(t, gatheredMetricNames(t), cosmosMetricsProbe, "cosmos metrics were not stopped")
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
