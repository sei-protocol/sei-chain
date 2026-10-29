package evmrpc

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// blockFilterTestClient serves BlockchainInfo the way sei-tendermint does: at most 20 metas
// per call, newest first, clamped to the stored [earliest, latest] range.
type blockFilterTestClient struct {
	*heightTestClient
}

func blockFilterTestHash(height int64) common.Hash {
	return common.BigToHash(big.NewInt(height))
}

func (c *blockFilterTestClient) BlockchainInfo(_ context.Context, minHeight, maxHeight int64) (*coretypes.ResultBlockchainInfo, error) {
	maxHeight = min(maxHeight, c.latest)
	minHeight = max(minHeight, maxHeight-19, c.earliest)
	metas := []*tmtypes.BlockMeta{}
	for h := maxHeight; h >= minHeight; h-- {
		metas = append(metas, &tmtypes.BlockMeta{
			BlockID: tmtypes.BlockID{Hash: blockFilterTestHash(h).Bytes()},
			Header:  tmtypes.Header{Height: h},
		})
	}
	return &coretypes.ResultBlockchainInfo{LastHeight: c.latest, BlockMetas: metas}, nil
}

func newBlockFilterTestAPI(t *testing.T, maxHashes uint64, earliest, latest int64) (*FilterAPI, *blockFilterTestClient) {
	t.Helper()
	c := &blockFilterTestClient{heightTestClient: newHeightTestClient(0, earliest, latest)}
	api := newTestFilterAPIWithClient(t, nil, &FilterConfig{
		timeout:              time.Hour,
		maxFilters:           DefaultMaxFilters,
		maxBlockFilterHashes: maxHashes,
	}, c, c, 1<<40)
	return api, c
}

func heightRange(from, to int64) []common.Hash {
	hashes := []common.Hash{}
	for h := from; h <= to; h++ {
		hashes = append(hashes, blockFilterTestHash(h))
	}
	return hashes
}

func pollBlockFilter(t *testing.T, api *FilterAPI, id ethrpc.ID) []common.Hash {
	t.Helper()
	got, err := api.GetFilterChanges(context.Background(), id)
	require.NoError(t, err)
	return got.([]common.Hash)
}

func TestFilterAPI_BlockFilterWithoutNotifierStartsAfterHead(t *testing.T) {
	// Setup: head at 100 when the filter is created.
	api, c := newBlockFilterTestAPI(t, DefaultMaxBlockFilterHashes, 1, 100)
	id, err := api.NewBlockFilter(context.Background())
	require.NoError(t, err)

	// Verify: no block at or below the creation head is ever returned.
	require.Equal(t, []common.Hash{}, pollBlockFilter(t, api, id))

	c.latest = 103
	require.Equal(t, heightRange(101, 103), pollBlockFilter(t, api, id))
	require.Equal(t, []common.Hash{}, pollBlockFilter(t, api, id))
}

func TestFilterAPI_BlockFilterWithoutNotifierReturnsEveryBlockSinceLastPoll(t *testing.T) {
	// Setup: gaps below, at and above one BlockchainInfo page (20), and far above the event
	// log's 10-item page that the cursor used to skip past.
	api, c := newBlockFilterTestAPI(t, DefaultMaxBlockFilterHashes, 1, 100)
	id, err := api.NewBlockFilter(context.Background())
	require.NoError(t, err)

	next := int64(101)
	for _, gap := range []int64{1, 9, 10, 11, 19, 20, 21, 45, 120} {
		c.latest = next + gap - 1
		// Verify: every block since the last poll, ascending, nothing repeated.
		require.Equal(t, heightRange(next, c.latest), pollBlockFilter(t, api, id), "gap %d", gap)
		next = c.latest + 1
	}
}

func TestFilterAPI_BlockFilterWithoutNotifierCapsOnePollAndResumes(t *testing.T) {
	// Setup: at most 30 hashes per poll, and 50 new blocks.
	api, c := newBlockFilterTestAPI(t, 30, 1, 100)
	id, err := api.NewBlockFilter(context.Background())
	require.NoError(t, err)
	c.latest = 150

	// Verify: the first poll returns the oldest 30 blocks, the next poll the remaining 20.
	require.Equal(t, heightRange(101, 130), pollBlockFilter(t, api, id))
	require.Equal(t, heightRange(131, 150), pollBlockFilter(t, api, id))
	require.Equal(t, []common.Hash{}, pollBlockFilter(t, api, id))
}

func TestFilterAPI_BlockFilterWithoutNotifierSkipsPrunedHeights(t *testing.T) {
	// Setup: blocks 101-110 are pruned before the first poll.
	api, c := newBlockFilterTestAPI(t, DefaultMaxBlockFilterHashes, 1, 100)
	id, err := api.NewBlockFilter(context.Background())
	require.NoError(t, err)
	c.earliest, c.latest = 111, 115

	// Verify: the poll resumes at the earliest block still served.
	require.Equal(t, heightRange(111, 115), pollBlockFilter(t, api, id))
}

func TestFilterAPI_GetFilterLogsRejectsBlockFilter(t *testing.T) {
	api, _ := newBlockFilterTestAPI(t, DefaultMaxBlockFilterHashes, 1, 100)
	id, err := api.NewBlockFilter(context.Background())
	require.NoError(t, err)

	_, err = api.GetFilterLogs(context.Background(), id)
	require.EqualError(t, err, "filter does not exist")
}
