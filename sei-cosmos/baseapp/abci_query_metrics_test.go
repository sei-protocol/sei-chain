package baseapp

import (
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func TestAbciQueryMetricRoute(t *testing.T) {
	db := dbm.NewMemDB()
	app := NewBaseApp(t.Name(), db, nil, nil, &testutil.TestAppOpts{})

	grpcQR := app.GRPCQueryRouter()
	grpcQR.SetInterfaceRegistry(testdata.NewTestInterfaceRegistry())
	testdata.RegisterQueryServer(grpcQR, testdata.QueryImpl{})

	app.QueryRouter().AddRoute("bank", func(_ sdk.Context, _ []string, _ abci.RequestQuery) ([]byte, error) {
		return nil, nil
	})

	grpcEcho := "/testdata.Query/Echo"

	tests := map[string]struct {
		reqPath  string
		expected string
	}{
		// Production gRPC paths (3-month sample); registered via testdata.Query.
		"grpc registered": {
			reqPath:  grpcEcho,
			expected: grpcEcho,
		},
		"grpc unregistered": {
			reqPath:  "/cosmos.bank.v1beta1.Query/TotalSupply",
			expected: "other",
		},

		// Legacy app paths.
		"app snapshots": {reqPath: "app/snapshots", expected: "app/snapshots"},
		"app version":   {reqPath: "app/version", expected: "app/version"},
		"app unknown":   {reqPath: "app/unknown-action", expected: "app/unknown"},

		// Legacy store paths (unknown store name or subpath → single bucket).
		"store ibc key unregistered": {reqPath: "store/ibc/key", expected: "store/unknown"},
		"store random attack":        {reqPath: "store/random1/random2", expected: "store/unknown"},
		"store bad subpath":          {reqPath: "store/key1/evil", expected: "store/unknown"},
		"store short":                {reqPath: "store/bank", expected: "store/unknown"},

		// Legacy custom paths; subpath segments are not part of the metric label.
		"custom bank":        {reqPath: "custom/bank/all_balances", expected: "custom/bank"},
		"custom unregistered": {reqPath: "custom/unknown/foo", expected: "custom/unknown"},

		// Garbage / attack paths.
		"empty":        {reqPath: "", expected: "other"},
		"random":       {reqPath: "/totally/made/up", expected: "other"},
		"leading slash app": {reqPath: "/app/snapshots", expected: "app/snapshots"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.expected, app.abciQueryMetricRoute(tc.reqPath))
		})
	}
}

func TestAbciQueryMetricRoute_RegisteredStore(t *testing.T) {
	app := setupBaseApp(t)
	require.Equal(t, "store/key1/key", app.abciQueryMetricRoute("store/key1/key"))
	require.Equal(t, "store/unknown", app.abciQueryMetricRoute("store/random1/key"))
}
