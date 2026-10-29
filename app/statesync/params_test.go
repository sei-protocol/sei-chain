package statesync_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app/statesync"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmjson "github.com/sei-protocol/sei-chain/sei-tendermint/libs/json"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
)

// TestGovernanceSetsTheTrustPeriod passes param change proposals for the trust period
// and requires a positive value to be stored and a zero or negative one refused.
func TestGovernanceSetsTheTrustPeriod(t *testing.T) {
	a := processblock.NewTestApp(t)
	handler := params.NewParamChangeProposalHandler(a.ParamsKeeper)
	propose := func(value string) error {
		change := proposal.NewParamChange(statesync.SubspaceName, string(statesync.KeyTrustPeriod), value)
		return handler(a.Ctx(), proposal.NewParameterChangeProposal("title", "description",
			[]proposal.ParamChange{change}, false))
	}
	subspace, ok := a.ParamsKeeper.GetSubspace(statesync.SubspaceName)
	require.True(t, ok)

	require.NoError(t, propose(`"403200000000000"`))
	var trustPeriod time.Duration
	subspace.Get(a.Ctx(), statesync.KeyTrustPeriod, &trustPeriod)
	require.Equal(t, 112*time.Hour, trustPeriod)

	require.Error(t, propose(`"0"`))
	require.Error(t, propose(`"-1"`))
	subspace.Get(a.Ctx(), statesync.KeyTrustPeriod, &trustPeriod)
	require.Equal(t, 112*time.Hour, trustPeriod, "a refused proposal changed the trust period")
}

// TestTheTrustPeriodIsServedWhereStateSyncReadsIt stores a trust period through the
// subspace and requires the ABCI query a state syncing node sends its RPC servers to
// return it, in the encoding that node decodes.
func TestTheTrustPeriodIsServedWhereStateSyncReadsIt(t *testing.T) {
	a := processblock.NewTestApp(t)
	processblock.CommonPreset(a)
	subspace, ok := a.ParamsKeeper.GetSubspace(statesync.SubspaceName)
	require.True(t, ok)
	subspace.Set(a.Ctx(), statesync.KeyTrustPeriod, 48*time.Hour)
	a.RunBlock(nil)

	res, err := a.Query(context.Background(), &abci.RequestQuery{
		Path: tmcfg.StateSyncTrustPeriodQueryPath,
		Data: []byte(tmcfg.StateSyncTrustPeriodQueryKey),
	})
	require.NoError(t, err)
	require.True(t, res.IsOK(), res.Log)
	var trustPeriod time.Duration
	require.NoError(t, tmjson.Unmarshal(res.Value, &trustPeriod), "trust period %q", res.Value)
	require.Equal(t, 48*time.Hour, trustPeriod)
}
