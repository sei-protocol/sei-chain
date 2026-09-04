package core

import (
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	kvsink "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/sink/kv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func newAutobahnBroadcastEnv(t *testing.T) *Environment {
	t.Helper()
	rng := utils.TestRng()
	_, keys := atypes.GenCommittee(rng, 1)
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
	blockStore, err := blockstore.New(memblock.NewBlockDB())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, blockStore.Close()) })
	app := proxy.New(&abci.BaseApplication{})
	commonCfg := p2p.GigaRouterCommonConfig{
		DialInterval:   time.Second,
		ValidatorAddrs: addrs,
		App:            app,
		GenDoc:         genDoc,
	}
	dataState, err := p2p.BuildDataState(&commonCfg, blockStore)
	require.NoError(t, err)
	giga, err := p2p.NewGigaValidatorRouter(&p2p.GigaValidatorConfig{
		GigaRouterCommonConfig: commonCfg,
		ValidatorKey:           valKey,
		ViewTimeout:            func(atypes.View) time.Duration { return time.Hour },
		Producer: &producer.Config{
			MaxGasWantedPerBlock:    1,
			MaxGasEstimatedPerBlock: 1,
			MaxTxsPerBlock:          1,
			MaxTxsPerSecond:         utils.None[uint64](),
			BlockInterval:           time.Second,
		},
	}, nodeKey, dataState)
	require.NoError(t, err)
	require.True(t, giga.Mempool().IsPresent(), "validator GigaRouter must expose a mempool so a regression would wait, not take the fullnode shortcut")
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
		App:        app,
		GenDoc:     genDoc,
		Router:     router,
		EventSinks: []indexer.EventSink{kvsink.NewEventSink(dbm.NewMemDB(), nil)},
		Config:     *config.DefaultRPCConfig(),
	}
}
