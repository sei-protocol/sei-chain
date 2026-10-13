package gigasim

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
)

// reportExecutionHashes reports to appHashBuilder the hashes of block height that execution reports in a node: its
// block hash and its receipt hash. Gigasim simulates execution, so the block hash is derived from the height and the
// receipt hash is empty.
func reportExecutionHashes(ctx context.Context, appHashBuilder builder.AppHashBuilder, height uint64) error {
	var preimage [8]byte
	binary.BigEndian.PutUint64(preimage[:], height)
	if err := appHashBuilder.ReportBlockHash(ctx, height, sha256.Sum256(preimage[:])); err != nil {
		return fmt.Errorf("failed to report the block hash of block %d: %w", height, err)
	}
	if err := appHashBuilder.ReportReceiptHash(ctx, height, [32]byte{}); err != nil {
		return fmt.Errorf("failed to report the receipt hash of block %d: %w", height, err)
	}
	return nil
}

// awaitAppHash waits until appHashBuilder has published the app hash of block height.
func awaitAppHash(appHashBuilder builder.AppHashBuilder, height uint64) error {
	stop := make(chan struct{})
	defer close(stop)
	published := make(chan uint64)
	newest, err := appHashBuilder.RegisterListener(context.Background(), func(appHash *apphash.AppHashData) {
		select {
		case published <- appHash.BlockHeight():
		case <-stop:
		}
	})
	if err != nil {
		return fmt.Errorf("failed to follow the published app hashes: %w", err)
	}
	if newest != nil && newest.BlockHeight() >= height {
		return nil
	}
	timeout := time.NewTimer(hashWaitTimeout)
	defer timeout.Stop()
	for {
		select {
		case publishedHeight := <-published:
			if publishedHeight >= height {
				return nil
			}
		case <-timeout.C:
			return fmt.Errorf("the app hash of block %d was not published within %s",
				height, hashWaitTimeout)
		}
	}
}
