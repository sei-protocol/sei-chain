package core

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/sink/kv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const autobahnBroadcastCommitteeSize = 4

// okApp is a minimal ABCI app whose CheckTx always succeeds.
type okApp struct{ abci.BaseApplication }

func (okApp) CheckTx(context.Context, *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
		Code:         abci.CodeTypeOK,
		GasWanted:    1,
		GasEstimated: 1,
	}}
}

type autobahnBroadcastNode struct {
	router *p2p.Router
	giga   p2p.GigaRouter
}

// TestBroadcastTxCommitUnderAutobahn verifies BroadcastTxCommit returns CheckTx
// and DeliverTx results once Autobahn has included the transaction.
func TestBroadcastTxCommitUnderAutobahn(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()

	// Setup: committee identities and p2p listen addresses.
	_, keys := atypes.GenCommittee(rng, autobahnBroadcastCommitteeSize)
	type nodeKeys struct {
		validator atypes.SecretKey
		node      p2p.NodeSecretKey
		addr      netip.AddrPort
	}
	cfgs := make([]nodeKeys, autobahnBroadcastCommitteeSize)
	addrs := map[atypes.PublicKey]p2p.GigaNodeAddr{}
	for i, key := range keys {
		nodeKey := p2p.NodeSecretKey(ed25519.TestSecretKey(utils.GenBytes(rng, 32)))
		addr := tcp.TestReserveAddr()
		cfgs[i] = nodeKeys{validator: key, node: nodeKey, addr: addr}
		addrs[key.Public()] = p2p.GigaNodeAddr{
			Key:      nodeKey.Public(),
			HostPort: tcp.HostPort{Hostname: addr.Addr().String(), Port: addr.Port()},
			EVMRPC:   utils.OrPanic1(url.Parse(fmt.Sprintf("http://%s:8545", addr.Addr().String()))),
		}
	}

	// Setup: shared genesis.
	genDoc := &types.GenesisDoc{
		ChainID:       "autobahn-broadcast-tx-commit",
		InitialHeight: 1,
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	// Setup: per-node EventBus, giga validator, and p2p router. Node 0 also runs the KV indexer.
	var indexSink indexer.EventSink
	var indexBus *eventbus.EventBus
	nodes := make([]autobahnBroadcastNode, autobahnBroadcastCommitteeSize)
	for i, cfg := range cfgs {
		bus := eventbus.NewDefault()
		require.NoError(t, bus.Start(ctx))
		t.Cleanup(bus.Wait)
		if i == 0 {
			indexBus = bus
			indexSink = kv.NewEventSink(dbm.NewMemDB(), nil)
			idx := indexer.NewService(indexer.ServiceArgs{
				Sinks:    []indexer.EventSink{indexSink},
				EventBus: bus,
			})
			require.NoError(t, idx.Start(ctx))
			t.Cleanup(idx.Wait)
		}

		db := memblock.NewBlockDB()
		blockStore, err := blockstore.New(db)
		require.NoError(t, err)
		t.Cleanup(func() { _ = blockStore.Close() })
		commonCfg := p2p.GigaRouterCommonConfig{
			DialInterval:            100 * time.Millisecond,
			ValidatorAddrs:          addrs,
			App:                     proxy.New(okApp{}),
			GenDoc:                  genDoc,
			HashVaultDisabledUnsafe: true,
			EventBus:                bus,
		}
		dataState, err := p2p.BuildDataState(&commonCfg, blockStore)
		require.NoError(t, err)

		giga, err := p2p.NewGigaValidatorRouter(&p2p.GigaValidatorConfig{
			GigaRouterCommonConfig: commonCfg,
			ValidatorKey:           cfg.validator,
			ViewTimeout:            func(atypes.View) time.Duration { return time.Hour },
			Producer: &producer.Config{
				MaxGasWantedPerBlock:    1_000_000,
				MaxGasEstimatedPerBlock: 1_000_000,
				MaxTxsPerBlock:          16,
				MaxTxsPerSecond:         utils.None[uint64](),
				BlockInterval:           100 * time.Millisecond,
				AllowEmptyBlocks:        false,
			},
		}, cfg.node, dataState)
		require.NoError(t, err)

		nodeInfo := autobahnBroadcastNodeInfo(cfg.node, cfg.addr.String(), genDoc.ChainID)
		endpoint := p2p.Endpoint{AddrPort: cfg.addr}
		router, err := p2p.NewRouter(
			cfg.node,
			func() *types.NodeInfo { return &nodeInfo },
			dbm.NewMemDB(),
			&p2p.RouterOptions{
				SelfAddress:              utils.Some(endpoint.NodeAddress(cfg.node.Public().NodeID())),
				Endpoint:                 endpoint,
				Connection:               conn.DefaultMConnConfig(),
				IncomingConnectionWindow: utils.Some(time.Duration(0)),
				MaxAcceptRate:            rate.Inf,
				MaxDialRate:              rate.Limit(30),
				Giga:                     utils.Some[p2p.GigaRouter](giga),
			},
		)
		require.NoError(t, err)
		nodes[i] = autobahnBroadcastNode{router: router, giga: giga}
	}

	// Setup: RPC Environment on node 0 (Autobahn router + KV sink).
	env := &Environment{
		Router:     nodes[0].router,
		EventSinks: []indexer.EventSink{indexSink},
		EventBus:   indexBus,
		Config:     config.RPCConfig{},
	}

	// Test: start the cluster, then BroadcastTxCommit until inclusion (or hang).
	tx := types.Tx(utils.GenBytes(rng, 32))
	var res *coretypes.ResultBroadcastTxCommit
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for i, n := range nodes {
			s.SpawnBgNamed(fmt.Sprintf("router[%v]", i), func() error {
				return utils.IgnoreCancel(n.router.Run(ctx))
			})
			s.SpawnBgNamed(fmt.Sprintf("giga[%v]", i), func() error {
				return utils.IgnoreCancel(n.giga.Run(ctx))
			})
		}
		var err error
		res, err = env.BroadcastTxCommit(ctx, &coretypes.RequestBroadcastTx{Tx: tx})
		return err
	})

	// Validate: CheckTx OK, hash matches, height assigned, DeliverTx OK.
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, abci.CodeTypeOK, res.CheckTx.Code)
	require.Equal(t, tx.Hash().Bytes(), res.Hash)
	require.Positive(t, res.Height)
	require.Equal(t, abci.CodeTypeOK, res.TxResult.Code)
}

func autobahnBroadcastNodeInfo(key p2p.NodeSecretKey, listen, chainID string) types.NodeInfo {
	nodeID := key.Public().NodeID()
	return types.NodeInfo{
		NodeID:     nodeID,
		ListenAddr: listen,
		Network:    chainID,
		Moniker:    string(nodeID),
		Channels:   []byte{},
		ProtocolVersion: types.ProtocolVersion{
			P2P:   1,
			Block: 2,
			App:   3,
		},
		Version: "1.2.3",
		Other: types.NodeInfoOther{
			TxIndex:    "on",
			RPCAddress: "127.0.0.1:26657",
		},
	}
}
