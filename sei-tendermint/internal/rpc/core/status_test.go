package core

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func newStatusEnv(latestHeight int64, freezeHeight uint64) *Environment {
	blockStore := &mocks.BlockStore{}
	blockStore.On("LoadBaseMeta").Return(nil)
	blockStore.On("Height").Return(latestHeight)
	blockStore.On("LoadBlockMeta", mock.Anything).Return(nil)
	return &Environment{
		App:          utils.Some(proxy.New(abci.BaseApplication{})),
		BlockStore:   blockStore,
		ReadOnly:     freezeHeight > 0,
		FreezeHeight: freezeHeight,
	}
}

func TestStatusReportsFreezeState(t *testing.T) {
	cases := []struct {
		name         string
		latestHeight int64
		freezeHeight uint64
		wantFrozen   bool
	}{
		{name: "below freeze height", latestHeight: 98, freezeHeight: 100, wantFrozen: false},
		{name: "stopped before freeze height", latestHeight: 99, freezeHeight: 100, wantFrozen: true},
		{name: "empty block store", latestHeight: 0, freezeHeight: 100, wantFrozen: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, err := newStatusEnv(tc.latestHeight, tc.freezeHeight).Status(t.Context())
			require.NoError(t, err)
			require.Equal(t, int64(tc.freezeHeight), status.SyncInfo.FreezeHeight) //nolint:gosec // test heights are small.
			require.Equal(t, tc.wantFrozen, status.SyncInfo.Frozen)
		})
	}
}

func TestStatusOmitsFreezeStateWhenFreezeModeIsOff(t *testing.T) {
	status, err := newStatusEnv(99, 0).Status(t.Context())
	require.NoError(t, err)
	require.Zero(t, status.SyncInfo.FreezeHeight)
	require.False(t, status.SyncInfo.Frozen)

	encoded, err := json.Marshal(status.SyncInfo)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.NotContains(t, fields, "freeze_height")
	require.NotContains(t, fields, "frozen")
}

func TestStatusEncodesFreezeState(t *testing.T) {
	status, err := newStatusEnv(99, 100).Status(t.Context())
	require.NoError(t, err)

	encoded, err := json.Marshal(status.SyncInfo)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.JSONEq(t, `"100"`, string(fields["freeze_height"]))
	require.JSONEq(t, `true`, string(fields["frozen"]))
}
