package evmrpc_test

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	rpctest "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/test"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var prunedBlocksErr = regexp.MustCompile(`^blocks (\d+) to (\d+) were pruned before the filter read them$`)

// startBlockFilterTestNode starts an in-process Tendermint node running the kvstore app, which
// keeps the last retainBlocks blocks (all of them when 0), and returns a local client to it.
func startBlockFilterTestNode(t *testing.T, retainBlocks int64) *local.Local {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cfg, err := rpctest.CreateConfig(t, t.Name())
	require.NoError(t, err)

	// Pruning keeps every block inside the evidence window, so the retain height only takes
	// effect once that window is narrower than the chain the test produces.
	genDoc, err := types.GenesisDocFromFile(cfg.GenesisFile())
	require.NoError(t, err)
	genDoc.ConsensusParams.Evidence.MaxAgeNumBlocks = 1
	genDoc.ConsensusParams.Evidence.MaxAgeDuration = time.Nanosecond
	require.NoError(t, genDoc.SaveAs(cfg.GenesisFile()))

	app := kvstore.NewApplication()
	app.RetainBlocks = retainBlocks
	node, closer, err := rpctest.StartTendermint(ctx, cfg, app, rpctest.SuppressStdout)
	require.NoError(t, err)
	t.Cleanup(func() {
		cancel()
		require.NoError(t, closer(context.Background()))
		require.NoError(t, app.Close())
	})

	nodeService, ok := node.(local.NodeService)
	require.True(t, ok)
	c, err := local.New(nodeService)
	require.NoError(t, err)
	return c
}

// waitForBlockFilterTestHeight waits until the node has committed height.
func waitForBlockFilterTestHeight(t *testing.T, c *local.Local, height int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	require.NoError(t, rpcclient.WaitForHeight(ctx, c, height, func(int64) error {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for height %d: %w", height, ctx.Err())
		case <-time.After(20 * time.Millisecond):
			return nil
		}
	}))
}

func latestNodeHeight(t *testing.T, c *local.Local) int64 {
	t.Helper()
	status, err := c.Status(t.Context())
	require.NoError(t, err)
	return status.SyncInfo.LatestBlockHeight
}

// newNodeBlockFilter creates a block filter, and returns its ID with the lowest and highest
// heights its first poll can start at: the blocks after the heads read before and after creation.
func newNodeBlockFilter(t *testing.T, api *evmrpc.FilterAPI, c *local.Local) (ethrpc.ID, int64, int64) {
	t.Helper()
	before := latestNodeHeight(t, c)
	id, err := api.NewBlockFilter(t.Context())
	require.NoError(t, err)
	return id, before + 1, latestNodeHeight(t, c) + 1
}

func pollNodeBlockFilter(t *testing.T, api *evmrpc.FilterAPI, id ethrpc.ID) []common.Hash {
	t.Helper()
	got, err := api.GetFilterChanges(t.Context(), id)
	require.NoError(t, err)
	return got.([]common.Hash)
}

func nodeBlockHeight(t *testing.T, c *local.Local, hash common.Hash) int64 {
	t.Helper()
	block, err := c.BlockByHash(t.Context(), hash.Bytes())
	require.NoError(t, err)
	require.NotNil(t, block.Block, "block %s", hash)
	return block.Block.Height
}

// requireNodeBlockHashes checks hashes against the hashes the node's Block endpoint reports for
// the heights from on.
func requireNodeBlockHashes(t *testing.T, c *local.Local, from int64, hashes []common.Hash) {
	t.Helper()
	for i, hash := range hashes {
		height := from + int64(i)
		block, err := c.Block(t.Context(), &height)
		require.NoError(t, err)
		require.Equal(t, common.BytesToHash(block.BlockID.Hash), hash, "block %d", height)
	}
}

// staleStatusClient answers the next Status call with stale, then defers to the node again, like a
// watermark read that lands just before pruning advances the block store's base.
type staleStatusClient struct {
	*local.Local
	stale *coretypes.ResultStatus
}

func (c *staleStatusClient) Status(ctx context.Context) (*coretypes.ResultStatus, error) {
	if stale := c.stale; stale != nil {
		c.stale = nil
		return stale, nil
	}
	return c.Local.Status(ctx)
}

