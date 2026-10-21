package evmrpc

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

// Block tags as eth_newFilter decodes them into filters.FilterCriteria.
var (
	latestTag    = big.NewInt(rpc.LatestBlockNumber.Int64())
	safeTag      = big.NewInt(rpc.SafeBlockNumber.Int64())
	finalizedTag = big.NewInt(rpc.FinalizedBlockNumber.Int64())
	pendingTag   = big.NewInt(rpc.PendingBlockNumber.Int64())
)

func TestComputeBlockBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		latest      int64
		earliest    int64
		fromBlock   *big.Int
		toBlock     *big.Int
		wantBegin   int64
		wantEnd     int64
		errContains string
	}{
		{
			name:      "defaults when no bounds provided",
			latest:    100,
			earliest:  0,
			wantBegin: 100,
			wantEnd:   100,
		},
		{
			name:      "from block sets begin",
			latest:    100,
			fromBlock: big.NewInt(80),
			wantBegin: 80,
			wantEnd:   100,
		},
		{
			name:      "to block with no from forces begin to end",
			latest:    100,
			toBlock:   big.NewInt(90),
			wantBegin: 90,
			wantEnd:   90,
		},
		{
			name:        "from before earliest fails",
			latest:      100,
			earliest:    30,
			fromBlock:   big.NewInt(5),
			errContains: "before earliest available block 30",
		},
		{
			name:        "to after latest fails",
			latest:      100,
			earliest:    0,
			toBlock:     big.NewInt(150),
			errContains: "after latest available block 100",
		},
		{
			name:        "to before earliest fails",
			latest:      100,
			earliest:    30,
			toBlock:     big.NewInt(5),
			errContains: "before earliest available block 30",
		},
		{
			name:        "from greater than to fails",
			latest:      100,
			earliest:    0,
			fromBlock:   big.NewInt(40),
			toBlock:     big.NewInt(20),
			errContains: "greater than toBlock",
		},
		{
			name:      "nil from with to collapses to single block",
			latest:    42,
			toBlock:   big.NewInt(21),
			wantBegin: 21,
			wantEnd:   21,
		},
		{
			name:      "pinned from preserves narrowed window start",
			latest:    42,
			fromBlock: big.NewInt(1),
			toBlock:   big.NewInt(21),
			wantBegin: 1,
			wantEnd:   21,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			crit := filters.FilterCriteria{FromBlock: tc.fromBlock, ToBlock: tc.toBlock}
			gotBegin, gotEnd, err := ComputeBlockBounds(tc.latest, tc.earliest, crit)
			if tc.errContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
			require.Equalf(t, tc.wantBegin, gotBegin, "begin mismatch")
			require.Equalf(t, tc.wantEnd, gotEnd, "end mismatch")
		})
	}
}

