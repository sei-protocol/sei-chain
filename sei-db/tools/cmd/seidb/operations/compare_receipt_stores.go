package operations

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"os"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
)

// CompareReceiptStoresCmd is the seidb subcommand that compares the receipts
// two pebble receipt stores hold, block by block.
func CompareReceiptStoresCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compare-receipt-stores",
		Short: "Compare the receipts two pebble receipt stores hold, block by block",
		Long: "Reads the receipts in --dir-a and --dir-b side by side and reports, for every block, whether the two " +
			"stores hold the same receipts for it, receipts only one of them holds, or receipts that differ. A " +
			"receipt is read as a node reads it, at its store's latest version, and counted for the block it names. " +
			"Runs of blocks in one state are listed as ranges; a block neither store holds receipts for does not " +
			"break a run. --from and --to limit the blocks compared, and --block lists every receipt of one block in " +
			"both stores with the fields that differ. The stores are keyed by transaction hash, so every comparison " +
			"reads both in full. Both are opened read-only; stop the nodes that own them first. A store whose " +
			"changelog holds versions it has not applied is refused until it is opened once to replay them.\n\n" +
			"Exits non-zero when the stores differ for any block compared, or when either holds an entry that does " +
			"not decode as a receipt of a block at or below the version it is stored at.",
		Args:         cobra.NoArgs,
		RunE:         executeCompareReceiptStores,
		SilenceUsage: true,
		// main prints the returned error.
		SilenceErrors: true,
	}
	cmd.Flags().String("dir-a", "", "First receipt store directory")
	cmd.Flags().String("dir-b", "", "Second receipt store directory")
	cmd.Flags().Uint64("from", 0, "Lowest block to compare")
	cmd.Flags().Uint64("to", 0, "Highest block to compare; 0 compares every block from --from up")
	cmd.Flags().Uint64("block", 0, "Block whose receipts in both stores to list")
	cmd.Flags().Int("examples", 10, "Number of differing receipts to list")
	cmd.Flags().Int("runs", 50, "Number of runs of blocks in one state to list")
	_ = cmd.MarkFlagRequired("dir-a")
	_ = cmd.MarkFlagRequired("dir-b")
	return cmd
}

func executeCompareReceiptStores(cmd *cobra.Command, _ []string) error {
	dirA, _ := cmd.Flags().GetString("dir-a")
	dirB, _ := cmd.Flags().GetString("dir-b")
	from, _ := cmd.Flags().GetUint64("from")
	to, _ := cmd.Flags().GetUint64("to")
	focus, _ := cmd.Flags().GetUint64("block")
	examples, _ := cmd.Flags().GetInt("examples")
	listedRuns, _ := cmd.Flags().GetInt("runs")
	if examples < 0 || listedRuns < 0 {
		return errors.New("--examples and --runs must be at least 0")
	}
	if to > 0 && to < from {
		return errors.New("--to must not be below --from")
	}
	if err := requireTwoStores(dirA, dirB); err != nil {
		return err
	}

	a, err := mvcc.OpenStoreReader(dirA, receiptStoreKey)
	if err != nil {
		return fmt.Errorf("store A %s: %w", dirA, err)
	}
	defer func() { _ = a.Close() }()
	b, err := mvcc.OpenStoreReader(dirB, receiptStoreKey)
	if err != nil {
		return fmt.Errorf("store B %s: %w", dirB, err)
	}
	defer func() { _ = b.Close() }()

	c := &receiptComparison{from: from, to: to, focus: focus, exampleLimit: examples}
	err = c.walk(a, b, func(keys int64) {
		// Kept off stdout, which carries the report.
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "keys read: %d\n", keys)
	})
	if err != nil {
		return fmt.Errorf("compare receipt stores: %w", err)
	}
	states := c.blockStates(listedRuns)

	out := cmd.OutOrStdout()
	printComparison(out, a.Info(), b.Info(), c, states)
	if problems := c.problems(states); len(problems) > 0 {
		return fmt.Errorf("receipt stores %s and %s: %s", dirA, dirB, strings.Join(problems, "; "))
	}
	_, _ = fmt.Fprintln(out, "the stores hold the same receipts for every block compared")
	return nil
}

