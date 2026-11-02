package receipthash

import (
	"context"
	"errors"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// A receipt hash reported by a ReceiptHasher.
type reportedReceiptHash struct {
	blockHeight uint64
	receiptHash apphash.ReceiptHash
}

// recordReports returns a report function that sends each receipt hash to the returned channel, which holds up to
// capacity of them.
func recordReports(
	capacity int,
) (func(context.Context, uint64, apphash.ReceiptHash) error, <-chan reportedReceiptHash) {
	reported := make(chan reportedReceiptHash, capacity)
	report := func(_ context.Context, blockHeight uint64, receiptHash apphash.ReceiptHash) error {
		reported <- reportedReceiptHash{blockHeight: blockHeight, receiptHash: receiptHash}
		return nil
	}
	return report, reported
}

// newReceiptHasher starts a ReceiptHasher with the default config and the given buffer size.
func newReceiptHasher(
	t *testing.T,
	bufferSize int,
	report func(context.Context, uint64, apphash.ReceiptHash) error,
) *ReceiptHasher {
	t.Helper()
	config := DefaultReceiptHasherConfig()
	config.BufferSize = bufferSize
	hasher, err := NewReceiptHasher(t.Context(), config, report)
	require.NoError(t, err)
	return hasher
}

func TestReceiptHasherReportsEveryBlockInOrderBeforeClosing(t *testing.T) {
	report, reported := recordReports(3)
	hasher := newReceiptHasher(t, 3, report)

	blocks := [][]*ethtypes.Receipt{specReceipts()[:1], nil, specReceipts()}
	for i, receipts := range blocks {
		blockHeight := uint64(10 + i) //nolint:gosec // small test heights
		require.NoError(t, hasher.Submit(t.Context(), blockHeight, receipts))
	}
	require.NoError(t, hasher.Close())

	require.Len(t, reported, len(blocks))
	for i, receipts := range blocks {
		blockHeight := uint64(10 + i) //nolint:gosec // small test heights
		want := reportedReceiptHash{blockHeight: blockHeight, receiptHash: receiptHashOf(t, receipts)}
		require.Equal(t, want, <-reported)
	}
}

func TestReceiptHasherStopsOnReceiptsItCannotHash(t *testing.T) {
	report, reported := recordReports(1)
	hasher := newReceiptHasher(t, 1, report)

	require.NoError(t, hasher.Submit(t.Context(), 1, []*ethtypes.Receipt{nil}))

	require.ErrorContains(t, hasher.Close(), "failed to hash the receipts of block 1")
	require.ErrorContains(t, hasher.Submit(t.Context(), 2, nil), "receipt hasher stopped")
	require.Empty(t, reported)
}

func TestReceiptHasherStopsWhenAReportFails(t *testing.T) {
	reportErr := errors.New("builder stopped")
	hasher := newReceiptHasher(t, 1, func(context.Context, uint64, apphash.ReceiptHash) error {
		return reportErr
	})

	require.NoError(t, hasher.Submit(t.Context(), 1, nil))

	err := hasher.Close()
	require.ErrorIs(t, err, reportErr)
	require.ErrorContains(t, err, "failed to report the receipt hash of block 1")
	require.ErrorIs(t, hasher.Submit(t.Context(), 2, nil), reportErr)
}

func TestReceiptHasherSubmitGivesUpWhenItsContextIsDone(t *testing.T) {
	release := make(chan struct{})
	reporting := make(chan struct{}, 1)
	hasher := newReceiptHasher(t, 1, func(context.Context, uint64, apphash.ReceiptHash) error {
		reporting <- struct{}{}
		<-release
		return nil
	})

	// Block 1 holds the hashing goroutine, and block 2 fills the queue behind it.
	require.NoError(t, hasher.Submit(t.Context(), 1, nil))
	<-reporting
	require.NoError(t, hasher.Submit(t.Context(), 2, nil))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, hasher.Submit(ctx, 3, nil), context.Canceled)

	close(release)
	<-reporting
	require.NoError(t, hasher.Close())
}

func TestReceiptHasherRefusesAnInvalidConfig(t *testing.T) {
	report, _ := recordReports(0)
	_, err := NewReceiptHasher(t.Context(), ReceiptHasherConfig{BufferSize: 0}, report)
	require.ErrorContains(t, err, "buffer size must be positive")
}
