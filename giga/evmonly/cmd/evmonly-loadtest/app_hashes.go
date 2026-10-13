package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
)

// reportExecutionHashes reports to appHashBuilder the hashes of block height that the EVM-only application reports
// in a node: its block hash and its receipt hash. The load test has neither, so the block hash is derived from the
// height and the receipt hash is empty.
func reportExecutionHashes(ctx context.Context, appHashBuilder builder.AppHashBuilder, height uint64) error {
	var preimage [8]byte
	binary.BigEndian.PutUint64(preimage[:], height)
	if err := appHashBuilder.ReportBlockHash(ctx, height, sha256.Sum256(preimage[:])); err != nil {
		return fmt.Errorf("report the block hash of block %d: %w", height, err)
	}
	if err := appHashBuilder.ReportReceiptHash(ctx, height, [32]byte{}); err != nil {
		return fmt.Errorf("report the receipt hash of block %d: %w", height, err)
	}
	return nil
}