// requireTwoStores fails unless dirA and dirB are two different existing
// directories.
func requireTwoStores(dirA, dirB string) error {
	infoA, err := os.Stat(dirA)
	if err != nil {
		return fmt.Errorf("store A: %w", err)
	}
	infoB, err := os.Stat(dirB)
	if err != nil {
		return fmt.Errorf("store B: %w", err)
	}
	if os.SameFile(infoA, infoB) {
		return fmt.Errorf("store A and store B are the same directory %s", dirA)
	}
	return nil
}

// storeSide is one of the two stores a comparison reads.
type storeSide int

const (
	storeA storeSide = iota
	storeB
)

func (s storeSide) String() string {
	if s == storeA {
		return "A"
	}
	return "B"
}

func (s storeSide) other() storeSide {
	return 1 - s
}

// blockState is how two receipt stores compare for one block.
type blockState int

const (
	sameReceipts blockState = iota
	receiptsOnlyInA
	receiptsOnlyInB
	receiptsMissingFromOne
	receiptsDiffer
	blockStateCount
)

var blockStateNames = [blockStateCount]string{
	"same receipts",
	"receipts only in A",
	"receipts only in B",
	"some receipts missing from one store",
	"receipts that differ",
}

// receiptComparison is what comparing two receipt stores found.
type receiptComparison struct {
	// from and to bound the blocks compared; to is 0 when there is no upper
	// bound.
	from, to uint64
	// focus is the block whose receipts are listed, 0 for none.
	focus uint64
	// exampleLimit is the number of differing receipts to keep as examples.
	exampleLimit int

	// held is the blocks each store holds a receipt for, lacking the blocks for
	// which a store lacks a receipt the other holds, and changed the blocks for
	// which the stores hold one receipt in two forms.
	held    [2]blockSet
	lacking [2]blockSet
	changed blockSet

	// same, only and differ count the receipts both stores hold alike, the
	// receipts only one holds, and the receipts both hold in two forms.
	same   int64
	only   [2]int64
	differ int64
	// sameAtOtherVersion counts the receipts both hold alike at two versions.
	sameAtOtherVersion int64
	undecodable        [2]int64
	otherKeys          [2]int64

	examples []receiptDiff
	focused  []receiptDiff
}

// receiptDiff is how the two stores hold one receipt.
type receiptDiff struct {
	hash    []byte
	block   uint64
	txIndex uint64
	state   string
}

// walk reads both stores side by side in key order and records every key.
func (c *receiptComparison) walk(a, b *mvcc.StoreReader, progress func(keys int64)) error {
	hasA, err := a.Next()
	if err != nil {
		return fmt.Errorf("store A: %w", err)
	}
	hasB, err := b.Next()
	if err != nil {
		return fmt.Errorf("store B: %w", err)
	}
	for keys := int64(1); hasA || hasB; keys++ {
		order := compareCursors(hasA, hasB, a.Key(), b.Key())
		switch {
		case order < 0:
			c.recordOne(storeA, a.Key(), a.Value(), a.Version())
		case order > 0:
			c.recordOne(storeB, b.Key(), b.Value(), b.Version())
		default:
			c.recordBoth(a.Key(), a.Value(), b.Value(), a.Version(), b.Version())
		}
		if order <= 0 {
			if hasA, err = a.Next(); err != nil {
				return fmt.Errorf("store A: %w", err)
			}
		}
		if order >= 0 {
			if hasB, err = b.Next(); err != nil {
				return fmt.Errorf("store B: %w", err)
			}
		}
		if keys%1_000_000 == 0 {
			progress(keys)
		}
	}
	return nil
}

// compareCursors orders the keys two readers are on, an exhausted reader
// sorting after every key.
func compareCursors(hasA, hasB bool, keyA, keyB []byte) int {
	switch {
	case !hasB:
		return -1
	case !hasA:
		return 1
	default:
		return bytes.Compare(keyA, keyB)
	}
}

