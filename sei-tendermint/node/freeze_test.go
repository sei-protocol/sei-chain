package node

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	mempoolreactor "github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool/reactor"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub"
	rpccore "github.com/sei-protocol/sei-chain/sei-tendermint/internal/rpc/core"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestValidateFreezeHeight(t *testing.T) {
	for _, tc := range []struct {
		name          string
		freezeHeight  uint64
		initialHeight int64
		stateHeight   int64
		blockHeight   int64
		appHeight     int64
		wantErr       bool
	}{
		{name: "disabled"},
		{name: "below target", freezeHeight: 10, initialHeight: 1, stateHeight: 8, blockHeight: 9, appHeight: 8},
		{name: "immediately before target", freezeHeight: 10, initialHeight: 1, stateHeight: 9, blockHeight: 9, appHeight: 9},
		{name: "target below initial height", freezeHeight: 9, initialHeight: 10, wantErr: true},
		{name: "state at target", freezeHeight: 10, initialHeight: 1, stateHeight: 10, wantErr: true},
		{name: "block store at target", freezeHeight: 10, initialHeight: 1, blockHeight: 10, wantErr: true},
		{name: "application at target", freezeHeight: 10, initialHeight: 1, appHeight: 10, wantErr: true},
		{name: "target above max height", freezeHeight: uint64(math.MaxInt64) + 1, initialHeight: 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFreezeHeight(tc.freezeHeight, tc.initialHeight, tc.stateHeight, tc.blockHeight, tc.appHeight)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateFreezeHeight() error = %v, wantErr %t", err, tc.wantErr)
			}
		})
	}
}

func TestWithFreezeHeight(t *testing.T) {
	const height = uint64(123)
	if got := resolveOptions(WithFreezeHeight(height)).freezeHeight; got != height {
		t.Fatalf("freeze height = %d, want %d", got, height)
	}
}

func TestValidateFreezeMode(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mode         string
		freezeHeight uint64
		wantErr      bool
	}{
		{name: "disabled validator", mode: config.ModeValidator},
		{name: "full node", mode: config.ModeFull, freezeHeight: 10},
		{name: "validator", mode: config.ModeValidator, freezeHeight: 10, wantErr: true},
		{name: "seed", mode: config.ModeSeed, freezeHeight: 10, wantErr: true},
		{name: "unknown mode handled by node mode validation", mode: "unknown", freezeHeight: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFreezeMode(tc.mode, tc.freezeHeight)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateFreezeMode() error = %v, wantErr %t", err, tc.wantErr)
			}
		})
	}
}

