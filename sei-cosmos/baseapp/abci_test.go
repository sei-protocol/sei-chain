package baseapp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/utils"
)

func TestGetBlockRentionHeight(t *testing.T) {
	logger := defaultLogger()
	db := dbm.NewMemDB()
	name := t.Name()

	testCases := map[string]struct {
		bapp         *BaseApp
		maxAgeBlocks int64
		commitHeight int64
		expected     int64
	}{
		"defaults": {
			bapp:         NewBaseApp(name, logger, db, nil, nil, &testutil.TestAppOpts{}),
			maxAgeBlocks: 0,
			commitHeight: 499000,
			expected:     0,
		},
		"pruning unbonding time only": {
			bapp:         NewBaseApp(name, logger, db, nil, nil, &testutil.TestAppOpts{}, SetMinRetainBlocks(1)),
			maxAgeBlocks: 362880,
			commitHeight: 499000,
			expected:     136120,
		},
		"pruning iavl snapshot only": {
			bapp: NewBaseApp(
				name, logger, db, nil, nil, &testutil.TestAppOpts{},
				SetPruning(sdk.PruningOptions{KeepEvery: 10000}),
				SetMinRetainBlocks(1),
			),
			maxAgeBlocks: 0,
			commitHeight: 499000,
			expected:     490000,
		},
		"pruning state sync snapshot only": {
			bapp: NewBaseApp(
				name, logger, db, nil, nil, &testutil.TestAppOpts{},
				SetSnapshotInterval(50000),
				SetSnapshotKeepRecent(3),
				SetMinRetainBlocks(1),
			),
			maxAgeBlocks: 0,
			commitHeight: 499000,
			expected:     349000,
		},
		"pruning min retention only": {
			bapp: NewBaseApp(
				name, logger, db, nil, nil, &testutil.TestAppOpts{},
				SetMinRetainBlocks(400000),
			),
			maxAgeBlocks: 0,
			commitHeight: 499000,
			expected:     99000,
		},
		"pruning all conditions": {
			bapp: NewBaseApp(
				name, logger, db, nil, nil, &testutil.TestAppOpts{},
				SetPruning(sdk.PruningOptions{KeepEvery: 10000}),
				SetMinRetainBlocks(400000),
				SetSnapshotInterval(50000), SetSnapshotKeepRecent(3),
			),
			maxAgeBlocks: 362880,
			commitHeight: 499000,
			expected:     99000,
		},
		"no pruning due to no persisted state": {
			bapp: NewBaseApp(
				name, logger, db, nil, nil, &testutil.TestAppOpts{},
				SetPruning(sdk.PruningOptions{KeepEvery: 10000}),
				SetMinRetainBlocks(400000),
				SetSnapshotInterval(50000), SetSnapshotKeepRecent(3),
			),
			maxAgeBlocks: 362880,
			commitHeight: 10000,
			expected:     0,
		},
		"disable pruning": {
			bapp: NewBaseApp(
				name, logger, db, nil, nil, &testutil.TestAppOpts{},
				SetPruning(sdk.PruningOptions{KeepEvery: 10000}),
				SetMinRetainBlocks(0),
				SetSnapshotInterval(50000), SetSnapshotKeepRecent(3),
			),
			maxAgeBlocks: 362880,
			commitHeight: 499000,
			expected:     0,
		},
	}

	for name, tc := range testCases {
		tc := tc

		tc.bapp.SetParamStore(&paramStore{db: dbm.NewMemDB()})
		tc.bapp.InitChain(context.Background(), &abci.RequestInitChain{
			ConsensusParams: &tmproto.ConsensusParams{
				Evidence: &tmproto.EvidenceParams{
					MaxAgeNumBlocks: tc.maxAgeBlocks,
				},
			},
		})

		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.bapp.GetBlockRetentionHeight(tc.commitHeight))
		})
	}
}

