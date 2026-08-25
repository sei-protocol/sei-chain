package core

import (
	"errors"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	kvsink "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/sink/kv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestBroadcastTxCommitUnderAutobahnFailsFast(t *testing.T) {
	// Setup: Giga validator RPC env with a real mempool and an empty KV indexer.
	env := newAutobahnBroadcastEnv(t)

	// Test: BroadcastTxCommit with TimeoutBroadcastTxCommit=0 (hangs on a wait regression).
	res, err := env.BroadcastTxCommit(t.Context(), &coretypes.RequestBroadcastTx{Tx: []byte("tx")})

	// Verify: Autobahn sentinel and no result.
	require.ErrorIs(t, err, ErrBroadcastTxCommitUnsupported)
	require.Nil(t, res)
}

func TestBroadcastTxCommitWithoutAutobahnUsesMempool(t *testing.T) {
	// Setup: RPC env with no GigaRouter (Comet path).
	env := &Environment{}

	// Test: BroadcastTxCommit with no local mempool either.
	_, err := env.BroadcastTxCommit(t.Context(), &coretypes.RequestBroadcastTx{Tx: []byte("tx")})

	// Verify: mempool error, not the Autobahn sentinel.
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrBroadcastTxCommitUnsupported))
}

func newAutobahnBroadcastEnv(t *testing.T) *Environment {
	t.Helper()
	// Setup: one-validator committee and node identity.
	rng := utils.TestRng()
	_, keys := atypes.GenCommittee(rng, 1)
	valKey := keys[0]
	nodeKey := p2p.NodeSecretKey(ed25519.TestSecretKey(utils.GenBytes(rng, 32)))

	// Setup: genesis the router needs to build DataState.
	genDoc := &types.GenesisDoc{
		ChainID:         "broadcast-tx-commit-autobahn",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		ConsensusParams: types.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	// Setup: in-process validator addr map and empty block store.
	addrs := map[atypes.PublicKey]p2p.GigaNodeAddr{
		valKey.Public(): {
			Key:      nodeKey.Public(),
			HostPort: tcp.HostPort{Hostname: "127.0.0.1", Port: 26657},
		},
	}
	blockDB, err := blockstore.New(memblock.NewBlockDB())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, blockDB.Close()) })

	// Setup: Giga validator router so gigaRouter() is present and mempool exists.
	commonCfg := p2p.GigaRouterCommonConfig{
		DialInterval:   time.Second,
		ValidatorAddrs: addrs,
		App:            proxy.New(&abci.BaseApplication{}),
		GenDoc:         genDoc,
	}
	dataState, err := p2p.BuildDataState(&commonCfg, blockDB)
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

	// Setup: p2p Router wrapping that GigaRouter (Environment.gigaRouter reads this).
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

	// Setup: empty KV indexer; TimeoutBroadcastTxCommit stays 0 so a wait regression hangs.
	return &Environment{
		Router:     router,
		EventSinks: []indexer.EventSink{kvsink.NewEventSink(dbm.NewMemDB(), nil)},
	}
}
