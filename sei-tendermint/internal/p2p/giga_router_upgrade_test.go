package p2p

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// upgradeStopApp is a testApp that refuses to finalize the block at height
// with an UpgradeNeededError, as an EVM-only app does at a plan it does not
// apply.
type upgradeStopApp struct {
	*testApp
	height int64
	plan   gov.Plan
}

func (a *upgradeStopApp) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	if req.Header.Height == a.height {
		return nil, &gov.UpgradeNeededError{Plan: a.plan}
	}
	return a.testApp.FinalizeBlock(ctx, req)
}

// TestGigaRouter_RunReturnsUpgradeNeeded runs a single validator whose app
// stops at an upgrade height, and requires Run to return the app's
// UpgradeNeededError where errors.As finds it, having executed every block
// below that height.
func TestGigaRouter_RunReturnsUpgradeNeeded(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	_, keys := atypes.GenCommittee(rng, 1)
	cfg := &testNodeCfg{validatorKey: keys[0], nodeKey: makeKey(rng), addr: tcp.TestReserveAddr()}
	genDoc := &types.GenesisDoc{
		ChainID:       "giga-router-upgrade-test",
		InitialHeight: rng.Int63n(100000) + 1,
		AppState:      testAppStateJSON(rng),
	}
	require.NoError(t, genDoc.ValidateAndComplete())
	app := &upgradeStopApp{testApp: newTestApp(), height: genDoc.InitialHeight + 3}
	app.plan = gov.Plan{Name: "0123456789abcdef0123456789abcdef01234567", Height: uint64(app.height), Info: "info", Proposal: 1} //nolint:gosec // height is positive

	dir := t.TempDir()
	littCfg, err := littblock.DefaultConfig(filepath.Join(dir, "blockdb"))
	require.NoError(t, err)
	db, err := littblock.NewBlockDB(littCfg)
	require.NoError(t, err)
	blockStore, err := blockstore.New(db)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blockStore.Close() })
	commonCfg := GigaRouterCommonConfig{
		DialInterval:       100 * time.Millisecond,
		ValidatorAddrs:     map[atypes.PublicKey]GigaNodeAddr{cfg.validatorKey.Public(): cfg.GigaNodeAddr()},
		PersistentStateDir: dir,
		App:                proxy.New(app),
		GenDoc:             genDoc,
	}
	dataState, err := BuildDataState(&commonCfg, blockStore)
	require.NoError(t, err)
	giga, err := NewGigaValidatorRouter(&GigaValidatorConfig{
		GigaRouterCommonConfig: commonCfg,
		ValidatorKey:           cfg.validatorKey,
		ViewTimeout:            func(atypes.View) time.Duration { return time.Hour },
		Producer: &producer.Config{
			MaxGasWantedPerBlock:    1_000_000,
			MaxGasEstimatedPerBlock: 1_000_000,
			MaxTxsPerBlock:          10,
			MaxTxsPerSecond:         utils.None[uint64](),
			BlockInterval:           50 * time.Millisecond,
			AllowEmptyBlocks:        true,
			MaxPendingInserts:       producer.DefaultMaxPendingInserts,
		},
	}, cfg.nodeKey, dataState)
	require.NoError(t, err)
	nodeInfo := makeInfo(cfg.nodeKey)
	nodeInfo.ListenAddr = cfg.addr.String()
	nodeInfo.Network = genDoc.ChainID
	e := Endpoint{AddrPort: cfg.addr}
	router, err := NewRouter(
		cfg.nodeKey,
		func() *types.NodeInfo { return &nodeInfo },
		dbm.NewMemDB(),
		&RouterOptions{
			SelfAddress:              utils.Some(e.NodeAddress(cfg.nodeKey.Public().NodeID())),
			Endpoint:                 e,
			Connection:               conn.DefaultMConnConfig(),
			IncomingConnectionWindow: utils.Some(time.Duration(0)),
			MaxAcceptRate:            rate.Inf,
			MaxDialRate:              rate.Limit(30),
			Giga:                     utils.Some[GigaRouter](giga),
		},
	)
	require.NoError(t, err)

	var runErr error
	err = scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("router", func() error { return utils.IgnoreCancel(router.Run(ctx)) })
		runErr = giga.Run(ctx)
		return nil
	})
	require.NoError(t, err)
	needed, ok := errors.AsType[*gov.UpgradeNeededError](runErr)
	require.True(t, ok, "giga.Run() = %v, want an UpgradeNeededError", runErr)
	require.Equal(t, app.plan, needed.Plan)
	require.Equal(t, app.height-1, app.LastBlockHeight())
}
