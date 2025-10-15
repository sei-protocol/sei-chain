package evmrpc

import (
	"math/big"
	"testing"

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
