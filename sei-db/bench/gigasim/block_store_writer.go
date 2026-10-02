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
		// Every block the open QC range covers is written by now, which is what the app-level records
		// require, so they are written before the next range opens.
		if err := w.finalizeBlocksBelow(globalNumber); err != nil {
			return err
		}
		if err := w.writeCoveringQC(globalNumber); err != nil {
			return err
		}
	}

	w.phases.SetPhase("encode_block")
	block, err := w.buildBlock(payload)
	if err != nil {
		return err
	}

	w.phases.SetPhase("write_block")
	if err := w.store.WriteBlock(globalNumber, block); err != nil {
		return fmt.Errorf("failed to write block %d to the block store: %w", number, err)
	}
	w.metrics.ReportStoreBytesWritten(storeBlockDB, payloadBytes(payload))
	w.parentHash = block.Header().Hash()
	w.laneBlockNumber = block.Header().Next()
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
func (w *blockStoreWriter) finalizeBlocksBelow(next autobahn.GlobalBlockNumber) error {
	status, ok := w.store.Status().Get()
	if !ok || next <= status.NextAppQC {
		// Nothing is finalizable before the first QC, and a frontier already at next covers everything.
		return nil
	}
	first := status.NextAppQC

	w.phases.SetPhase("write_app_qc")
	appProposal := autobahn.GenAppProposalRange(w.rng, first, next)
	if err := w.store.WriteAppProposal(appProposal); err != nil {
		return fmt.Errorf("failed to write the AppProposal covering [%d, %d): %w", first, next, err)
	}
	if err := w.store.WriteAppQC(autobahn.GenAppQCFor(w.rng, appProposal)); err != nil {
		return fmt.Errorf("failed to write the AppQC covering [%d, %d): %w", first, next, err)
	}
	return nil
}

// writeCoveringQC writes the QC finalizing the superblock that starts at first.
func (w *blockStoreWriter) writeCoveringQC(first autobahn.GlobalBlockNumber) error {
	next := first + autobahn.GlobalBlockNumber(w.config.LaneBlocksPerSuperblock) //nolint:gosec // G115 - validation keeps the count positive

	w.phases.SetPhase("write_qc")
	qc, err := superblockQC(w.rng, first, next)
	if err != nil {
		return err
	}
	if err := w.store.WriteQC(qc); err != nil {
		return fmt.Errorf("failed to write the QC covering [%d, %d): %w", first, next, err)
	}
	w.qcNext = next
	w.metrics.ReportQCWritten()
	return nil
}

// superblockQC returns a QC covering [first, next). Its lane ranges sum to that length.
func superblockQC(rng tmutils.Rng, first, next autobahn.GlobalBlockNumber) (*autobahn.FullCommitQC, error) {
	span := next - first
	if span == 0 {
		return nil, fmt.Errorf("QC covering [%d, %d) contains no blocks", first, next)
	}
	maxPerLane := autobahn.GlobalBlockNumber(autobahn.MaxLaneRangeInProposal)
	laneCount := int((span + maxPerLane - 1) / maxPerLane) //nolint:gosec // span is bounded by the superblock size

	committee, keys := autobahn.GenCommittee(rng, laneCount)
	epoch := autobahn.NewEpoch(autobahn.GenEpochIndex(rng), autobahn.OpenRoadRange(), time.Time{}, committee, first)
	view := autobahn.ViewSpec{ConsensusSpec: autobahn.ConsensusSpec{Epoch: epoch}}

	lanes := committee.Lanes()
	laneQCs := make(map[autobahn.LaneID]*autobahn.LaneQC, laneCount)
	var sig *autobahn.Signature
	remaining := span
	for i := range laneCount {
		length := min(remaining, maxPerLane)
		remaining -= length
		lane := lanes.At(i)
		header := autobahn.NewBlock(
			lane,
			autobahn.BlockNumber(length-1), //nolint:gosec // length is at most MaxLaneRangeInProposal
			autobahn.GenBlockHeaderHash(rng),
			autobahn.GenPayload(rng),
		).Header()
		signed := autobahn.Sign(keys[0], autobahn.NewLaneVote(header))
		sig = signed.Sig()
		laneQCs[lane] = autobahn.NewLaneQC([]*autobahn.Signed[*autobahn.LaneVote]{signed})
	}

	proposal, err := autobahn.NewProposalForTesting(committee, view, time.Now(), laneQCs, sig)
	if err != nil {
		return nil, fmt.Errorf("failed to build the QC covering [%d, %d): %w", first, next, err)
	}
	if got := proposal.Proposal().Msg().GlobalRange(); got.First != first || got.Next != next {
		return nil, fmt.Errorf("QC covers [%d, %d), want [%d, %d)", got.First, got.Next, first, next)
	}
	commit := autobahn.NewCommitQC([]*autobahn.Signed[*autobahn.CommitVote]{
		autobahn.Sign(keys[0], autobahn.NewCommitVote(proposal.Proposal().Msg())),
	})
	headers := tmutils.GenSliceN(rng, int(span), autobahn.GenBlockHeader) //nolint:gosec // span is bounded by the superblock size
	return autobahn.NewFullCommitQC(commit, headers), nil
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
