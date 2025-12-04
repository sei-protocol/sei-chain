package consensus

import (
	"context"
	"fmt"
	mrand "math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel/sdk/trace"

	abciclient "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/abci/example/kvstore"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/internal/eventbus"
	"github.com/tendermint/tendermint/internal/proxy"
	sm "github.com/tendermint/tendermint/internal/state"
	"github.com/tendermint/tendermint/internal/store"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
)

// WALGenerateNBlocks generates a consensus WAL. It does this by
// spinning up a stripped down version of node (proxy app, event bus,
// consensus state) with a kvstore application and special consensus
// wal instance (byteBufferWAL) and waits until numBlocks are created.
// If the node fails to produce given numBlocks, it fails the test.
func WALGenerateNBlocks(t *testing.T, logger log.Logger, numBlocks int64) *WAL {
	ctx := t.Context()

	cfg := getConfig(t)

	app := kvstore.NewApplication()

	logger.Info("generating WAL (last height msg excluded)", "numBlocks", numBlocks)

	// COPY PASTE FROM node.go WITH A FEW MODIFICATIONS
	// NOTE: we can't import node package because of circular dependency.
	// NOTE: we don't do handshake so need to set state.Version.Consensus.App directly.
	genDoc, err := types.GenesisDocFromFile(cfg.GenesisFile())
	require.NoError(t, err)
	blockStoreDB := dbm.NewMemDB()
	stateDB := blockStoreDB
	stateStore := sm.NewStore(stateDB)
	state, err := sm.MakeGenesisState(genDoc)
	require.NoError(t, err)
	state.Version.Consensus.App = kvstore.ProtocolVersion
	require.NoError(t, stateStore.Save(state))
	blockStore := store.NewBlockStore(blockStoreDB)
	proxyApp := proxy.New(abciclient.NewLocalClient(logger, app), logger, proxy.NopMetrics())
	require.NoError(t, proxyApp.Start(ctx))
	t.Cleanup(proxyApp.Wait)
	eventBus := eventbus.NewDefault(logger.With("module", "events"))
	require.NoError(t, eventBus.Start(ctx))
	t.Cleanup(func() { eventBus.Stop(); eventBus.Wait() })
	mempool := emptyMempool{}
	evpool := sm.EmptyEvidencePool{}
	blockExec := sm.NewBlockExecutor(stateStore, log.NewNopLogger(), proxyApp, mempool, evpool, blockStore, eventBus, sm.NopMetrics())
	consensusState, err := NewState(logger, cfg.Consensus, stateStore, blockExec, blockStore, mempool, evpool, eventBus, []trace.TracerProviderOption{})
	require.NoError(t, err)
	privValidatorKeyFile := cfg.PrivValidator.KeyFile()
	privValidatorStateFile := cfg.PrivValidator.StateFile()
	privValidator, err := privval.LoadOrGenFilePV(privValidatorKeyFile, privValidatorStateFile)
	require.NoError(t, err)
	if privValidator != nil {
		consensusState.SetPrivValidator(ctx, utils.Some[types.PrivValidator](privValidator))
	}
	// END OF COPY PASTE
	err = scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(consensusState.Run(ctx)) })
		for {
			rs := consensusState.GetRoundState()
			if rs.Height > numBlocks {
				return nil
			}
			// TODO(gprusak): remove active polling once consensus state code is reasonable cleaned up.
			if err := utils.Sleep(ctx, 100*time.Millisecond); err != nil {
				return err
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	consensusState.wal.Close()
	wal, err := openWAL(cfg.Consensus.WalFile())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wal.Close)
	return wal
}

func randPort() int {
	// returns between base and base + spread
	base, spread := 20000, 20000
	// nolint:gosec // G404: Use of weak random number generator
	return base + mrand.Intn(spread)
}

// makeAddrs constructs local TCP addresses for node services.
// It uses consecutive ports from a random starting point, so that concurrent
// instances are less likely to collide.
func makeAddrs() (p2pAddr, rpcAddr string) {
	const addrTemplate = "tcp://127.0.0.1:%d"
	start := randPort()
	return fmt.Sprintf(addrTemplate, start), fmt.Sprintf(addrTemplate, start+1)
}

// getConfig returns a config for test cases
func getConfig(t *testing.T) *config.Config {
	c, err := config.ResetTestRoot(t.TempDir(), t.Name())
	require.NoError(t, err)

	p2pAddr, rpcAddr := makeAddrs()
	c.P2P.ListenAddress = p2pAddr
	c.RPC.ListenAddress = rpcAddr
	return c
}
