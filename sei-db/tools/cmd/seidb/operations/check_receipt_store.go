package operations

import (
	"errors"
	"fmt"
	"io"
	"math/bits"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
)

const (
	// receiptKeyPrefix is the first byte of every receipt key. It must equal
	// x/evm/types.ReceiptKeyPrefix.
	receiptKeyPrefix = 0x0b
	// receiptKeyLen is the length of a receipt key: the prefix and a 32-byte
	// transaction hash.
	receiptKeyLen = 33
	// receiptBlockNumberField and receiptTxIndexField are the field numbers of
	// block_number and transaction_index in x/evm's Receipt message.
	receiptBlockNumberField = 7
	receiptTxIndexField     = 8
	// blockChunkBits is the number of blocks one chunk of a blockSet covers.
	blockChunkBits = 1 << 22
)

// CheckReceiptStoreCmd is the seidb subcommand that reports which blocks a
// pebble receipt store holds receipts for.
func CheckReceiptStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-receipt-store",
		Short: "Report which blocks a pebble receipt store holds receipts for, and where it holds none",
		Long: "Reads every receipt in --dir and reports the store's encoding and version markers, the blocks its " +
			"receipts belong to, and the longest runs of blocks inside its version range that hold no receipt. " +
			"Blocks are taken from each receipt, not from the version it is stored at, so legacy receipts that " +
			"v6.3 moved into the store count for their own blocks. The store is opened read-only; stop the node " +
			"that owns it first.\n\n" +
			"Exits non-zero when the store holds a store other than receipts or no receipts at all, has a " +
			"changelog it has not applied, holds an entry that does not decode as a receipt or a receipt stored " +
			"below its own block, or has a run of at least --hole-threshold blocks without receipts. Blocks " +
			"without EVM transactions leave runs too, so raise the threshold for a quiet chain.",
		Args:         cobra.NoArgs,
		RunE:         executeCheckReceiptStore,
		SilenceUsage: true,
		// main prints the returned error.
		SilenceErrors: true,
	}
	cmd.Flags().String("dir", "",
		"Receipt store directory: <home>/data/receipt.db, or <home>/data/ledger/receipt/pebbledb")
	cmd.Flags().Int("longest-runs", 10, "Number of the longest runs of blocks without receipts to list")
	cmd.Flags().Uint64("hole-threshold", 10_000,
		"Length in blocks from which a run without receipts counts as a possible hole; 0 counts none")
	_ = cmd.MarkFlagRequired("dir")
	return cmd
}

func executeCheckReceiptStore(cmd *cobra.Command, _ []string) error {
	dir, _ := cmd.Flags().GetString("dir")
	listed, _ := cmd.Flags().GetInt("longest-runs")
	holeThreshold, _ := cmd.Flags().GetUint64("hole-threshold")
	if listed < 0 {
		return errors.New("--longest-runs must be at least 0")
	}

	tally := &receiptTally{}
	scan, err := mvcc.Scan(dir, mvcc.ScanOptions{
		StoreKey: receiptStoreKey,
		Visit:    tally.add,
		// Kept off stdout, which carries the report.
		Progress: func(r mvcc.ScanReport) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "entries read: %d\n", r.Entries)
		},
	})
	if err != nil {
		return fmt.Errorf("check receipt store: %w", err)
	}
	span, searched := receiptSpan(scan.DBInfo, &tally.blocks)
	runs := findEmptyRuns(&tally.blocks, span, searched, listed, holeThreshold)

	out := cmd.OutOrStdout()
	printReceiptTally(out, scan, tally)
	printEmptyRuns(out, runs, holeThreshold)
	problems := receiptStoreProblems(scan, tally, runs, holeThreshold)
	if len(problems) > 0 {
		return fmt.Errorf("receipt store %s: %s", dir, strings.Join(problems, "; "))
	}
	_, _ = fmt.Fprintln(out, "no problems found")
	return nil
}

// receiptTally accumulates what a scan finds in a receipt store.
type receiptTally struct {
	// blocks holds every block a decoded receipt belongs to.
	blocks blockSet
	// receipts counts entries that decode as receipts, and undecodable the
	// receipt keys whose value does not.
	receipts    int64
	undecodable int64
	// otherKeys counts entries whose key is not a receipt key.
	otherKeys int64
	// storedAboveBlock and storedBelowBlock count receipts stored at a version
	// above or below their own block.
	storedAboveBlock int64
	storedBelowBlock int64
}

// add records one entry of a receipt store.
func (t *receiptTally) add(key []byte, version int64, value []byte) error {
	if !isReceiptKey(key) {
		t.otherKeys++
		return nil
	}
	block, err := receiptBlockNumber(value)
	if err != nil || block == 0 {
		t.undecodable++
		return nil
	}
	t.receipts++
	t.blocks.add(block)
	stored := uint64(version) //nolint:gosec // MVCC versions are non-negative
	switch {
	case stored > block:
		t.storedAboveBlock++
	case stored < block:
		t.storedBelowBlock++
	}
	return nil
}

