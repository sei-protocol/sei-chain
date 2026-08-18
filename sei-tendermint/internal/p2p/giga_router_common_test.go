package p2p

import (
	"context"
	"net/url"
	"testing"
	"time"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashvault"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func registerEvmProxyForTest(t *testing.T, router *gigaRouterCommon, validator atypes.PublicKey, rpcURL *url.URL) *ethrpc.Client {
	t.Helper()
	client, err := ethrpc.DialContext(t.Context(), rpcURL.String())
	require.NoError(t, err)
	t.Cleanup(client.Close)
	for proxies := range router.proxies.Lock() {
		proxies[validator] = client
	}
	return client
}

type fixedHeightApp struct {
	abci.BaseApplication
	height int64
}

func (a *fixedHeightApp) LastBlockHeight() int64 { return a.height }

func (a *fixedHeightApp) Info() *abci.ResponseInfo {
	return &abci.ResponseInfo{LastBlockHeight: a.height}
}

// newSeededVault returns a durable Pebble vault rooted in a temp dir with hash committed at height.
func newSeededVault(t *testing.T, height atypes.GlobalBlockNumber, hash []byte) hashvault.HashVault {
	t.Helper()
	cfg := hashvault.DefaultHashVaultConfig()
	cfg.DataDir = t.TempDir()
	v, err := hashvault.NewUnsafePebbleHashVault(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close(context.Background()) })
	require.NoError(t, v.CommitToHash(context.Background(), uint64(height), hash))
	return v
}

// TestCommitHashToVault covers the safety contract the restart path in runExecute relies on:
// an idempotent match returns nil, a divergent hash halts the node (panic), and a canceled
// context returns an error without halting.
func TestCommitHashToVault(t *testing.T) {
	const height atypes.GlobalBlockNumber = 42
	h1 := make([]byte, hashvault.BlockHashSize)
	for i := range h1 {
		h1[i] = 0xAA
	}
	h2 := make([]byte, hashvault.BlockHashSize)
	for i := range h2 {
		h2[i] = 0xBB
	}

	t.Run("matching hash is idempotent", func(t *testing.T) {
		vault := newSeededVault(t, height, h1)
		require.NoError(t, commitAppHashToVault(context.Background(), vault, height, h1))
	})

	t.Run("divergent hash halts the node", func(t *testing.T) {
		vault := newSeededVault(t, height, h1)
		require.Panics(t, func() {
			_ = commitAppHashToVault(context.Background(), vault, height, h2)
		})
	})

	t.Run("canceled context returns error without halting", func(t *testing.T) {
		vault := newSeededVault(t, height, h1)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		// Must not panic: a canceled context is a benign shutdown, not an equivocation.
		err := commitAppHashToVault(ctx, vault, height, h2)
		require.Error(t, err)
	})
}

func TestFinalizeBlockGasUsed(t *testing.T) {
	resp := &abci.ResponseFinalizeBlock{
		TxResults: []*abci.ExecTxResult{
			{GasUsed: 10},
			nil,
			{GasUsed: -1},
			{GasUsed: 20},
		},
	}
	require.Equal(t, int64(30), finalizeBlockGasUsed(resp))
}

func TestBuildDataStateStartsRecoveryAtAppTip(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	keys := []atypes.SecretKey{key}
	validatorAddrs := map[atypes.PublicKey]GigaNodeAddr{key.Public(): {}}

	genDoc := &tmtypes.GenesisDoc{
		ChainID:         "restart-tip-test",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		ConsensusParams: tmtypes.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	committee, err := atypes.NewCommittee(map[atypes.PublicKey]uint64{key.Public(): 1})
	require.NoError(t, err)
	registry, err := epoch.NewRegistry(committee, atypes.GlobalBlockNumber(genDoc.InitialHeight), genDoc.GenesisTime)
	require.NoError(t, err)
	qc, blocks := data.TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*atypes.CommitQC]())
	gr := qc.QC().GlobalRange()
	require.Greater(t, gr.Len(), 2)
	last := gr.First + atypes.GlobalBlockNumber(gr.Len()/2)

	db := memblock.NewBlockDB()
	require.NoError(t, db.WriteQC(qc))
	for i, n := 0, gr.First; n < gr.Next; i, n = i+1, n+1 {
		require.NoError(t, db.WriteBlock(n, blocks[i]))
	}
	require.NoError(t, db.Flush())
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	state, err := BuildDataState(&GigaRouterCommonConfig{
		DialInterval:   time.Second,
		ValidatorAddrs: validatorAddrs,
		GenDoc:         genDoc,
		App:            proxy.New(&fixedHeightApp{height: int64(last)}),
	}, db)
	require.NoError(t, err)
	got, err := state.TryBlock(last)
	require.NoError(t, err)
	require.Equal(t, blocks[gr.Len()/2].Header().Hash(), got.Header().Hash())
}