func TestFreezeModeDisablesMempoolTraffic(t *testing.T) {
	cfg, err := config.ResetTestRoot(t.TempDir(), "freeze_mempool_test")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Mode = config.ModeFull
	// Broadcast checks below call rpcEnv in-process; skip the TCP listener
	// so Start does not race a freeLoopbackAddr (net.Listen cannot adopt a
	// TestReserveAddr, and a closed :0 bind is stealable on macOS).
	cfg.RPC.ListenAddress = ""
	nodeService, err := newLocalNodeService(t.Context(), cfg, WithFreezeHeight(2))
	if err != nil {
		t.Fatal(err)
	}
	node := nodeService.(*nodeImpl)

	if !node.mempool.IsPresent() {
		t.Fatal("internal mempool is unavailable")
	}
	if !node.rpcEnv.Mempool.IsPresent() {
		t.Fatal("RPC mempool reads are unavailable in freeze mode")
	}
	if !node.rpcEnv.ReadOnly {
		t.Fatal("RPC writes are enabled in freeze mode")
	}
	txRequest := &coretypes.RequestBroadcastTx{Tx: types.Tx{1}}
	for name, broadcast := range map[string]func() error{
		"async": func() error {
			_, err := node.rpcEnv.BroadcastTxAsync(t.Context(), txRequest)
			return err
		},
		"sync": func() error {
			_, err := node.rpcEnv.BroadcastTxSync(t.Context(), txRequest)
			return err
		},
		"default": func() error {
			_, err := node.rpcEnv.BroadcastTx(t.Context(), txRequest)
			return err
		},
		"commit": func() error {
			_, err := node.rpcEnv.BroadcastTxCommit(t.Context(), txRequest)
			return err
		},
	} {
		if err := broadcast(); !errors.Is(err, rpccore.ErrReadOnly) {
			t.Fatalf("%s RPC transaction broadcast error = %v, want ErrReadOnly", name, err)
		}
	}
	if _, err := node.rpcEnv.BroadcastEvidence(t.Context(), &coretypes.RequestBroadcastEvidence{}); !errors.Is(err, rpccore.ErrReadOnly) {
		t.Fatalf("RPC evidence broadcast error = %v, want ErrReadOnly", err)
	}
	if pending, err := node.rpcEnv.UnconfirmedTxs(t.Context(), &coretypes.RequestUnconfirmedTxs{}); err != nil {
		t.Fatalf("reading unconfirmed transactions: %v", err)
	} else if pending.Total != 0 {
		t.Fatalf("unconfirmed transaction total = %d, want 0", pending.Total)
	}
	for _, nodeService := range node.services {
		if _, ok := nodeService.(*mempoolreactor.Reactor); ok {
			t.Fatal("mempool reactor is enabled in freeze mode")
		}
	}
	if err := node.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		node.Stop()
		node.Wait()
	})
	if slices.Contains(node.NodeInfo().Channels, byte(mempoolreactor.MempoolChannel)) {
		t.Fatal("mempool channel is advertised in freeze mode")
	}
}

type lastHeaderRecordingApp struct {
	*kvstore.Application
	lastHeaders []*tmproto.Header
}

func (app *lastHeaderRecordingApp) InitLastHeader(lastHeader *tmproto.Header) {
	app.lastHeaders = append(app.lastHeaders, lastHeader)
}

// A frozen node commits no further blocks after a restart, so the node must
// initialize the app from the last stored header instead.
func TestFreezeModeRestartInitializesAppFromLastHeader(t *testing.T) {
	cfg, err := config.ResetTestRoot(t.TempDir(), "freeze_restart_test")
	require.NoError(t, err)
	cfg.RPC.ListenAddress = ""
	// The frozen node must reopen the validator's stores.
	cfg.DBBackend = "goleveldb"

	validator, err := newLocalNodeService(t.Context(), cfg)
	require.NoError(t, err)
	blocks, err := validator.(*nodeImpl).EventBus().SubscribeWithArgs(t.Context(), pubsub.SubscribeArgs{
		ClientID: "freeze_restart_test",
		Query:    types.EventQueryNewBlock,
		Limit:    10,
	})
	require.NoError(t, err)
	require.NoError(t, validator.Start(t.Context()))
	for range 2 {
		_, err := blocks.Next(t.Context())
		require.NoError(t, err)
	}
	validator.Stop()
	validator.Wait()

	cfg.Mode = config.ModeFull
	app := &lastHeaderRecordingApp{Application: kvstore.NewApplication()}
	app.SetValidators(utils.OrPanic1(types.GenesisDocFromFile(cfg.GenesisFile())).ValidatorUpdates())
	frozenService, err := New(t.Context(), cfg, func() {}, app, nil, nil, types.DefaultConsensusPolicy(), WithFreezeHeight(1000))
	require.NoError(t, err)
	frozen := frozenService.(*nodeImpl)
	lastHeight := frozen.blockStore.Height()
	require.GreaterOrEqual(t, lastHeight, int64(2))
	want := frozen.blockStore.LoadBlockMeta(lastHeight).Header

	require.NoError(t, frozen.Start(t.Context()))
	t.Cleanup(func() {
		frozen.Stop()
		frozen.Wait()
	})

	require.Equal(t, 1, len(app.lastHeaders))
	got, err := types.HeaderFromProto(app.lastHeaders[0])
	require.NoError(t, err)
	require.Equal(t, want.Hash(), got.Hash())
	require.Equal(t, lastHeight, got.Height)
	require.Equal(t, frozen.genesisDoc.ChainID, got.ChainID)
}
