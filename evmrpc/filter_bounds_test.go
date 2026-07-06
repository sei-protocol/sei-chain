package evmrpc

import (
	"math/big"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/stretchr/testify/require"
)

func TestComputeBlockBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		latest       int64
		earliest     int64
		lastToHeight int64
		fromBlock    *big.Int
		toBlock      *big.Int
		wantBegin    int64
		wantEnd      int64
		errContains  string
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
			name:         "last height advances begin",
			latest:       100,
			lastToHeight: 60,
			fromBlock:    big.NewInt(20),
			wantBegin:    60,
			wantEnd:      100,
		},
		{
			name:         "last height surpasses end returns empty range",
			latest:       100,
			fromBlock:    big.NewInt(20),
			toBlock:      big.NewInt(40),
			lastToHeight: 60,
			wantBegin:    60,
			wantEnd:      40,
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
			name:         "last height below begin no change",
			latest:       100,
			fromBlock:    big.NewInt(80),
			lastToHeight: 80,
			wantBegin:    80,
			wantEnd:      100,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			crit := filters.FilterCriteria{FromBlock: tc.fromBlock, ToBlock: tc.toBlock}
			gotBegin, gotEnd, err := ComputeBlockBounds(tc.latest, tc.earliest, tc.lastToHeight, crit)
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
