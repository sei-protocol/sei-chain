package core

import (
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type blockTransactionsApp struct {
	abevm.Application
	hashes    []common.Hash
	err       error
	gotHeight int64
	missing   bool
}

func (*blockTransactionsApp) EvmChainID() uint64     { return 1 }
func (*blockTransactionsApp) LastBlockHeight() int64 { return 2 }
func (*blockTransactionsApp) EvmBlockNumber() int64  { return 1 }
func (a *blockTransactionsApp) BlockTxsHashes(height int64) (utils.Option[[]common.Hash], error) {
	a.gotHeight = height
	if a.missing {
		return utils.None[[]common.Hash](), nil
	}
	return utils.Some(a.hashes), a.err
}

func TestGigaBlocksUseExecutedTransactions(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := atypes.GenCommittee(rng, 1)
	registry, err := epoch.TestGenesisRegistry(committee, 1, epoch.DefaultEpochLength)
	require.NoError(t, err)
	ep, err := registry.Snapshot().Epoch(0)
	require.NoError(t, err)
	var raw [][]byte
	for nonce := range uint64(3) {
		tx := ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: nonce})
		raw = append(raw, utils.OrPanic1(tx.MarshalBinary()))
	}
	payload := utils.OrPanic1((atypes.PayloadBuilder{CreatedAt: time.Unix(1, 0), Txs: raw}).Build())
	qc, blocks := data.TestCommitQCWithPayload(rng, ep, keys, utils.None[*atypes.CommitQC](), func() *atypes.Payload { return payload })
	state, err := data.NewState(&data.Config{App: abevm.TestEpochApp(registry), GenesisTimestamp: time.Unix(1, 0)}, blockstore.NewTestStore(t))
	require.NoError(t, err)
	require.NoError(t, state.PushPartialBlock(t.Context(), qc, data.TestBlockMap(blocks)))
	app := &blockTransactionsApp{hashes: []common.Hash{crypto.Keccak256Hash(raw[2]), crypto.Keccak256Hash(raw[1])}}
	proxy := abevm.NewProxy(app)
	genDoc := &types.GenesisDoc{ChainID: "executed-block-test", InitialHeight: 1}
	router, err := p2p.NewGigaFullnodeRouter(&p2p.GigaRouterCommonConfig{
		App: proxy, GenDoc: genDoc, AppHashBuilder: stubAppHashBuilder{}, PersistentStateDir: t.TempDir(),
	}, p2p.NodeSecretKey(ed25519.TestSecretKey(utils.GenBytes(rng, 32))), state, newTestSigner(1, 0))
	require.NoError(t, err)
	env := &GigaEnvironment{Proxy: proxy, Router: router.Common(), GenDoc: genDoc}
	require.Equal(t, int64(1), env.EvmBlockNumber())
	require.Equal(t, int64(2), env.LastBlockHeight())
	for _, height := range []int64{-1, 0} {
		found, err := env.BlockByNumber(t.Context(), utils.Some(height))
		require.ErrorIs(t, err, abevm.ErrPruned)
		require.False(t, found.IsPresent())
	}
	future, err := env.BlockByNumber(t.Context(), utils.Some[int64](2))
	require.NoError(t, err)
	require.False(t, future.IsPresent())
	for _, height := range []int64{0, 2} {
		found, err := router.Common().BlockByNumber(t.Context(), utils.Some(height))
		if height == 0 {
			require.ErrorIs(t, err, abevm.ErrPruned)
		} else {
			require.NoError(t, err)
		}
		require.False(t, found.IsPresent())
	}

	found, err := env.BlockByNumber(t.Context(), utils.Some[int64](1))
	require.NoError(t, err)
	byNumber := found.OrPanic("finalized block")
	require.Equal(t, []abevm.RawTx{raw[2], raw[1]}, byNumber.Txs)
	require.Equal(t, int64(1), app.gotHeight)
	require.Equal(t, common.Hash(qc.Header().Hash()), byNumber.Hash)
	require.Equal(t, qc.Header().Timestamp(), byNumber.Time)
	require.Equal(t, genDoc.ChainID, byNumber.ChainID)
	latest, err := env.BlockByNumber(t.Context(), utils.None[int64]())
	require.NoError(t, err)
	require.Equal(t, byNumber, latest.OrPanic("latest block"))
	byHash, err := env.BlockByHash(t.Context(), byNumber.Hash)
	require.NoError(t, err)
	require.Equal(t, byNumber, byHash.OrPanic("known block"))

	app.hashes = []common.Hash{}
	empty, err := env.BlockByNumber(t.Context(), utils.Some[int64](1))
	require.NoError(t, err)
	require.Empty(t, empty.OrPanic("empty block").Txs)
	app.hashes = []common.Hash{{0xff}}
	_, err = env.BlockByNumber(t.Context(), utils.Some[int64](1))
	require.Error(t, err)
	app.missing = true
	missing, err := env.BlockByNumber(t.Context(), utils.Some[int64](1))
	require.NoError(t, err)
	require.False(t, missing.IsPresent())
	missing, err = env.BlockByHash(t.Context(), byNumber.Hash)
	require.NoError(t, err)
	require.False(t, missing.IsPresent())
	app.missing = false
	app.err = errors.New("receipt store failure")
	_, err = env.BlockByNumber(t.Context(), utils.Some[int64](1))
	require.ErrorIs(t, err, app.err)
	_, err = env.BlockByHash(t.Context(), byNumber.Hash)
	require.ErrorIs(t, err, app.err)
	unknown, err := env.BlockByHash(t.Context(), common.Hash{})
	require.NoError(t, err)
	require.False(t, unknown.IsPresent())
}