// isReceiptKey reports whether key, relative to the receipt store, is a
// receipt's key.
func isReceiptKey(key []byte) bool {
	return len(key) == receiptKeyLen && key[0] == receiptKeyPrefix
}

// receiptBlockNumber returns the block_number field of a marshaled x/evm
// Receipt, 0 when the field is absent.
func receiptBlockNumber(receipt []byte) (uint64, error) {
	return receiptVarint(receipt, receiptBlockNumberField)
}

// receiptVarint returns the varint field num of a marshaled x/evm Receipt, 0
// when the field is absent.
func receiptVarint(receipt []byte, num protowire.Number) (uint64, error) {
	for len(receipt) > 0 {
		field, typ, n := protowire.ConsumeTag(receipt)
		if n < 0 {
			return 0, protowire.ParseError(n)
		}
		receipt = receipt[n:]
		if field == num && typ == protowire.VarintType {
			value, n := protowire.ConsumeVarint(receipt)
			if n < 0 {
				return 0, protowire.ParseError(n)
			}
			return value, nil
		}
		n = protowire.ConsumeFieldValue(field, typ, receipt)
		if n < 0 {
			return 0, protowire.ParseError(n)
		}
		receipt = receipt[n:]
	}
	return 0, nil
}

// receiptSpan returns the blocks a store's version markers claim, from its
// earliest marker, or its first receipt when that marker is unset, to its
// latest marker. It reports false when there is nothing to search.
func receiptSpan(db mvcc.DBInfo, blocks *blockSet) (blockRun, bool) {
	from := uint64(db.Earliest) //nolint:gosec // version markers are non-negative
	if from == 0 {
		if blocks.count == 0 {
			return blockRun{}, false
		}
		from = blocks.first
	}
	to := max(uint64(db.Latest), blocks.last) //nolint:gosec // version markers are non-negative
	if to < from {
		return blockRun{}, false
	}
	return blockRun{from: from, to: to}, true
}

// emptyRunReport is what a search for runs of blocks without receipts found.
type emptyRunReport struct {
	// span is the blocks searched, and searched is false when there were none.
	span     blockRun
	searched bool
	// count is the number of runs found, and longest lists the longest of
	// them, longest first.
	count   int64
	longest []blockRun
	// holes counts the runs at least the hole threshold long.
	holes int64
}

// findEmptyRuns searches span for runs of blocks the set does not hold,
// keeping the listed longest and counting those at least holeThreshold long.
func findEmptyRuns(blocks *blockSet, span blockRun, searched bool, listed int, holeThreshold uint64) emptyRunReport {
	report := emptyRunReport{span: span, searched: searched}
	if !searched {
		return report
	}
	blocks.emptyRuns(span, func(run blockRun) {
		report.count++
		report.longest = keepLongest(report.longest, run, listed)
		if holeThreshold > 0 && run.len() >= holeThreshold {
			report.holes++
		}
	})
	return report
}

// receiptStoreProblems lists what makes a scanned receipt store fail the check.
func receiptStoreProblems(scan mvcc.ScanReport, tally *receiptTally, runs emptyRunReport, holeThreshold uint64) []string {
	var problems []string
	if others := slices.DeleteFunc(slices.Clone(scan.StoreKeys), func(k string) bool { return k == receiptStoreKey }); len(others) > 0 {
		problems = append(problems, fmt.Sprintf("holds stores other than %q: %q", receiptStoreKey, others))
	}
	if scan.ChangelogAhead > 0 {
		problems = append(problems, fmt.Sprintf(
			"its changelog holds version %d above its latest version %d; open the store once to replay it, then check again",
			scan.ChangelogAhead, scan.Latest))
	}
	if tally.receipts == 0 {
		problems = append(problems, "holds no receipts")
	}
	if tally.undecodable > 0 {
		problems = append(problems, fmt.Sprintf("%d entries do not decode as receipts with a block number", tally.undecodable))
	}
	if tally.storedBelowBlock > 0 {
		problems = append(problems, fmt.Sprintf("%d receipts are stored below their own block", tally.storedBelowBlock))
	}
	if runs.holes > 0 {
		problems = append(problems, fmt.Sprintf("%d runs of at least %d blocks hold no receipts", runs.holes, holeThreshold))
	}
	return problems
}

func printReceiptTally(w io.Writer, scan mvcc.ScanReport, tally *receiptTally) {
	_, _ = fmt.Fprintf(w, "store %s: %s\n", scan.Dir, describeDB(scan.DBInfo))
	if tally.blocks.count > 0 {
		_, _ = fmt.Fprintf(w, "receipts: %d for %d blocks, blocks %d-%d\n",
			tally.receipts, tally.blocks.count, tally.blocks.first, tally.blocks.last)
	} else {
		_, _ = fmt.Fprintln(w, "receipts: 0")
	}
	_, _ = fmt.Fprintf(w, "receipts stored above their own block, as v6.3 stored the legacy receipts it moved: %d\n",
		tally.storedAboveBlock)
	if scan.Earliest > 0 {
		_, _ = fmt.Fprintf(w, "blocks below the earliest version holding receipts, served by transaction hash only: %d\n",
			tally.blocks.countBelow(uint64(scan.Earliest))) //nolint:gosec // version markers are non-negative
	}
	if tally.otherKeys > 0 {
		_, _ = fmt.Fprintf(w, "entries under keys that are not receipt keys: %d\n", tally.otherKeys)
	}
	if scan.Deletions > 0 {
		_, _ = fmt.Fprintf(w, "deletion entries: %d\n", scan.Deletions)
	}
}

