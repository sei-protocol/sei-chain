package gigasim

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	autobahn "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmutils "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// gasPerTransaction is the gas a plain transfer is charged, used to fill a block's gas totals.
const gasPerTransaction = 21_000

// blockStoreWriter persists blocks to the block ledger in the order the store's contract requires: the
// QC covering a range of blocks is written before any block in it, so a crash can only ever leave a QC
// without its blocks and never a block without its QC.
//
// The QCs are synthetic: the store verifies no signatures on the write path, so a QC only has to carry
// the range it covers.
type blockStoreWriter struct {
	store  *blockstore.Store
	config *GigasimConfig
	rng    tmutils.Rng

	// The single lane every simulated block belongs to. A real chain spreads blocks over one lane per
	// validator; the block store holds them the same way either way.
	lane autobahn.LaneID

	// The header hash of the last block written, which the next block names as its parent. Chaining
	// them keeps every block's hash distinct, which the store requires of the alias it indexes by.
	parentHash autobahn.BlockHeaderHash

	// The lane-local number of the next block, which advances with the global number because there is
	// only one lane.
	laneBlockNumber autobahn.BlockNumber

	// The exclusive end of the range the newest QC covers. A block at or above this needs a new QC
	// written before it.
	qcNext autobahn.GlobalBlockNumber

	metrics *GigasimMetrics
}

// newBlockStoreWriter prepares to append to the block ledger, resuming after whatever it already holds.
func newBlockStoreWriter(
	store *blockstore.Store,
	config *GigasimConfig,
	metrics *GigasimMetrics,
) *blockStoreWriter {
	rng := tmutils.TestRngFromSeed(config.Seed)
	w := &blockStoreWriter{
		store:   store,
		config:  config,
		rng:     rng,
		lane:    autobahn.GenLaneID(rng),
		metrics: metrics,
	}
	if status, ok := store.Status().Get(); ok {
		w.qcNext = status.NextQC
		w.laneBlockNumber = autobahn.BlockNumber(status.NextBlock)
	}
	return w
}

// nextBlockNumber returns the height the block store will accept next, reporting false for a store
// that holds nothing and so will accept any height as its first.
func (w *blockStoreWriter) nextBlockNumber() (int64, bool) {
	status, ok := w.store.Status().Get()
	if !ok {
		return 0, false
	}
	return int64(status.NextBlock), true //nolint:gosec // block heights are bounded well below the conversion limit
}

// writeBlock persists one block, first writing the QC that covers it when the previous QC's range has
// run out.
func (w *blockStoreWriter) writeBlock(number int64, payload [][]byte) error {
	//nolint:gosec // G115 - block heights are non-negative and bounded by the run's length
	globalNumber := autobahn.GlobalBlockNumber(number)

	if globalNumber >= w.qcNext {
		if err := w.writeCoveringQC(globalNumber); err != nil {
			return err
		}
	}

	block, err := w.buildBlock(payload)
	if err != nil {
		return err
	}

	w.metrics.SetGeneratorPhase("write_block")
	if err := w.store.WriteBlock(globalNumber, block); err != nil {
		return fmt.Errorf("failed to write block %d to the block store: %w", number, err)
	}
	w.parentHash = block.Header().Hash()
	w.laneBlockNumber = block.Header().Next()
	return nil
}

// writeCoveringQC writes the QC finalizing the range that starts at first.
func (w *blockStoreWriter) writeCoveringQC(first autobahn.GlobalBlockNumber) error {
	next := first + autobahn.GlobalBlockNumber(w.config.BlocksPerQc)

	w.metrics.SetGeneratorPhase("write_qc")
	if err := w.store.WriteQC(autobahn.GenFullCommitQCRange(w.rng, first, next)); err != nil {
		return fmt.Errorf("failed to write the QC covering [%d, %d): %w", first, next, err)
	}
	w.qcNext = next
	w.metrics.ReportQCWritten()
	return nil
}

// buildBlock wraps a payload as the block the store persists, chained onto the previous one.
func (w *blockStoreWriter) buildBlock(payload [][]byte) (*autobahn.Block, error) {
	gas := uint64(len(payload)) * gasPerTransaction
	built, err := autobahn.PayloadBuilder{
		CreatedAt:         time.Now(),
		TotalGasWanted:    gas,
		TotalGasEstimated: gas,
		Txs:               payload,
	}.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build the block payload: %w", err)
	}
	return autobahn.NewBlock(w.lane, w.laneBlockNumber, w.parentHash, built), nil
}

// Flush pushes the block ledger's buffered writes to disk.
func (w *blockStoreWriter) Flush() error {
	if err := w.store.Flush(); err != nil {
		return fmt.Errorf("failed to flush the block store: %w", err)
	}
	return nil
}
