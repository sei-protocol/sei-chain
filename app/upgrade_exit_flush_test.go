package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

// newUpgradeExitTestApp returns an initialized app whose SC store lives under home.
func newUpgradeExitTestApp(t *testing.T, home string) *App {
	t.Helper()
	encodingConfig := MakeEncodingConfig()
	options := []AppOption{
		func(app *App) {
			receiptStore, err := setupReceiptStore(app.keys[evmtypes.StoreKey])
			require.NoError(t, err)
			app.receiptStore = receiptStore
		},
	}
	testApp := New(
		dbm.NewMemDB(),
		nil,
		true,
		map[int64]bool{},
		home,
		1,
		false,
		config.TestConfig(),
		encodingConfig,
		wasm.EnableAllProposals,
		TestAppOpts{},
		EmptyWasmOpts,
		options,
	)
	genesisState := NewDefaultGenesisState(encodingConfig.Marshaler)
	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)
	_, err = testApp.InitChain(&abci.RequestInitChain{
		ConsensusParams: DefaultConsensusParams,
		ChainId:         "sei-test",
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)
	return testApp
}

func finalizeEmptyBlock(testApp *App, height int64) (*abci.ResponseFinalizeBlock, error) {
	return testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		DecidedLastCommit:   abci.CommitInfo{Round: 0},
		ByzantineValidators: []abci.Misbehavior{},
		Hash:                []byte("abc"),
		Header: &tmproto.Header{
			ChainID: "sei-test",
			Height:  height,
			Time:    time.Now(),
		},
	})
}

func TestUpgradeNeededPanicFlushesMemIAVLChangelog(t *testing.T) {
	home := t.TempDir()
	testApp := newUpgradeExitTestApp(t, home)
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	const (
		planName           = "flush-before-exit"
		planHeight   int64 = 3
		commitBlocks int64 = planHeight - 1
	)
	require.False(t, testApp.UpgradeKeeper.HasHandler(planName))

	_, err := finalizeEmptyBlock(testApp, 1)
	require.NoError(t, err)
	require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(testApp.GetContextForDeliverTx(nil), upgradetypes.Plan{
		Name:   planName,
		Height: planHeight,
	}))
	_, err = testApp.Commit(context.Background())
	require.NoError(t, err)
	for height := int64(2); height <= commitBlocks; height++ {
		_, err = finalizeEmptyBlock(testApp, height)
		require.NoError(t, err)
		_, err = testApp.Commit(context.Background())
		require.NoError(t, err)
	}
	require.Equal(t, commitBlocks, testApp.LastBlockHeight())

	var panicked any
	func() {
		defer func() { panicked = recover() }()
		_, _ = finalizeEmptyBlock(testApp, planHeight)
	}()
	require.Contains(t, fmt.Sprint(panicked), fmt.Sprintf("UPGRADE %q NEEDED at height: %d", planName, planHeight))

	persisted, err := memiavl.GetLatestVersion(utils.GetCosmosSCStorePath(home))
	require.NoError(t, err)
	require.Equal(t, commitBlocks, persisted)
}
