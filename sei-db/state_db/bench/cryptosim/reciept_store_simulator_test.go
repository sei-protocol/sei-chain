package cryptosim

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestRandomEntryInBlockRange(t *testing.T) {
	ring := newTxHashRing(100)
	crand := NewCannedRandom(42, 1024*1024)

	for i := uint64(1); i <= 50; i++ {
		ring.Push(common.BigToHash(common.Big0), i, common.Address{})
	}

	tests := []struct {
		name     string
		minBlock uint64
		maxBlock uint64
		wantNil  bool
	}{
		{
			name:     "range covers all entries",
			minBlock: 1,
			maxBlock: 50,
			wantNil:  false,
		},
		{
			name:     "range covers recent entries only",
			minBlock: 40,
			maxBlock: 50,
			wantNil:  false,
		},
		{
			name:     "range covers old entries only",
			minBlock: 1,
			maxBlock: 10,
			wantNil:  false,
		},
		{
			name:     "range outside all entries",
			minBlock: 100,
			maxBlock: 200,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := ring.RandomEntryInBlockRange(crand, tt.minBlock, tt.maxBlock)
			if tt.wantNil && entry != nil {
				t.Fatalf("expected nil, got block %d", entry.blockNumber)
			}
			if !tt.wantNil && entry == nil {
				t.Fatalf("expected entry in [%d,%d], got nil", tt.minBlock, tt.maxBlock)
			}
			if entry != nil && (entry.blockNumber < tt.minBlock || entry.blockNumber > tt.maxBlock) {
				t.Fatalf("expected block in [%d,%d], got %d", tt.minBlock, tt.maxBlock, entry.blockNumber)
			}
		})
	}
}