// Test and ensure that invalid block heights always cause errors.
// See issues:
// - https://github.com/cosmos/cosmos-sdk/issues/11220
// - https://github.com/cosmos/cosmos-sdk/issues/7662
func TestBaseAppCreateQueryContext(t *testing.T) {
	t.Parallel()

	logger := defaultLogger()
	db := dbm.NewMemDB()
	name := t.Name()
	app := NewBaseApp(name, logger, db, nil, nil, &testutil.TestAppOpts{})

	app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 1})
	app.SetDeliverStateToCommit()
	app.Commit(context.Background())

	app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 2})
	app.SetDeliverStateToCommit()
	app.Commit(context.Background())

	testCases := []struct {
		name   string
		height int64
		prove  bool
		expErr bool
	}{
		{"valid height", 2, true, false},
		{"future height", 10, true, true},
		{"negative height, prove=true", -1, true, true},
		{"negative height, prove=false", -1, false, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := app.createQueryContext(tc.height, tc.prove)
			if tc.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

type paramStore struct {
	db *dbm.MemDB
}

func (ps *paramStore) Set(_ sdk.Context, key []byte, value interface{}) {
	bz, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	ps.db.Set(key, bz)
}

func (ps *paramStore) Has(_ sdk.Context, key []byte) bool {
	ok, err := ps.db.Has(key)
	if err != nil {
		panic(err)
	}

	return ok
}

func (ps *paramStore) Get(_ sdk.Context, key []byte, ptr interface{}) {
	bz, err := ps.db.Get(key)
	if err != nil {
		panic(err)
	}

	if len(bz) == 0 {
		return
	}

	if err := json.Unmarshal(bz, ptr); err != nil {
		panic(err)
	}
}

// TestABCI_Proposal_Reset_State ensures that state is reset between runs of
// PrepareProposal and ProcessProposal in case they are called multiple times.
// This is only valid for heights > 1, given that on height 1 we always set the
// state to be deliverState.
func TestABCI_Proposal_Reset_State_Between_Calls(t *testing.T) {
	someKey := []byte("some-key")

	prepareOpt := func(bapp *BaseApp) {
		bapp.SetPrepareProposalHandler(func(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
			// This key should not exist given that we reset the state on every call.
			require.False(t, ctx.KVStore(capKey1).Has(someKey))
			ctx.KVStore(capKey1).Set(someKey, someKey)
			return &abci.ResponsePrepareProposal{
				TxRecords: utils.Map(req.Txs, func(tx []byte) *abci.TxRecord {
					return &abci.TxRecord{Action: abci.TxRecord_UNMODIFIED, Tx: tx}
				}),
			}, nil
		})
	}

	processOpt := func(bapp *BaseApp) {
		bapp.SetProcessProposalHandler(func(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
			// This key should not exist given that we reset the state on every call.
			require.False(t, ctx.KVStore(capKey1).Has(someKey))
			ctx.KVStore(capKey1).Set(someKey, someKey)
			return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
		})
	}

	app := setupBaseApp(
		t,
		processOpt,
		prepareOpt,
	)

	_, err := app.InitChain(context.Background(), &abci.RequestInitChain{
		Validators: []abci.ValidatorUpdate{},
		// ConsensusParams: simapp.DefaultConsensusParams,
		// AppStateBytes:   stateBytes,
	})
	ctx := app.NewContext(false, tmproto.Header{})

	require.Nil(t, err)

	// Genesis at height 1
	reqPrepareProposal := abci.RequestPrepareProposal{
		MaxTxBytes: 1000,
		Height:     1, // this value can't be 0
	}

	resPrepareProposal, _ := app.PrepareProposal(ctx.Context(), &reqPrepareProposal)
	require.Equal(t, 0, len(resPrepareProposal.TxRecords))

	reqProposalTxBytes := [][]byte{}
	reqProcessProposal := abci.RequestProcessProposal{
		Txs:    reqProposalTxBytes,
		Height: 1,
	}

	resProcessProposal, _ := app.ProcessProposal(ctx.Context(), &reqProcessProposal)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resProcessProposal.Status)

	app.BeginBlock(ctx, abci.RequestBeginBlock{
		Header: tmproto.Header{Height: app.LastBlockHeight() + 1},
	})

	// Post Genesis

	reqPrepareProposal = abci.RequestPrepareProposal{
		MaxTxBytes: 1000,
		Height:     2, // this value can't be 0
	}

	// Let's pretend something happened and PrepareProposal gets called many
	// times, this must be safe to do.
	for i := 0; i < 5; i++ {
		resPrepareProposal, _ := app.PrepareProposal(ctx.Context(), &reqPrepareProposal)
		require.Equal(t, 0, len(resPrepareProposal.TxRecords))
	}

	reqProposalTxBytes = [][]byte{}
	reqProcessProposal = abci.RequestProcessProposal{
		Txs:    reqProposalTxBytes,
		Height: 2,
	}

	// Let's pretend something happened and ProcessProposal gets called many
	// times, this must be safe to do.
	for i := 0; i < 5; i++ {
		resProcessProposal, _ := app.ProcessProposal(ctx.Context(), &reqProcessProposal)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resProcessProposal.Status)
	}

	app.BeginBlock(ctx, abci.RequestBeginBlock{
		Header: tmproto.Header{Height: app.LastBlockHeight() + 1},
	})
}