// recordOne records a key only one store holds.
func (c *receiptComparison) recordOne(side storeSide, key, value []byte, version int64) {
	if !isReceiptKey(key) {
		c.otherKeys[side]++
		return
	}
	block, ok := c.decodeBlock(side, value, version)
	if !ok {
		return
	}
	if c.wantsDetail(block, block, true) {
		c.keepDetail(receiptDiff{
			hash: key[1:], block: block, txIndex: receiptTxIndex(value), state: "only in " + side.String(),
		}, block, block, true)
	}
	if !c.compares(block) {
		return
	}
	c.held[side].add(block)
	c.lacking[side.other()].add(block)
	c.only[side]++
}

// recordBoth records a key both stores hold.
func (c *receiptComparison) recordBoth(key, valueA, valueB []byte, versionA, versionB int64) {
	if !isReceiptKey(key) {
		c.otherKeys[storeA]++
		c.otherKeys[storeB]++
		return
	}
	blockA, okA := c.decodeBlock(storeA, valueA, versionA)
	blockB, okB := c.decodeBlock(storeB, valueB, versionB)
	if !okA || !okB {
		return
	}
	differs := !bytes.Equal(valueA, valueB)
	if c.wantsDetail(blockA, blockB, differs) {
		state := "same"
		if differs {
			state = describeReceiptDiff(valueA, valueB)
		}
		c.keepDetail(receiptDiff{hash: key[1:], block: blockA, txIndex: receiptTxIndex(valueA), state: state},
			blockA, blockB, differs)
	}
	comparedA, comparedB := c.compares(blockA), c.compares(blockB)
	if !comparedA && !comparedB {
		return
	}
	if comparedA {
		c.held[storeA].add(blockA)
	}
	if comparedB {
		c.held[storeB].add(blockB)
	}
	if !differs {
		c.same++
		if versionA != versionB {
			c.sameAtOtherVersion++
		}
		return
	}
	c.differ++
	if comparedA {
		c.changed.add(blockA)
	}
	if comparedB {
		c.changed.add(blockB)
	}
}

// decodeBlock returns the block a receipt from side names, counting the
// receipt as undecodable when it names none, or names a block above the
// version it is stored at.
func (c *receiptComparison) decodeBlock(side storeSide, value []byte, version int64) (uint64, bool) {
	block, err := receiptBlockNumber(value)
	if err != nil || block == 0 || block > uint64(version) { //nolint:gosec // MVCC versions are non-negative
		c.undecodable[side]++
		return 0, false
	}
	return block, true
}

// compares reports whether block lies within the blocks compared.
func (c *receiptComparison) compares(block uint64) bool {
	return block >= c.from && (c.to == 0 || block <= c.to)
}

// wantsDetail reports whether a receipt naming blockA in store A and blockB in
// store B is to be listed, as a receipt of the focus block or as an example of
// a difference.
func (c *receiptComparison) wantsDetail(blockA, blockB uint64, differs bool) bool {
	return c.isFocus(blockA, blockB) || c.wantsExample(blockA, blockB, differs)
}

func (c *receiptComparison) isFocus(blockA, blockB uint64) bool {
	return c.focus != 0 && (blockA == c.focus || blockB == c.focus)
}

func (c *receiptComparison) wantsExample(blockA, blockB uint64, differs bool) bool {
	return differs && len(c.examples) < c.exampleLimit && (c.compares(blockA) || c.compares(blockB))
}

// keepDetail adds diff to the focus block's receipts, to the examples, or to
// both, as it belongs.
func (c *receiptComparison) keepDetail(diff receiptDiff, blockA, blockB uint64, differs bool) {
	if c.isFocus(blockA, blockB) {
		c.focused = append(c.focused, diff)
	}
	if c.wantsExample(blockA, blockB, differs) {
		c.examples = append(c.examples, diff)
	}
}

// receiptTxIndex returns a marshaled receipt's transaction_index, 0 when it
// does not decode.
func receiptTxIndex(receipt []byte) uint64 {
	index, _ := receiptVarint(receipt, receiptTxIndexField)
	return index
}

