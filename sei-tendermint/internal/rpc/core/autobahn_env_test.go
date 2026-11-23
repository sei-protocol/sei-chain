package core

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	kvsink "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/sink/kv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type stubAppHashBuilder struct{}

func (stubAppHashBuilder) ReportBlockHash(context.Context, uint64, [32]byte) error { return nil }
func (stubAppHashBuilder) ReportStateHash(context.Context, uint64, [32]byte, map[string][]byte) error {
	return nil
}
func (stubAppHashBuilder) ReportBUDHash(context.Context, uint64, [32]byte) error { return nil }
func (stubAppHashBuilder) ReportReceiptHash(context.Context, uint64, apphash.ReceiptHash) error {
	return nil
}
func (stubAppHashBuilder) SetupComplete(context.Context, uint64) error { return nil }
func (stubAppHashBuilder) GetAppHash(ctx context.Context, _ uint64) (*apphash.AppHashData, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (stubAppHashBuilder) Flush(context.Context) error           { return nil }
func (stubAppHashBuilder) Bounds() (uint64, uint64, bool, error) { return 0, 0, false, nil }
func (stubAppHashBuilder) Close() error                          { return nil }

type broadcastEVMApp struct{ abevm.Application }

func (broadcastEVMApp) LastBlockHeight() int64 { return 0 }

// EvmNonce is the nonce the producer reports before its mempool is running.
func (broadcastEVMApp) EvmNonce(common.Address) uint64 { return 7 }

func newAutobahnBroadcastEnv(t *testing.T) *Environment {
	t.Helper()
	return newAutobahnEnv(t, true)
}

func newAutobahnFullnodeEnv(t *testing.T) *Environment {
	t.Helper()
	return newAutobahnEnv(t, false)
}

func newAutobahnEnv(t *testing.T, validator bool) *Environment {
	t.Helper()
	return newAutobahnEnvWithApp(t, validator, abevm.NewProxy(broadcastEVMApp{}))
}

func newAutobahnEnvWithApp(t *testing.T, validator bool, app *abevm.Proxy) *Environment {
	t.Helper()
	rng := utils.TestRng()
	committee, keys := atypes.GenCommittee(rng, 1)
	valKey := keys[0]
	nodeKey := p2p.NodeSecretKey(ed25519.TestSecretKey(utils.GenBytes(rng, 32)))
	genDoc := &types.GenesisDoc{
		ChainID:         "rpc-autobahn",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		ConsensusParams: types.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())
	addrs := map[atypes.PublicKey]p2p.GigaNodeAddr{
		valKey.Public(): {
			Key:      nodeKey.Public(),
			HostPort: tcp.HostPort{Hostname: "127.0.0.1", Port: 26657},
			EVMRPC:   *utils.OrPanic1(url.Parse("ws://127.0.0.1:8546")),
		},
	}
	blockStore := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, blockStore.Close()) })
	signer := newTestSigner(app.EvmChainID(), 0)
	commonCfg := p2p.GigaRouterCommonConfig{
		DialInterval:       time.Second,
		ValidatorAddrs:     addrs,
		PersistentStateDir: t.TempDir(),
		App:                app,
		AppHashBuilder:     stubAppHashBuilder{},
		GenDoc:             genDoc,
	}
	registry, err := epoch.TestGenesisRegistry(committee, atypes.RoadHeight(genDoc.InitialHeight), epoch.DefaultEpochLength)
	require.NoError(t, err)
	dataState, err := p2p.BuildDataState(&commonCfg, blockStore, abevm.TestEpochApp(registry), genDoc.GenesisTime)
	require.NoError(t, err)
	var giga p2p.GigaRouter
	if validator {
		router, err := p2p.NewGigaValidatorRouter(&p2p.GigaValidatorConfig{
			GigaRouterCommonConfig: commonCfg,
			ValidatorKey:           valKey,
			ViewTimeout:            func(atypes.View) time.Duration { return time.Hour },
			ProposalTimeout:        time.Hour,
			Producer: &producer.Config{
				MaxGasWantedPerBlock:    1,
				MaxGasEstimatedPerBlock: 1,
				MaxTxsPerBlock:          1,
				MaxTxsPerSecond:         utils.None[uint64](),
				BlockInterval:           time.Second,
			},
		}, nodeKey, dataState, signer)
		require.NoError(t, err)
		require.True(t, router.Mempool().IsPresent(), "validator GigaRouter must expose a mempool so a regression would wait, not take the fullnode shortcut")
		giga = router
	} else {
		router, err := p2p.NewGigaFullnodeRouter(&commonCfg, nodeKey, dataState, signer)
		require.NoError(t, err)
		require.False(t, router.Mempool().IsPresent())
		giga = router
	}
	endpoint := p2p.Endpoint{AddrPort: tcp.TestReserveAddr()}
	nodeInfo := types.NodeInfo{
		NodeID:     nodeKey.Public().NodeID(),
		ListenAddr: endpoint.String(),
		Moniker:    string(nodeKey.Public().NodeID()),
		Network:    genDoc.ChainID,
	}
	router, err := p2p.NewRouter(
		nodeKey,
		func() *types.NodeInfo { return &nodeInfo },
		dbm.NewMemDB(),
		&p2p.RouterOptions{
			Endpoint:                 endpoint,
			Connection:               conn.DefaultMConnConfig(),
			IncomingConnectionWindow: utils.Some(time.Duration(0)),
			MaxAcceptRate:            rate.Inf,
			MaxDialRate:              rate.Inf,
			Giga:                     utils.Some[p2p.GigaRouter](giga),
		},
	)
	require.NoError(t, err)
	return &Environment{
		EVM:        utils.Some(app),
		GenDoc:     genDoc,
		Router:     router,
		EventSinks: []indexer.EventSink{kvsink.NewEventSink(dbm.NewMemDB(), nil)},
		Config:     *config.DefaultRPCConfig(),
	}
}

func (broadcastEVMApp) EvmChainID() uint64 { return 1 }