func TestFilterAPI_BlockFilterWithoutNotifierPollsNodeAcrossPages(t *testing.T) {
	// Setup: a node keeping every block, and a filter created once it is producing blocks.
	c := startBlockFilterTestNode(t, 0)
	waitForBlockFilterTestHeight(t, c, 2)
	api := evmrpc.NewBlockFilterAPIForTest(t, c, c)
	id, lowestStart, highestStart := newNodeBlockFilter(t, api, c)

	// Operate: let more than two BlockchainInfo pages (20 each) of blocks commit before polling.
	waitForBlockFilterTestHeight(t, c, highestStart+44)
	first := pollNodeBlockFilter(t, api, id)

	// Verify: every block since creation and none from before it, oldest first, as the node's
	// Block endpoint reports them.
	require.GreaterOrEqual(t, len(first), 45)
	start := nodeBlockHeight(t, c, first[0])
	require.GreaterOrEqual(t, start, lowestStart)
	require.LessOrEqual(t, start, highestStart)
	requireNodeBlockHashes(t, c, start, first)

	// Verify: the next poll continues at the following block.
	next := start + int64(len(first))
	waitForBlockFilterTestHeight(t, c, next)
	second := pollNodeBlockFilter(t, api, id)
	require.NotEmpty(t, second)
	requireNodeBlockHashes(t, c, next, second)
}

func TestFilterAPI_BlockFilterWithoutNotifierReportsBlocksNodePrunedDuringPoll(t *testing.T) {
	// Setup: a node keeping the last 10 blocks, and a filter created once it is producing blocks.
	c := startBlockFilterTestNode(t, 10)
	waitForBlockFilterTestHeight(t, c, 2)
	watermarkClient := &staleStatusClient{Local: c}
	api := evmrpc.NewBlockFilterAPIForTest(t, c, watermarkClient)
	id, lowestStart, highestStart := newNodeBlockFilter(t, api, c)

	// Setup: a status read once the filter's first block exists and before pruning reaches it.
	waitForBlockFilterTestHeight(t, c, highestStart)
	stale, err := c.Status(t.Context())
	require.NoError(t, err)
	require.LessOrEqual(t, stale.SyncInfo.EarliestBlockHeight, lowestStart)

	// Operate: let pruning pass the filter's first block, then poll with the earlier status as its
	// watermark, so only the node's BlockchainInfo can find the blocks gone.
	require.Eventually(t, func() bool {
		status, err := c.Status(t.Context())
		return err == nil && status.SyncInfo.EarliestBlockHeight > highestStart
	}, time.Minute, 20*time.Millisecond)
	watermarkClient.stale = stale
	_, err = api.GetFilterChanges(t.Context(), id)
	require.Nil(t, watermarkClient.stale, "the poll read the earlier status")

	// Verify: the poll names the blocks pruned from the filter's first block on.
	require.Error(t, err)
	match := prunedBlocksErr.FindStringSubmatch(err.Error())
	require.NotNil(t, match, "unexpected error: %v", err)
	start, err := strconv.ParseInt(match[1], 10, 64)
	require.NoError(t, err)
	require.GreaterOrEqual(t, start, lowestStart)
	require.LessOrEqual(t, start, highestStart)
	lastPruned, err := strconv.ParseInt(match[2], 10, 64)
	require.NoError(t, err)
	require.GreaterOrEqual(t, lastPruned, start)

	// Verify: the next poll starts at the block after them, whether it reads blocks or finds that
	// pruning has moved on again.
	res, err := api.GetFilterChanges(t.Context(), id)
	if err != nil {
		match = prunedBlocksErr.FindStringSubmatch(err.Error())
		require.NotNil(t, match, "unexpected error: %v", err)
		require.Equal(t, strconv.FormatInt(lastPruned+1, 10), match[1])
		return
	}
	// Pruning can remove the oldest returned blocks at any commit, so only the newest is looked up.
	hashes := res.([]common.Hash)
	require.NotEmpty(t, hashes)
	require.Equal(t, lastPruned+int64(len(hashes)), nodeBlockHeight(t, c, hashes[len(hashes)-1]))
}