// stateRun is a run of blocks in one state. Blocks neither store holds
// receipts for may lie inside it.
type stateRun struct {
	state    blockState
	from, to uint64
	// blocks counts the blocks in the run that either store holds receipts for.
	blocks uint64
}

// blockStateReport is how the blocks either store holds receipts for compare.
type blockStateReport struct {
	// blocks counts the blocks in each state.
	blocks [blockStateCount]uint64
	// count is the number of runs, and runs lists the first of them.
	count int64
	runs  []stateRun
}

// differing returns the number of blocks for which the stores differ.
func (r blockStateReport) differing() uint64 {
	var n uint64
	for state := receiptsOnlyInA; state < blockStateCount; state++ {
		n += r.blocks[state]
	}
	return n
}

// blockStates classifies every block either store holds a compared receipt
// for, keeping the first listed runs of blocks in one state.
func (c *receiptComparison) blockStates(listed int) blockStateReport {
	var report blockStateReport
	span, ok := c.heldSpan()
	if !ok {
		return report
	}
	var run stateRun
	inRun := false
	for base := span.from - span.from%64; base <= span.to; base += 64 {
		held := [2]uint64{c.held[storeA].word(base), c.held[storeB].word(base)}
		if held[storeA]|held[storeB] == 0 {
			continue
		}
		missing := c.lacking[storeA].word(base) | c.lacking[storeB].word(base)
		changed := c.changed.word(base)
		for pending := held[storeA] | held[storeB]; pending != 0; pending &= pending - 1 {
			offset := bits.TrailingZeros64(pending)
			block := base + uint64(offset) //nolint:gosec // a trailing-zero count is below 64
			state := classifyBlock(held, missing, changed, uint64(1)<<offset)
			report.blocks[state]++
			if inRun && run.state == state {
				run.to = block
				run.blocks++
				continue
			}
			if inRun {
				report.addRun(run, listed)
			}
			run, inRun = stateRun{state: state, from: block, to: block, blocks: 1}, true
		}
	}
	if inRun {
		report.addRun(run, listed)
	}
	return report
}

// classifyBlock returns the state of the block at bit in words of the
// comparison's block sets.
func classifyBlock(held [2]uint64, missing, changed, bit uint64) blockState {
	switch {
	case held[storeA]&bit == 0:
		return receiptsOnlyInB
	case held[storeB]&bit == 0:
		return receiptsOnlyInA
	case missing&bit != 0:
		return receiptsMissingFromOne
	case changed&bit != 0:
		return receiptsDiffer
	default:
		return sameReceipts
	}
}

func (r *blockStateReport) addRun(run stateRun, listed int) {
	r.count++
	if len(r.runs) < listed {
		r.runs = append(r.runs, run)
	}
}

// heldSpan returns the blocks from the lowest to the highest that either
// store holds a compared receipt for, and false when there are none.
func (c *receiptComparison) heldSpan() (blockRun, bool) {
	var span blockRun
	found := false
	for _, set := range c.held {
		if set.count == 0 {
			continue
		}
		if !found || set.first < span.from {
			span.from = set.first
		}
		if !found || set.last > span.to {
			span.to = set.last
		}
		found = true
	}
	return span, found
}

// problems lists what makes the comparison fail.
func (c *receiptComparison) problems(states blockStateReport) []string {
	var problems []string
	if n := states.differing(); n > 0 {
		problems = append(problems, "they differ for "+countBlocks(n))
	}
	for _, side := range []storeSide{storeA, storeB} {
		if c.undecodable[side] > 0 {
			problems = append(problems, fmt.Sprintf(
				"%d entries in %s do not decode as receipts of a block at or below their version",
				c.undecodable[side], side))
		}
	}
	return problems
}

