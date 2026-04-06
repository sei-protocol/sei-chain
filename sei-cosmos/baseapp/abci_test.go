package baseapp

import (
	"context"
	"encoding/json"
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func TestGetBlockRentionHeight(t *testing.T) {
	db := dbm.NewMemDB()
	name := t.Name()

	testCases := map[string]struct {
		bapp         *BaseApp
		maxAgeBlocks int64
		commitHeight int64
		expected     int64
	}{
		"defaults": {
			bapp:         NewBaseApp(name, db, nil, nil, &testutil.TestAppOpts{}),
			maxAgeBlocks: 0,
			commitHeight: 499000,
			expected:     0,
		},
		"pruning unbonding time only": {
			bapp:         NewBaseApp(name, db, nil, nil, &testutil.TestAppOpts{}, SetMinRetainBlocks(1)),
			maxAgeBlocks: 362880,
			commitHeight: 499000,
			expected:     136120,
		},
		"pruning snapshot only": {
			bapp: NewBaseApp(
				name, db, nil, nil, &testutil.TestAppOpts{},
				SetPruning(sdk.PruningOptions{KeepEvery: 10000}),
				SetMinRetainBlocks(1),
			),
			maxAgeBlocks: 0,
			commitHeight: 499000,
			expected:     490000,
		},
		"pruning state sync snapshot only": {
			bapp: NewBaseApp(
				name, db, nil, nil, &testutil.TestAppOpts{},
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
				name, db, nil, nil, &testutil.TestAppOpts{},
				SetMinRetainBlocks(400000),
			),
			maxAgeBlocks: 0,
			commitHeight: 499000,
			expected:     99000,
		},
		"pruning all conditions": {
			bapp: NewBaseApp(
				name, db, nil, nil, &testutil.TestAppOpts{},
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
				name, db, nil, nil, &testutil.TestAppOpts{},
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
				name, db, nil, nil, &testutil.TestAppOpts{},
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
			height, err := tc.bapp.GetBlockRetentionHeight(tc.commitHeight)
			require.NoError(t, err)
			require.Equal(t, tc.expected, height)
		})
	}
}

// Test and ensure that invalid block heights always cause errors.
// See issues:
// - https://github.com/cosmos/cosmos-sdk/issues/11220
// - https://github.com/cosmos/cosmos-sdk/issues/7662
func TestBaseAppCreateQueryContext(t *testing.T) {
	t.Parallel()

	db := dbm.NewMemDB()
	name := t.Name()
	app := NewBaseApp(name, db, nil, nil, &testutil.TestAppOpts{})

	app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Header: &tmproto.Header{ChainID: app.ChainID, Height: 1}})
	app.SetDeliverStateToCommit()
	app.Commit(context.Background())

	app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Header: &tmproto.Header{ChainID: app.ChainID, Height: 2}})
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
			_, err := app.CreateQueryContext(tc.height, tc.prove)
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
func TestHandleQueryStore_NonQueryableMultistore(t *testing.T) {

	db := dbm.NewMemDB()
	name := t.Name()
	app := NewBaseApp(name, db, nil, nil, &testutil.TestAppOpts{})

	// Mock a non-queryable cms
	mockCMS := &mockNonQueryableMultiStore{}
	app.cms = mockCMS

	path := []string{"store", "test"}
	req := abci.RequestQuery{
		Path:   "store/test",
		Height: 1,
	}

	resp := handleQueryStore(app, path, req)
	require.True(t, resp.IsErr())
	require.Contains(t, resp.Log, "multistore doesn't support queries")
}

// TestProcessProposalAlwaysResetsState verifies that processProposalState
// gets a fresh CacheMultiStore on every ProcessProposal call after block 1.
// Block 1 preserves the InitChain-set processProposalState (genesis state).
func TestProcessProposalAlwaysResetsState(t *testing.T) {
	db := dbm.NewMemDB()
	name := t.Name()
	app := NewBaseApp(name, db, nil, nil, &testutil.TestAppOpts{})
	capKey := sdk.NewKVStoreKey("main")
	app.MountStores(capKey)
	app.SetParamStore(&paramStore{db: dbm.NewMemDB()})

	genesisKey, genesisVal := []byte("genesis_key"), []byte("genesis_val")
	stateKey, stateVal := []byte("round_key"), []byte("round_val")

	type observation struct {
		genesisVisible bool
		stateVisible   bool
	}
	var observations []observation

	app.SetInitChainer(func(ctx sdk.Context, req abci.RequestInitChain) abci.ResponseInitChain {
		store := ctx.KVStore(capKey)
		store.Set(genesisKey, genesisVal)
		return abci.ResponseInitChain{}
	})

	app.SetProcessProposalHandler(func(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		store := ctx.KVStore(capKey)
		observations = append(observations, observation{
			genesisVisible: store.Get(genesisKey) != nil,
			stateVisible:   store.Get(stateKey) != nil,
		})
		store.Set(stateKey, stateVal)
		return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
	})

	err := app.LoadLatestVersion()
	require.NoError(t, err)

	app.InitChain(context.Background(), &abci.RequestInitChain{
		AppStateBytes: []byte("{}"),
		ChainId:       "test-chain",
	})

	// Call 0: Block 1 — should see genesis state
	app.ProcessProposal(context.Background(), &abci.RequestProcessProposal{
		Header: &tmproto.Header{ChainID: "test-chain", Height: 1},
		Hash:   []byte("hash0"),
	})

	// Commit block 1
	app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: "test-chain", Height: 1},
	})
	app.SetDeliverStateToCommit()
	app.Commit(context.Background())

	header := tmproto.Header{ChainID: "test-chain", Height: 2}

	// Call 1: Height 2, hash=A — fresh after commit
	app.ProcessProposal(context.Background(), &abci.RequestProcessProposal{
		Header: &header, Hash: []byte("hashA"),
	})

	// Call 2: Height 2, hash=A again (same hash) — still fresh
	app.ProcessProposal(context.Background(), &abci.RequestProcessProposal{
		Header: &header, Hash: []byte("hashA"),
	})

	// Call 3: Height 2, hash=B (different hash) — fresh
	app.ProcessProposal(context.Background(), &abci.RequestProcessProposal{
		Header: &header, Hash: []byte("hashB"),
	})

	require.Len(t, observations, 4)

	// Call 0: block 1 — genesis visible, no prior state
	require.True(t, observations[0].genesisVisible, "genesis state must be visible for block 1")
	require.False(t, observations[0].stateVisible)

	// Call 1: fresh after commit
	require.False(t, observations[1].stateVisible, "state should not survive commit")

	// Call 2: same hash — still reset, no stale state
	require.False(t, observations[2].stateVisible, "state should not carry over even for same hash")

	// Call 3: different hash — reset
	require.False(t, observations[3].stateVisible, "state should not carry over for different hash")
}

type mockNonQueryableMultiStore struct {
	sdk.CommitMultiStore
}
