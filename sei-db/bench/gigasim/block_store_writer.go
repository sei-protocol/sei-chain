package gigasim

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	autobahn "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmutils "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// blockStoreWriter persists each SuperBlock and its synthetic QC atomically.
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

	pending []*autobahn.Block

	// Breaks the block store write into the records it writes. Owned by the generator, which closes it
	// once the flush behind them is done, so the phases subdivide its whole write_block phase.
	phases *metrics.PhaseTimer

	metrics *GigasimMetrics
}

// newBlockStoreWriter prepares to append to the block ledger, resuming after whatever it already holds.
func newBlockStoreWriter(
	store *blockstore.Store,
	config *GigasimConfig,
	gigasimMetrics *GigasimMetrics,
	phases *metrics.PhaseTimer,
) *blockStoreWriter {
	rng := tmutils.TestRngFromSeed(config.Seed)
	w := &blockStoreWriter{
		store:   store,
		config:  config,
		rng:     rng,
		lane:    autobahn.GenLaneID(rng),
		phases:  phases,
		metrics: gigasimMetrics,
	}
	if status, ok := store.Status().Get(); ok {
		w.laneBlockNumber = autobahn.BlockNumber(status.NextBlock-1) * autobahn.BlockNumber(config.LaneBlocksPerSuperblock)
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

// writeBlock persists a block with its QC after finalizing prior blocks.
func (w *blockStoreWriter) writeBlock(number int64, payload [][]byte) error {
	w.phases.SetPhase("encode_block")
	block, err := w.buildBlock(payload)
	if err != nil {
		return err
	}
	w.parentHash = block.Header().Hash()
	w.laneBlockNumber = block.Header().Next()
	w.pending = append(w.pending, block)
	if len(w.pending) < w.config.LaneBlocksPerSuperblock {
		return nil
	}
	index := autobahn.RoadIndex((number-1)/int64(w.config.LaneBlocksPerSuperblock) + 1) //nolint:gosec // generated heights are positive
	if err := w.finalizeBlocksBelow(index); err != nil {
		return err
	}
	w.phases.SetPhase("write_block")
	qc := autobahn.GenFullCommitQCAt(w.rng, index, len(w.pending))
	headers := make([]*autobahn.BlockHeader, len(w.pending))
	for i, laneBlock := range w.pending {
		headers[i] = laneBlock.Header()
	}
	qc = autobahn.NewFullCommitQC(qc.QC(), headers)
	if err := w.store.WriteBlock(autobahn.NewRoadBlock(qc, w.pending)); err != nil {
		return fmt.Errorf("failed to write SuperBlock %d: %w", index, err)
	}
	for _, laneBlock := range w.pending {
		w.metrics.ReportStoreBytesWritten(storeBlockDB, payloadBytes(laneBlock.Payload().Txs()))
	}
	w.metrics.ReportQCWritten()
	w.pending = w.pending[:0]
	return nil
}

// maxLedgerEntries is the most payload entries one ledger block holds.
const maxLedgerEntries = int(autobahn.MaxTxsPerBlock)

// ledgerPayload draws the payload one block stores: TransactionsPerBlock transactions of
// BytesPerTransaction bytes each, packed into at most maxLedgerEntries entries so that a block may carry
// more transactions than a ledger block has entries. The total size is the same however they are packed.
func ledgerPayload(rand *crand.CannedRandom, config *GigasimConfig) [][]byte {
	transactions := config.TransactionsPerBlock
	entries := min(transactions, maxLedgerEntries)
	perEntry, remainder := transactions/entries, transactions%entries

	payload := make([][]byte, entries)
	for i := range payload {
		packed := perEntry
		if i < remainder {
			packed++
		}
		payload[i] = rand.Bytes(packed * config.BytesPerTransaction)
	}
	return payload
}

// payloadBytes is the size of the transactions a block carries into the ledger.
func payloadBytes(payload [][]byte) int64 {
	var total int64
	for _, tx := range payload {
		total += int64(len(tx))
	}
	return total
}

// finalizeBlocksBelow writes the AppProposal and the AppQC over it covering every block below next
// that no AppQC covers yet, which is the app-level finalization a real node's ledger carries.
//
// They are what bounds the block store's recovery scan: the scan walks back only as far as the newest
// AppQC, so a ledger holding none is read in full on every open. Both are synthetic, as the QCs are,
// since the store verifies no signatures on the write path.
//
// The frontier is read from the store rather than tracked here, so it cannot drift from the one the
// store enforces contiguity against.
func (w *blockStoreWriter) finalizeBlocksBelow(next autobahn.RoadIndex) error {
	status, ok := w.store.Status().Get()
	if !ok || next <= status.NextAppQC {
		// Nothing is finalizable before the first QC, and a frontier already at next covers everything.
		return nil
	}
	for index := status.NextAppQC; index < next; index++ {
		w.phases.SetPhase("write_app_qc")
		appProposal := autobahn.GenAppProposalAt(w.rng, index)
		if err := w.store.WriteAppProposal(appProposal); err != nil {
			return fmt.Errorf("failed to write AppProposal %d: %w", index, err)
		}
		if err := w.store.WriteAppQC(autobahn.GenAppQCFor(w.rng, appProposal)); err != nil {
			return fmt.Errorf("failed to write AppQC %d: %w", index, err)
		}
	}
	return nil
}

// buildBlock wraps a payload as the block the store persists, chained onto the previous one. Every block,
// setup's included, carries TransactionsPerBlock transactions, however its payload packs them.
func (w *blockStoreWriter) buildBlock(payload [][]byte) (*autobahn.Block, error) {
	gas := uint64(w.config.gasUsedBy(w.config.TransactionsPerBlock)) //nolint:gosec // validation keeps the gas positive
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