func printEmptyRuns(w io.Writer, runs emptyRunReport, holeThreshold uint64) {
	if !runs.searched {
		return
	}
	if runs.count == 0 {
		_, _ = fmt.Fprintf(w, "blocks %d-%d all hold receipts\n", runs.span.from, runs.span.to)
		return
	}
	_, _ = fmt.Fprintf(w, "runs of blocks without receipts in %d-%d: %d\n", runs.span.from, runs.span.to, runs.count)
	for _, run := range runs.longest {
		hole := ""
		if holeThreshold > 0 && run.len() >= holeThreshold {
			hole = ", a possible hole"
		}
		_, _ = fmt.Fprintf(w, "  %d-%d: %s%s\n", run.from, run.to, countBlocks(run.len()), hole)
	}
	if holeThreshold > 0 {
		_, _ = fmt.Fprintf(w, "runs of at least %d blocks without receipts: %d\n", holeThreshold, runs.holes)
	}
}

// blockRun is an inclusive run of blocks.
type blockRun struct {
	from, to uint64
}

// countBlocks renders a number of blocks.
func countBlocks(n uint64) string {
	if n == 1 {
		return "1 block"
	}
	return fmt.Sprintf("%d blocks", n)
}

func (r blockRun) len() uint64 {
	return r.to - r.from + 1
}

// keepLongest inserts run into longest, which keeps at most n runs, longest
// first and in block order among equals.
func keepLongest(longest []blockRun, run blockRun, n int) []blockRun {
	i := sort.Search(len(longest), func(i int) bool { return longest[i].len() < run.len() })
	if i >= n {
		return longest
	}
	longest = slices.Insert(longest, i, run)
	if len(longest) > n {
		longest = longest[:n]
	}
	return longest
}

// blockSet is a set of block numbers.
type blockSet struct {
	chunks map[uint64][]uint64
	// count is the number of blocks held, and first and last the lowest and
	// highest of them.
	count       int64
	first, last uint64
}

func (s *blockSet) add(block uint64) {
	if s.chunks == nil {
		s.chunks = make(map[uint64][]uint64)
	}
	chunk := s.chunks[block/blockChunkBits]
	if chunk == nil {
		chunk = make([]uint64, blockChunkBits/64)
		s.chunks[block/blockChunkBits] = chunk
	}
	offset := block % blockChunkBits
	bit := uint64(1) << (offset % 64)
	if chunk[offset/64]&bit != 0 {
		return
	}
	chunk[offset/64] |= bit
	if s.count == 0 || block < s.first {
		s.first = block
	}
	s.last = max(s.last, block)
	s.count++
}

// word returns the 64 bits of the set starting at block, a multiple of 64.
func (s *blockSet) word(block uint64) uint64 {
	chunk := s.chunks[block/blockChunkBits]
	if chunk == nil {
		return 0
	}
	return chunk[(block%blockChunkBits)/64]
}

func (s *blockSet) has(block uint64) bool {
	return s.word(block-block%64)&(uint64(1)<<(block%64)) != 0
}

// countBelow returns how many of the blocks the set holds are below limit.
func (s *blockSet) countBelow(limit uint64) int64 {
	var n int64
	for index, chunk := range s.chunks {
		first := index * blockChunkBits
		for _, w := range chunk {
			if first >= limit {
				break
			}
			if limit-first < 64 {
				w &= uint64(1)<<(limit-first) - 1
			}
			n += int64(bits.OnesCount64(w))
			first += 64
		}
	}
	return n
}

// emptyRuns calls fn with every maximal run of blocks in span that the set
// does not hold, in block order.
func (s *blockSet) emptyRuns(span blockRun, fn func(blockRun)) {
	var run blockRun
	inRun := false
	open := func(block uint64) {
		if !inRun {
			run.from, inRun = block, true
		}
	}
	shut := func(block uint64) {
		if inRun {
			run.to, inRun = block-1, false
			fn(run)
		}
	}
	for block := span.from; block <= span.to; {
		if block%64 == 0 && span.to-block >= 63 {
			switch s.word(block) {
			case 0:
				open(block)
				block += 64
				continue
			case ^uint64(0):
				shut(block)
				block += 64
				continue
			}
		}
		if s.has(block) {
			shut(block)
		} else {
			open(block)
		}
		block++
	}
	if inRun {
		run.to = span.to
		fn(run)
	}
}
