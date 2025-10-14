package evmrpc_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/sei-protocol/sei-chain/evmrpc"
)

func TestNormalizeBlockBounds(t *testing.T) {
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
			name:      "begin clamped to earliest",
			latest:    100,
			earliest:  30,
			fromBlock: big.NewInt(5),
			wantBegin: 30,
			wantEnd:   100,
		},
		{
			name:      "end clamped to latest",
			latest:    100,
			toBlock:   big.NewInt(150),
			wantBegin: 100,
			wantEnd:   100,
		},
		{
			name:      "end clamped to earliest",
			latest:    100,
			earliest:  30,
			toBlock:   big.NewInt(5),
			wantBegin: 30,
			wantEnd:   30,
		},
		{
			name:         "last height surpasses end",
			latest:       100,
			fromBlock:    big.NewInt(20),
			toBlock:      big.NewInt(40),
			lastToHeight: 60,
			wantBegin:    60,
			wantEnd:      40,
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
			gotBegin, gotEnd := evmrpc.NormalizeBlockBounds(tc.latest, tc.earliest, tc.lastToHeight, crit)
			if gotBegin != tc.wantBegin {
				t.Fatalf("begin mismatch: got %d, want %d", gotBegin, tc.wantBegin)
			}
			if gotEnd != tc.wantEnd {
				t.Fatalf("end mismatch: got %d, want %d", gotEnd, tc.wantEnd)
			}
		})
	}
}