// TestPollStart pins where a new log filter's polls start: a numeric fromBlock is kept, an
// omitted or tag fromBlock is pinned to the head at creation, and an inverted range is rejected.
func TestPollStart(t *testing.T) {
	t.Parallel()

	const head = int64(100)
	blockHash := common.HexToHash("0x01")
	tests := []struct {
		name        string
		crit        filters.FilterCriteria
		want        int64
		errContains string
	}{
		{name: "omitted fromBlock starts at the head", want: head},
		{name: "latest fromBlock starts at the head", crit: filters.FilterCriteria{FromBlock: latestTag}, want: head},
		{name: "safe fromBlock starts at the head", crit: filters.FilterCriteria{FromBlock: safeTag}, want: head},
		{name: "finalized fromBlock starts at the head", crit: filters.FilterCriteria{FromBlock: finalizedTag}, want: head},
		{name: "pending fromBlock starts at the head", crit: filters.FilterCriteria{FromBlock: pendingTag}, want: head},
		{name: "tag toBlock leaves the start at the head", crit: filters.FilterCriteria{ToBlock: latestTag}, want: head},
		{name: "past fromBlock starts there", crit: filters.FilterCriteria{FromBlock: big.NewInt(40)}, want: 40},
		{name: "future fromBlock starts there", crit: filters.FilterCriteria{FromBlock: big.NewInt(150)}, want: 150},
		{name: "numeric fromBlock with tag toBlock starts at fromBlock", crit: filters.FilterCriteria{FromBlock: big.NewInt(40), ToBlock: latestTag}, want: 40},
		{name: "omitted fromBlock defaults to a toBlock below the head", crit: filters.FilterCriteria{ToBlock: big.NewInt(40)}, want: 40},
		{name: "omitted fromBlock with a future toBlock starts at the head", crit: filters.FilterCriteria{ToBlock: big.NewInt(150)}, want: head},
		{
			name:        "fromBlock above toBlock is rejected",
			crit:        filters.FilterCriteria{FromBlock: big.NewInt(50), ToBlock: big.NewInt(40)},
			errContains: "requested fromBlock 50 is greater than toBlock 40",
		},
		{
			name:        "tag fromBlock above toBlock is rejected",
			crit:        filters.FilterCriteria{FromBlock: latestTag, ToBlock: big.NewInt(40)},
			errContains: "requested fromBlock 100 is greater than toBlock 40",
		},
		{name: "blockHash filter starts at zero", crit: filters.FilterCriteria{BlockHash: &blockHash}, want: 0},
	}

	fetcher := &LogFetcher{watermarks: newHeightTestWatermarks(newHeightTestClient(0, 1, head), head)}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := fetcher.pollStart(t.Context(), tc.crit)
			if tc.errContains != "" {
				require.ErrorContains(t, err, tc.errContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestPollWindow pins the blocks a log filter's poll scans: from its cursor to the head or a
// lower numeric toBlock, with tags following the head, and at most maxBlock blocks.
func TestPollWindow(t *testing.T) {
	t.Parallel()

	const head = int64(10)
	tests := []struct {
		name     string
		toBlock  *big.Int
		cursor   int64
		maxBlock int64
		wantFrom int64
		wantTo   int64
		wantOK   bool
	}{
		{name: "omitted toBlock scans from the cursor to the head", cursor: 5, maxBlock: 100, wantFrom: 5, wantTo: head, wantOK: true},
		{name: "latest toBlock follows the head", toBlock: latestTag, cursor: 5, maxBlock: 100, wantFrom: 5, wantTo: head, wantOK: true},
		{name: "safe toBlock follows the head", toBlock: safeTag, cursor: 5, maxBlock: 100, wantFrom: 5, wantTo: head, wantOK: true},
		{name: "finalized toBlock follows the head", toBlock: finalizedTag, cursor: 5, maxBlock: 100, wantFrom: 5, wantTo: head, wantOK: true},
		{name: "pending toBlock follows the head", toBlock: pendingTag, cursor: 5, maxBlock: 100, wantFrom: 5, wantTo: head, wantOK: true},
		{name: "toBlock below the head ends the range", toBlock: big.NewInt(7), cursor: 5, maxBlock: 100, wantFrom: 5, wantTo: 7, wantOK: true},
		{name: "toBlock above the head waits for the head", toBlock: big.NewInt(20), cursor: 5, maxBlock: 100, wantFrom: 5, wantTo: head, wantOK: true},
		{name: "cursor at toBlock still scans toBlock", toBlock: big.NewInt(7), cursor: 7, maxBlock: 100, wantFrom: 7, wantTo: 7, wantOK: true},
		{name: "cursor past toBlock scans nothing", toBlock: big.NewInt(7), cursor: 8, maxBlock: 100},
		{name: "cursor at the head scans the head", cursor: head, maxBlock: 100, wantFrom: head, wantTo: head, wantOK: true},
		{name: "cursor past the head scans nothing", cursor: head + 1, maxBlock: 100},
		{name: "range wider than maxBlock is capped", cursor: 5, maxBlock: 3, wantFrom: 5, wantTo: 7, wantOK: true},
		{name: "range of exactly maxBlock is not capped", cursor: 5, maxBlock: 6, wantFrom: 5, wantTo: head, wantOK: true},
		{name: "unset maxBlock leaves the range uncapped", cursor: 5, wantFrom: 5, wantTo: head, wantOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			from, to, ok := pollWindow(tc.toBlock, tc.cursor, head, tc.maxBlock)
			require.Equal(t, tc.wantOK, ok)
			if !tc.wantOK {
				return
			}
			require.Equal(t, tc.wantFrom, from, "from")
			require.Equal(t, tc.wantTo, to, "to")
		})
	}
}

// TestLogsExhausted pins when a log filter can match no further logs: once its cursor passes a
// numeric toBlock or its blockHash block has been polled, and never for a tag toBlock.
func TestLogsExhausted(t *testing.T) {
	t.Parallel()

	const farCursor = int64(1) << 40
	blockHash := common.HexToHash("0x01")
	tests := []struct {
		name   string
		crit   filters.FilterCriteria
		cursor int64
		want   bool
	}{
		{name: "omitted toBlock is never exhausted", cursor: farCursor},
		{name: "latest toBlock is never exhausted", crit: filters.FilterCriteria{ToBlock: latestTag}, cursor: farCursor},
		{name: "safe toBlock is never exhausted", crit: filters.FilterCriteria{ToBlock: safeTag}, cursor: farCursor},
		{name: "finalized toBlock is never exhausted", crit: filters.FilterCriteria{ToBlock: finalizedTag}, cursor: farCursor},
		{name: "pending toBlock is never exhausted", crit: filters.FilterCriteria{ToBlock: pendingTag}, cursor: farCursor},
		{name: "cursor at toBlock still has toBlock to scan", crit: filters.FilterCriteria{ToBlock: big.NewInt(7)}, cursor: 7},
		{name: "cursor past toBlock is exhausted", crit: filters.FilterCriteria{ToBlock: big.NewInt(7)}, cursor: 8, want: true},
		{name: "blockHash filter before its poll", crit: filters.FilterCriteria{BlockHash: &blockHash}, cursor: 0},
		{name: "blockHash filter after its poll", crit: filters.FilterCriteria{BlockHash: &blockHash}, cursor: 1, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, logsExhausted(tc.crit, tc.cursor))
		})
	}
}