func printComparison(w io.Writer, infoA, infoB mvcc.DBInfo, c *receiptComparison, states blockStateReport) {
	_, _ = fmt.Fprintf(w, "store A %s: %s\n", infoA.Dir, describeDB(infoA))
	_, _ = fmt.Fprintf(w, "store B %s: %s\n", infoB.Dir, describeDB(infoB))
	if c.from > 0 || c.to > 0 {
		to := "up"
		if c.to > 0 {
			to = fmt.Sprint(c.to)
		}
		_, _ = fmt.Fprintf(w, "blocks compared: %d to %s\n", c.from, to)
	}
	_, _ = fmt.Fprintf(w, "receipts: %d the same in both, %d only in A, %d only in B, %d that differ\n",
		c.same, c.only[storeA], c.only[storeB], c.differ)
	if c.sameAtOtherVersion > 0 {
		_, _ = fmt.Fprintf(w, "receipts both stores hold alike at different versions: %d\n", c.sameAtOtherVersion)
	}
	for _, side := range []storeSide{storeA, storeB} {
		if c.otherKeys[side] > 0 {
			_, _ = fmt.Fprintf(w, "entries in %s under keys that are not receipt keys: %d\n", side, c.otherKeys[side])
		}
	}
	printBlockStates(w, states)
	printReceiptDiffs(w, fmt.Sprintf("differing receipts, first %d:", len(c.examples)), c.examples, true)
	if c.focus == 0 {
		return
	}
	if len(c.focused) == 0 {
		_, _ = fmt.Fprintf(w, "receipts of block %d: neither store holds any\n", c.focus)
		return
	}
	sort.Slice(c.focused, func(i, j int) bool {
		if c.focused[i].txIndex != c.focused[j].txIndex {
			return c.focused[i].txIndex < c.focused[j].txIndex
		}
		return bytes.Compare(c.focused[i].hash, c.focused[j].hash) < 0
	})
	printReceiptDiffs(w, fmt.Sprintf("receipts of block %d:", c.focus), c.focused, false)
}

func printBlockStates(w io.Writer, states blockStateReport) {
	if states.count == 0 {
		_, _ = fmt.Fprintln(w, "neither store holds receipts for the blocks compared")
		return
	}
	counts := make([]string, 0, blockStateCount)
	for state, name := range blockStateNames {
		counts = append(counts, fmt.Sprintf("%d with %s", states.blocks[state], name))
	}
	_, _ = fmt.Fprintf(w, "blocks: %s\n", strings.Join(counts, ", "))
	_, _ = fmt.Fprintf(w, "runs of blocks in one state, not broken by blocks neither store holds receipts for: %d\n",
		states.count)
	for _, run := range states.runs {
		_, _ = fmt.Fprintf(w, "  %d-%d: %s (%s)\n", run.from, run.to, blockStateNames[run.state], countBlocks(run.blocks))
	}
	if more := states.count - int64(len(states.runs)); more > 0 {
		_, _ = fmt.Fprintf(w, "  and %d more; narrow --from and --to to list them\n", more)
	}
}

func printReceiptDiffs(w io.Writer, heading string, diffs []receiptDiff, withBlock bool) {
	if len(diffs) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, heading)
	for _, d := range diffs {
		block := ""
		if withBlock {
			block = fmt.Sprintf("block %d ", d.block)
		}
		_, _ = fmt.Fprintf(w, "  %stx %d 0x%s: %s\n", block, d.txIndex, hex.EncodeToString(d.hash), d.state)
	}
}

// fieldKind is how a field of x/evm's Receipt message is shown.
type fieldKind int

const (
	varintField fieldKind = iota
	stringField
	bytesField
	repeatedField
)

// receiptFieldInfo names a field of x/evm's Receipt message.
type receiptFieldInfo struct {
	name string
	kind fieldKind
}

// receiptFieldInfos describes x/evm's Receipt message by field number.
var receiptFieldInfos = map[protowire.Number]receiptFieldInfo{
	1:  {"tx_type", varintField},
	2:  {"cumulative_gas_used", varintField},
	3:  {"contract_address", stringField},
	4:  {"tx_hash_hex", stringField},
	5:  {"gas_used", varintField},
	6:  {"effective_gas_price", varintField},
	7:  {"block_number", varintField},
	8:  {"transaction_index", varintField},
	9:  {"status", varintField},
	10: {"from", stringField},
	11: {"to", stringField},
	12: {"vm_error", stringField},
	13: {"logs", repeatedField},
	14: {"logsBloom", bytesField},
	15: {"pre_execution_failure", varintField},
}

