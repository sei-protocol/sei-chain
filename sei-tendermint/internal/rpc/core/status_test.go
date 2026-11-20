package core

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type statusEnvCase struct {
	initialHeight    int64
	blockStoreHeight int64
	executedHeight   int64
	freezeHeight     uint64
}

func newStatusEnv(t *testing.T, c statusEnvCase) *Environment {
	t.Helper()
	blockStore := &mocks.BlockStore{}
	blockStore.On("LoadBaseMeta").Return(nil)
	blockStore.On("Height").Return(c.blockStoreHeight)
	blockStore.On("LoadBlockMeta", mock.Anything).Return(nil)

	vals, _ := types.RandValidatorSet(1, 10)
	stateStore := sm.NewStore(dbm.NewMemDB())
	require.NoError(t, stateStore.Save(sm.State{
		InitialHeight:   c.initialHeight,
		LastBlockHeight: c.executedHeight,
		Validators:      vals,
		NextValidators:  vals,
		LastValidators:  vals,
		ConsensusParams: *types.DefaultConsensusParams(),
	}))

	return &Environment{
		App:          proxy.New(abci.BaseApplication{}),
		BlockStore:   blockStore,
		StateStore:   stateStore,
		ReadOnly:     c.freezeHeight > 0,
		FreezeHeight: c.freezeHeight,
	}
}

func TestStatusReportsFreezeState(t *testing.T) {
	cases := []struct {
		name       string
		env        statusEnvCase
		wantFrozen bool
	}{
		{
			name:       "below freeze height",
			env:        statusEnvCase{initialHeight: 1, blockStoreHeight: 98, executedHeight: 98, freezeHeight: 100},
			wantFrozen: false,
		},
		{
			name:       "stopped before freeze height",
			env:        statusEnvCase{initialHeight: 1, blockStoreHeight: 99, executedHeight: 99, freezeHeight: 100},
			wantFrozen: true,
		},
		{
			name:       "last block saved but not yet executed",
			env:        statusEnvCase{initialHeight: 1, blockStoreHeight: 99, executedHeight: 98, freezeHeight: 100},
			wantFrozen: false,
		},
		{
			name:       "nothing executed below freeze height",
			env:        statusEnvCase{initialHeight: 1, blockStoreHeight: 0, executedHeight: 0, freezeHeight: 100},
			wantFrozen: false,
		},
		{
			name:       "freeze height equals initial height",
			env:        statusEnvCase{initialHeight: 50, blockStoreHeight: 0, executedHeight: 0, freezeHeight: 50},
			wantFrozen: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, err := newStatusEnv(t, tc.env).Status(t.Context())
			require.NoError(t, err)
			require.Equal(t, int64(tc.env.freezeHeight), status.SyncInfo.FreezeHeight) //nolint:gosec // test heights are small.
			require.Equal(t, tc.wantFrozen, status.SyncInfo.Frozen)
		})
	}
}

func TestStatusOmitsFreezeStateWhenFreezeModeIsOff(t *testing.T) {
	env := newStatusEnv(t, statusEnvCase{initialHeight: 1, blockStoreHeight: 99, executedHeight: 99})
	status, err := env.Status(t.Context())
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
	env := newStatusEnv(t, statusEnvCase{initialHeight: 1, blockStoreHeight: 99, executedHeight: 99, freezeHeight: 100})
	status, err := env.Status(t.Context())
	require.NoError(t, err)

	encoded, err := json.Marshal(status.SyncInfo)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.JSONEq(t, `"100"`, string(fields["freeze_height"]))
	require.JSONEq(t, `true`, string(fields["frozen"]))
}
