package core

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
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
func (broadcastEVMApp) EvmBlockNumber() int64  { return 0 }

// EvmNonce is the nonce the producer reports before its mempool is running.
func (broadcastEVMApp) EvmNonce(common.Address) uint64 { return 7 }

func newAutobahnBroadcastEnv(t *testing.T) *GigaEnvironment {
	t.Helper()
	return newAutobahnEnv(t, true)
}

func newAutobahnFullnodeEnv(t *testing.T) *GigaEnvironment {
	t.Helper()
	return newAutobahnEnv(t, false)
}

func newAutobahnEnv(t *testing.T, validator bool) *GigaEnvironment {
	t.Helper()
	return newAutobahnEnvWithApp(t, validator, abevm.NewProxy(broadcastEVMApp{}))
}

func newAutobahnEnvWithApp(t *testing.T, validator bool, app *abevm.Proxy) *GigaEnvironment {
	t.Helper()
	return newAutobahnEnvWithSigner(t, validator, app, newTestSigner(app.EvmChainID(), 0))
}

func newAutobahnEnvWithSigner(t *testing.T, validator bool, app *abevm.Proxy, signer *abevm.Signer) *GigaEnvironment {
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
		},
	}
	blockStore := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, blockStore.Close()) })
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
			EVMRPC:                 url.URL{Scheme: "ws", Host: "127.0.0.1:8546"},
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
	return &GigaEnvironment{
		Proxy:  app,
		GenDoc: genDoc,
		Router: giga.Common(),
		Executor: execution.New(&execution.Config{
			GenDoc:         genDoc,
			App:            app,
			Signer:         signer,
			AppHashBuilder: commonCfg.AppHashBuilder,
			BlockStore:     blockStore,
		}),
	}
}

func (broadcastEVMApp) EvmChainID() uint64 { return 1 }