// fieldValues is every occurrence of one field in a marshaled message.
type fieldValues struct {
	// raw is the occurrences' tags and values, concatenated.
	raw []byte
	// payloads holds each occurrence's value: a varint's bytes, or the content
	// of a length-delimited field.
	payloads [][]byte
}

// describeReceiptDiff names the fields in which two marshaled receipts differ,
// with both values where they are short.
func describeReceiptDiff(a, b []byte) string {
	fieldsA, errA := messageFields(a)
	fieldsB, errB := messageFields(b)
	if errA != nil || errB != nil {
		return "differs, and does not decode as a receipt"
	}
	nums := make([]protowire.Number, 0, len(fieldsA)+len(fieldsB))
	for num := range fieldsA {
		nums = append(nums, num)
	}
	for num := range fieldsB {
		if _, inA := fieldsA[num]; !inA {
			nums = append(nums, num)
		}
	}
	slices.Sort(nums)
	parts := make([]string, 0, len(nums))
	for _, num := range nums {
		if !bytes.Equal(fieldsA[num].rawBytes(), fieldsB[num].rawBytes()) {
			parts = append(parts, describeFieldDiff(num, fieldsA[num], fieldsB[num]))
		}
	}
	return "differs: " + strings.Join(parts, "; ")
}

// messageFields splits a marshaled message into its fields by number.
func messageFields(b []byte) (map[protowire.Number]*fieldValues, error) {
	fields := make(map[protowire.Number]*fieldValues)
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		m := protowire.ConsumeFieldValue(num, typ, b[n:])
		if m < 0 {
			return nil, protowire.ParseError(m)
		}
		field := fields[num]
		if field == nil {
			field = &fieldValues{}
			fields[num] = field
		}
		field.raw = append(field.raw, b[:n+m]...)
		payload := b[n : n+m]
		if typ == protowire.BytesType {
			payload, _ = protowire.ConsumeBytes(payload)
		}
		field.payloads = append(field.payloads, payload)
		b = b[n+m:]
	}
	return fields, nil
}

// rawBytes returns the field's encoding, nil for an absent field.
func (f *fieldValues) rawBytes() []byte {
	if f == nil {
		return nil
	}
	return f.raw
}

// last returns the field's last value, which proto3 reads for a singular
// field, and nil for an absent one.
func (f *fieldValues) last() []byte {
	if f == nil || len(f.payloads) == 0 {
		return nil
	}
	return f.payloads[len(f.payloads)-1]
}

func (f *fieldValues) count() int {
	if f == nil {
		return 0
	}
	return len(f.payloads)
}

// describeFieldDiff names field num, which differs between a and b, with both
// values where they are short.
func describeFieldDiff(num protowire.Number, a, b *fieldValues) string {
	info, known := receiptFieldInfos[num]
	if !known {
		return fmt.Sprintf("field %d", num)
	}
	switch info.kind {
	case varintField:
		valueA, _ := protowire.ConsumeVarint(a.last())
		valueB, _ := protowire.ConsumeVarint(b.last())
		return fmt.Sprintf("%s %d vs %d", info.name, valueA, valueB)
	case stringField:
		return fmt.Sprintf("%s %q vs %q", info.name, shortText(a.last()), shortText(b.last()))
	case repeatedField:
		if a.count() != b.count() {
			return fmt.Sprintf("%s (%d vs %d)", info.name, a.count(), b.count())
		}
		return info.name
	default:
		if len(a.last()) != len(b.last()) {
			return fmt.Sprintf("%s (%d vs %d bytes)", info.name, len(a.last()), len(b.last()))
		}
		return info.name
	}
}

// shortText returns b as text, cut to its first 64 characters.
func shortText(b []byte) string {
	const limit = 64
	if utf8.RuneCount(b) <= limit {
		return string(b)
	}
	return string([]rune(string(b))[:limit]) + "…"
}
