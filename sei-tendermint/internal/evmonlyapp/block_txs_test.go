package evmonlyapp

import (
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestBlockTxsHashes(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	require.Zero(t, app.EvmBlockNumber())
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	first := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0)
	second := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1)
	third := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 2)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1, first))
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2, first, second, second, third))
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 3))
	store := app.(*evmOnlyApplication).storage.ReceiptDB()
	require.NoError(t, store.Flush())
	proxy := abevm.NewProxy(app)
	require.Equal(t, int64(3), proxy.EvmBlockNumber())
	for _, height := range []int64{-1, 0} {
		_, err := proxy.BlockTxsHashes(height)
		require.Error(t, err)
	}

	hashes, err := proxy.BlockTxsHashes(1)
	require.NoError(t, err)
	require.Equal(t, []common.Hash{crypto.Keccak256Hash(first)}, hashes)
	hashes, err = proxy.BlockTxsHashes(2)
	require.NoError(t, err)
	require.Equal(t, []common.Hash{crypto.Keccak256Hash(second), crypto.Keccak256Hash(third)}, hashes)
	hashes, err = proxy.BlockTxsHashes(3)
	require.NoError(t, err)
	require.Empty(t, hashes)
	_, err = proxy.BlockTxsHashes(4)
	require.ErrorIs(t, err, coretypes.ErrHeightExceedsChainHead)
	require.NoError(t, store.PruneHistory(3))
	_, err = proxy.BlockTxsHashes(2)
	require.ErrorIs(t, err, coretypes.ErrHeightNotAvailable)
}

func TestBlockTxsHashesWithoutReceipts(t *testing.T) {
	config, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	config.ReceiptDBConfig.Enable = false
	app := newEVMOnlyTestAppWithStorage(t, nil, gigaconfig.DefaultConfig.Execution, config)
	_, err = app.BlockTxsHashes(1)
	require.ErrorIs(t, err, receiptdb.ErrNotConfigured)
	require.NoError(t, app.InitChain(evmOnlyTestInitChain()))
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1))
	require.Equal(t, int64(1), app.LastBlockHeight())
	require.Zero(t, app.EvmBlockNumber())
}

func TestGigaBlocksReconstructReorderedAndFilteredTransactions(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			rng := utils.TestRng()
			committee, keys := atypes.GenCommittee(rng, 1)
			validators := abevm.CommitteeWeights{keys[0].Public(): committee.Weight(keys[0].Public())}
			execution := gigaconfig.DefaultConfig.Execution
			execution.OCCWorkers = workers
			app := newEVMOnlyTestAppWithExecution(t, validators, execution)
			require.NoError(t, app.InitChain(evmOnlyTestInitChain()))
			proxy := abevm.NewProxy(app)
			storage := app.(*evmOnlyApplication).storage
			registry, err := app.EpochRegistry()
			require.NoError(t, err)
			ep, err := registry.Epoch(0)
			require.NoError(t, err)

			key, err := crypto.GenerateKey()
			require.NoError(t, err)
			first := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0)
			second := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1)
			afterGap := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 3)
			payload, err := (atypes.PayloadBuilder{
				CreatedAt: time.Unix(1, 0), Txs: [][]byte{second, first, first, afterGap},
			}).Build()
			require.NoError(t, err)
			lane := ep.Committee().Lanes().At(0)
			laneBlock := atypes.NewLaneBlock(lane, atypes.LaneRangeOpt(utils.None[*atypes.CommitQC](), lane).Next(), atypes.LaneBlockHeaderHash{}, payload)
			laneQC := data.TestLaneQC(ep.EpochIndex(), keys, laneBlock.Header())
			view := atypes.ViewSpec{ConsensusSpec: atypes.ConsensusSpec{Epoch: ep, GenesisTimestamp: time.Unix(1, 0)}}
			proposal, err := atypes.NewProposal(keys[0], view, view.NextTimestamp(), map[atypes.LaneID]*atypes.LaneQC{lane: laneQC})
			require.NoError(t, err)
			prepareQC := atypes.BuildPrepareQC(keys, proposal.Proposal().Msg())
			commitQC := atypes.NewCommitQC([]*atypes.Signed[*atypes.CommitVote]{keys[0].Sign(atypes.NewCommitVote(prepareQC))})
			qc := atypes.NewFullCommitQC(commitQC, []*atypes.LaneBlockHeader{laneBlock.Header()})
			require.NoError(t, qc.Verify(ep))
			blockStore := blockstore.NewTestStore(t)
			defer func() { require.NoError(t, blockStore.Close()) }()
			state, err := data.NewState(&data.Config{App: proxy, GenesisTimestamp: time.Unix(1, 0)}, blockStore)
			require.NoError(t, err)
			require.NoError(t, state.PushPartialBlock(t.Context(), qc, data.TestBlockMap([]*atypes.LaneBlock{laneBlock})))
			header := qc.Header()
			road, err := state.RoadBlock(t.Context(), header.Height())
			require.NoError(t, err)
			signer := newTestSigner(evmOnlyTestChainID, 0)
			raw := make([]abevm.RawTx, len(road.Txs()))
			for i, tx := range road.Txs() {
				raw[i] = tx
			}
			txs, err := signer.ParseTxs(raw)
			require.NoError(t, err)
			response, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
				Height: header.Height(), Time: header.Timestamp(), Hash: common.Hash(header.Hash()), Txs: txs,
			})
			require.NoError(t, err)
			require.Equal(t, uint64(42_000), response.GasUsed)
			require.NoError(t, app.AwaitCommits())
			require.NoError(t, storage.ReceiptDB().Flush())
			require.Equal(t, uint64(2), app.EvmNonce(crypto.PubkeyToAddress(key.PublicKey)))
			hashes, err := proxy.BlockTxsHashes(1)
			require.NoError(t, err)
			require.Equal(t, []common.Hash{abevm.RawTx(first).Hash(), abevm.RawTx(second).Hash()}, hashes)

			router, err := p2p.NewGigaFullnodeRouter(&p2p.GigaRouterCommonConfig{
				App: proxy, GenDoc: &types.GenesisDoc{ChainID: "reordered-block-test", InitialHeight: 1},
				AppHashBuilder: storage.AppHashBuilder(), PersistentStateDir: t.TempDir(),
			}, p2p.NodeSecretKey(ed25519.TestSecretKey(utils.GenBytes(rng, 32))), state, signer)
			require.NoError(t, err)
			byNumber, err := router.Common().BlockByNumber(t.Context(), header.Height())
			require.NoError(t, err)
			require.Equal(t, [][]byte{first, second}, byNumber.Txs)
			require.Equal(t, common.Hash(header.Hash()), byNumber.Hash)
			byHash, err := router.Common().BlockByHash(t.Context(), byNumber.Hash)
			require.NoError(t, err)
			require.Equal(t, byNumber, byHash.OrPanic("executed block"))
		})
	}
}