// TestMergeSortedLogs covers the limit-aware k-way merge: with limit == 0 it
// merges every batch in global (block, index) order, and with a positive limit
// it stops early after collecting that many logs while still returning the
// globally-smallest prefix.
func TestMergeSortedLogs(t *testing.T) {
	t.Parallel()

	mkLog := func(block uint64, idx uint) *ethtypes.Log {
		return &ethtypes.Log{BlockNumber: block, Index: idx}
	}
	// Three pre-sorted batches that interleave when merged. Flattened global
	// order is (1,0)(1,1)(2,0)(2,1)(3,0)(3,1)(4,0)(5,0).
	batches := [][]*ethtypes.Log{
		{mkLog(1, 0), mkLog(2, 1), mkLog(4, 0)},
		{mkLog(1, 1), mkLog(3, 0), mkLog(5, 0)},
		{mkLog(2, 0), mkLog(3, 1)},
	}
	type key struct {
		block uint64
		idx   uint
	}
	wantOrder := []key{{1, 0}, {1, 1}, {2, 0}, {2, 1}, {3, 0}, {3, 1}, {4, 0}, {5, 0}}

	f := &LogFetcher{}

	assertPrefix := func(t *testing.T, res []*ethtypes.Log, n int) {
		t.Helper()
		require.Len(t, res, n)
		for i := range n {
			require.Equalf(t, wantOrder[i].block, res[i].BlockNumber, "log %d block", i)
			require.Equalf(t, wantOrder[i].idx, res[i].Index, "log %d index", i)
		}
	}

	t.Run("no limit merges everything in order", func(t *testing.T) {
		t.Parallel()
		assertPrefix(t, f.mergeSortedLogs(batches, 0), len(wantOrder))
	})

	t.Run("limit truncates to global prefix", func(t *testing.T) {
		t.Parallel()
		res := f.mergeSortedLogs(batches, 3)
		assertPrefix(t, res, 3)
		// Allocation is bounded to the limit, not the total batch size.
		require.Equal(t, 3, cap(res))
	})

	t.Run("limit at least total returns everything", func(t *testing.T) {
		t.Parallel()
		assertPrefix(t, f.mergeSortedLogs(batches, int64(len(wantOrder)+5)), len(wantOrder))
	})

	t.Run("empty batches return empty slice", func(t *testing.T) {
		t.Parallel()
		res := f.mergeSortedLogs([][]*ethtypes.Log{{}, nil}, 10)
		require.NotNil(t, res)
		require.Empty(t, res)
	})
}
