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
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
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
	registry, err := epoch.NewRegistry(committee, atypes.GlobalBlockNumber(genDoc.InitialHeight), genDoc.GenesisTime, utils.None[string]())
	require.NoError(t, err)
	qc, blocks := data.TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*atypes.CommitQC]())
	gr := qc.QC().GlobalRange()
	require.Greater(t, gr.Len(), 2)
	last := gr.First + atypes.GlobalBlockNumber(gr.Len()/2)

	db, err := blockstore.New(memblock.NewBlockDB())
	require.NoError(t, err)
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

func TestGigaRouterCommon_ValidatorsAtGlobalHeight(t *testing.T) {
	rng := utils.TestRng()
	low := atypes.GenSecretKey(rng)
	mid := atypes.GenSecretKey(rng)
	high := atypes.GenSecretKey(rng)
	keys := []atypes.SecretKey{low, mid, high}
	router := testGigaRouterWithData(t, map[atypes.PublicKey]GigaNodeAddr{
		low.Public():  {},
		mid.Public():  {},
		high.Public(): {},
	})
	first := router.data.Registry().FirstBlock()

	got, h, err := router.Validators(first)
	require.NoError(t, err)
	require.Equal(t, first, h)
	require.Len(t, got, 3)
	require.Equal(t, []int64{1, 1, 1}, []int64{got[0].VotingPower, got[1].VotingPower, got[2].VotingPower})

	_, _, err = router.Validators(0)
	require.ErrorIs(t, err, coretypes.ErrHeightNotAvailable)

	_, _, err = router.Validators(first + 100)
	require.ErrorIs(t, err, coretypes.ErrHeightExceedsChainHead)

	weights := map[atypes.PublicKey]uint64{
		low.Public():  1,
		mid.Public():  5,
		high.Public(): 10,
	}
	require.NoError(t, router.data.Registry().StageAndActivate(0, weights))
	fakeNext := utils.NewAtomicSend(router.data.Registry().MustEpoch(2))
	router.nextCommitEpoch = fakeNext.Subscribe()
	got, h, err = router.Validators(first)
	require.NoError(t, err)
	require.Equal(t, first, h)
	require.Equal(t, []int64{1, 1, 1}, []int64{got[0].VotingPower, got[1].VotingPower, got[2].VotingPower})

	n := pushQCAtRoad(t, router, keys, router.data.Registry().MustEpoch(2), epoch.FirstRoad(2))
	got, h, err = router.Validators(n)
	require.NoError(t, err)
	require.Equal(t, n, h)
	require.Equal(t, []int64{10, 5, 1}, []int64{got[0].VotingPower, got[1].VotingPower, got[2].VotingPower})
	require.Equal(t, high.Public().Bytes(), got[0].PubKey.Bytes())
	require.Equal(t, mid.Public().Bytes(), got[1].PubKey.Bytes())
	require.Equal(t, low.Public().Bytes(), got[2].PubKey.Bytes())

	require.NoError(t, router.data.Registry().StageAndActivate(1, weights))
	require.NoError(t, router.data.Registry().StageAndActivate(2, weights))
	require.NoError(t, router.data.Registry().PruneBefore(4))
	_, _, err = router.Validators(n)
	require.ErrorIs(t, err, coretypes.ErrHeightNotAvailable)
}

func pushQCAtRoad(t *testing.T, router *gigaRouterCommon, keys []atypes.SecretKey, ep *atypes.Epoch, road atypes.RoadIndex) atypes.GlobalBlockNumber {
	t.Helper()
	first := router.data.Registry().FirstBlock()
	proposal, blocks := atypes.ProposalAtBlocks(ep, atypes.View{Index: road, Number: 0}, first, 1)
	votes := make([]*atypes.Signed[*atypes.CommitVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, atypes.Sign(k, atypes.NewCommitVote(proposal)))
	}
	headers := make([]*atypes.BlockHeader, len(blocks))
	for i, b := range blocks {
		headers[i] = b.Header()
	}
	qc := atypes.NewFullCommitQC(atypes.NewCommitQC(votes), headers)
	require.NoError(t, router.data.PushQC(t.Context(), qc, blocks))
	return first
}

func testGigaRouterWithData(t *testing.T, addrs map[atypes.PublicKey]GigaNodeAddr) *gigaRouterCommon {
	t.Helper()
	genDoc := &tmtypes.GenesisDoc{
		ChainID:         "validators-road-test",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		ConsensusParams: tmtypes.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())
	db, err := blockstore.New(memblock.NewBlockDB())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	state, err := BuildDataState(&GigaRouterCommonConfig{
		DialInterval:   time.Second,
		ValidatorAddrs: addrs,
		GenDoc:         genDoc,
		App:            proxy.New(&fixedHeightApp{height: 1}),
	}, db)
	require.NoError(t, err)
	return &gigaRouterCommon{
		data:            state,
		nextCommitEpoch: state.NextCommitEpoch(),
	}
}

func TestCommitteeWeights(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	wantPK := utils.OrPanic1(atypes.PublicKeyFromBytes(sk.Public().Bytes()))
	got, err := committeeWeights([]abci.ValidatorUpdate{{
		PubKey: crypto.PubKeyToProto(sk.Public()),
		Power:  42,
	}})
	require.NoError(t, err)
	require.Equal(t, map[atypes.PublicKey]uint64{wantPK: 42}, got)
}

func TestCommitteeWeights_SkipsZeroPower(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	got, err := committeeWeights([]abci.ValidatorUpdate{{
		PubKey: crypto.PubKeyToProto(sk.Public()),
		Power:  0,
	}})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestCommitteeWeights_DuplicateKey(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	pk := crypto.PubKeyToProto(sk.Public())
	_, err := committeeWeights([]abci.ValidatorUpdate{
		{PubKey: pk, Power: 1},
		{PubKey: pk, Power: 2},
	})
	require.Error(t, err)
}
