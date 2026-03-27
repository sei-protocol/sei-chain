package cryptosim

import (
	"math/rand"
	"testing"
)

func TestSelectReceiptReadBlockRanges(t *testing.T) {
	tests := []struct {
		name              string
		earliestBlock     int64
		latestBlock       int64
		hotWindowBlocks   uint64
		cacheWindowBlocks uint64
		coldReadRatio     float64
		minBlock          int64
		maxBlock          int64
	}{
		{
			name:              "hot reads stay near tip",
			earliestBlock:     1,
			latestBlock:       10_000,
			hotWindowBlocks:   500,
			cacheWindowBlocks: 1_000,
			coldReadRatio:     0,
			minBlock:          9_501,
			maxBlock:          10_000,
		},
		{
			name:              "cold reads stay outside cache",
			earliestBlock:     1,
			latestBlock:       10_000,
			hotWindowBlocks:   500,
			cacheWindowBlocks: 1_000,
			coldReadRatio:     1,
			minBlock:          1,
			maxBlock:          9_000,
		},
		{
			name:              "short chains fall back to hot range",
			earliestBlock:     1,
			latestBlock:       700,
			hotWindowBlocks:   500,
			cacheWindowBlocks: 1_000,
			coldReadRatio:     1,
			minBlock:          201,
			maxBlock:          700,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic test RNG

			for i := 0; i < 1_000; i++ {
				block := selectReceiptReadBlock(
					rng,
					tt.earliestBlock,
					tt.latestBlock,
					tt.hotWindowBlocks,
					tt.cacheWindowBlocks,
					tt.coldReadRatio,
					3,
				)
				if block < tt.minBlock || block > tt.maxBlock {
					t.Fatalf("expected block in [%d,%d], got %d", tt.minBlock, tt.maxBlock, block)
				}
			}
		})
	}
}
