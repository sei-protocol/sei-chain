package operations

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/spf13/cobra"
)

// HashLogCmd is the parent "hashlog" command. It groups read-only tools for inspecting the on-disk hash log
// archives produced by the hashlogger, wrapping the reader utilities in sc/hashlog so an operator can pull a
// single block's hashes or diff two archives without writing Go.
func HashLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hashlog",
		Short: "Inspect hash log archives produced by the hashlogger",
		Long: "Read-only tools for hash log archives. Use 'get-block' to print every hash recorded for a " +
			"single block, or 'compare' to find blocks whose hashes differ between two archives.",
	}
	cmd.AddCommand(hashLogGetBlockCmd(), hashLogCompareCmd())
	return cmd
}

func hashLogGetBlockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-block <archive> <block>",
		Short: "Print every hash recorded for a single block in a hash log archive",
		Args:  cobra.ExactArgs(2),
		Run:   executeHashLogGetBlock,
	}
	cmd.PersistentFlags().Bool("json", false, "Emit JSON instead of human-readable text")
	return cmd
}

func executeHashLogGetBlock(cmd *cobra.Command, args []string) {
	archive := args[0]
	block, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		panic(fmt.Errorf("invalid block number %q: %w", args[1], err))
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	logs, err := hashlog.ReadHashForBlock(archive, block)
	if err != nil {
		panic(fmt.Errorf("read hash for block %d: %w", block, err))
	}

	if err := renderGetBlock(cmd.OutOrStdout(), block, logs, asJSON); err != nil {
		panic(err)
	}
}

func hashLogCompareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compare <archive-a> <archive-b>",
		Short: "Compare two hash log archives and report blocks whose hashes differ",
		Args:  cobra.ExactArgs(2),
		Run:   executeHashLogCompare,
	}
	cmd.PersistentFlags().Uint64("low", 0, "Lowest block to compare (inclusive); requires --high")
	cmd.PersistentFlags().Uint64("high", 0, "Highest block to compare (inclusive); requires --low")
	cmd.PersistentFlags().Int("max-diffs", -1, "Maximum number of differing blocks to report, or -1 for all")
	cmd.PersistentFlags().Bool("full", false,
		"Show every column for each differing block (default shows only the columns that differ)")
	cmd.PersistentFlags().Bool("json", false, "Emit JSON instead of human-readable text")
	return cmd
}

func executeHashLogCompare(cmd *cobra.Command, args []string) {
	archiveA := args[0]
	archiveB := args[1]
	maxDiffs, _ := cmd.Flags().GetInt("max-diffs")
	full, _ := cmd.Flags().GetBool("full")
	asJSON, _ := cmd.Flags().GetBool("json")

	result := compareResult{archiveA: archiveA, archiveB: archiveB, maxDiffs: maxDiffs}

	// --low/--high are optional, but must be supplied together: a one-sided range is almost certainly a mistake.
	lowSet := cmd.Flags().Changed("low")
	highSet := cmd.Flags().Changed("high")
	var diffs []*hashlog.HashLogPair
	var err error
	if lowSet || highSet {
		if !lowSet || !highSet {
			panic("Must provide both --low and --high to compare a block range")
		}
		result.ranged = true
		result.low, _ = cmd.Flags().GetUint64("low")
		result.high, _ = cmd.Flags().GetUint64("high")
		diffs, err = hashlog.CompareHashesInRange(archiveA, archiveB, result.low, result.high, maxDiffs)
	} else {
		diffs, err = hashlog.CompareHashes(archiveA, archiveB, maxDiffs)
	}
	if err != nil {
		panic(fmt.Errorf("compare hash archives: %w", err))
	}
	result.diffs = diffs

	if err := renderCompare(cmd.OutOrStdout(), result, asJSON, full); err != nil {
		panic(err)
	}
}

// compareResult bundles everything renderCompare needs: the inputs that shape the human-readable header plus the
// diffs themselves.
type compareResult struct {
	archiveA string
	archiveB string
	ranged   bool
	low      uint64
	high     uint64
	maxDiffs int
	diffs    []*hashlog.HashLogPair
}

// errWriter funnels a sequence of formatted writes through a single retained error, so the many small writes in
// the text renderers below need only one error check at the end rather than one per line.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}

// renderGetBlock writes the hashes recorded for a single block, either as JSON or human-readable text.
func renderGetBlock(w io.Writer, block uint64, logs []*hashlog.HashLog, asJSON bool) error {
	if asJSON {
		return encodeJSON(w, toHashLogJSONSlice(logs, nil))
	}

	ew := &errWriter{w: w}
	if len(logs) == 0 {
		ew.printf("No records for block %d.\n", block)
		return ew.err
	}
	// More than one record means the block was executed multiple times (e.g. the chain rolled back and replayed
	// it); each execution's hashes are reported separately.
	if len(logs) > 1 {
		ew.printf("Block %d has %d records (the block was executed more than once, e.g. after a rollback):\n",
			block, len(logs))
	}
	for i, log := range logs {
		if len(logs) > 1 {
			ew.printf("record %d (block %d):\n", i+1, log.BlockNumber)
		} else {
			ew.printf("block %d:\n", log.BlockNumber)
		}
		writeHashLogText(ew, log, "  ")
	}
	return ew.err
}

// renderCompare writes the result of comparing two archives, either as JSON or human-readable text. The default
// is compact (only the columns that differ); full includes every column for both sides. This applies to both
// text and JSON output.
func renderCompare(w io.Writer, result compareResult, asJSON bool, full bool) error {
	if asJSON {
		out := make([]hashLogPairJSON, 0, len(result.diffs))
		for _, pair := range result.diffs {
			out = append(out, toHashLogPairJSON(pair, full))
		}
		return encodeJSON(w, out)
	}

	ew := &errWriter{w: w}
	ew.printf("Comparing archive A (%s) against archive B (%s)\n", result.archiveA, result.archiveB)
	if result.ranged {
		ew.printf("Restricted to blocks [%d, %d]\n", result.low, result.high)
	}
	if len(result.diffs) == 0 {
		ew.printf("Archives are identical over the compared range.\n")
		return ew.err
	}
	for _, pair := range result.diffs {
		if full {
			writeFullPair(ew, pair)
		} else {
			writeCompactPair(ew, pair)
		}
	}
	ew.printf("%d differing block(s) reported.\n", len(result.diffs))
	// CompareHashes stops as soon as it has collected maxDiffs diffs, so an exactly-full result may be a
	// truncation rather than the true total. Warn so the operator knows to widen the cap if needed.
	if result.maxDiffs >= 0 && len(result.diffs) == result.maxDiffs {
		ew.printf("Output truncated at --max-diffs=%d; there may be more differing blocks.\n", result.maxDiffs)
	}
	return ew.err
}

// writeFullPair renders both sides of a differing block in full: every column of every record. This is the
// behaviour behind --full, and the only sensible rendering when record counts differ (a rollback), since there
// is no single pair of records to diff column-by-column.
func writeFullPair(ew *errWriter, pair *hashlog.HashLogPair) {
	ew.printf("block %d differs:\n", pairBlock(pair))
	ew.printf("  archive A:\n")
	writeHashLogSet(ew, pair.HashesFromA, "    ")
	ew.printf("  archive B:\n")
	writeHashLogSet(ew, pair.HashesFromB, "    ")
}

// writeCompactPair renders only the columns that differ between the two sides. Column-level diffing is only
// well-defined when each side holds exactly one record; when the record counts differ (a rollback re-executed
// the block a different number of times) there is no record pairing to diff, so we report the counts and defer
// to --full for the details.
func writeCompactPair(ew *errWriter, pair *hashlog.HashLogPair) {
	block := pairBlock(pair)
	if len(pair.HashesFromA) != 1 || len(pair.HashesFromB) != 1 {
		ew.printf("block %d differs: %d record(s) in A vs %d in B (use --full to see them)\n",
			block, len(pair.HashesFromA), len(pair.HashesFromB))
		return
	}
	a := pair.HashesFromA[0].Hashes
	b := pair.HashesFromB[0].Hashes
	columns := unionKeys(a, b)
	differing := make([]string, 0, len(columns))
	for _, column := range columns {
		if !bytes.Equal(a[column], b[column]) {
			differing = append(differing, column)
		}
	}
	ew.printf("block %d differs (%d of %d columns):\n", block, len(differing), len(columns))
	for _, column := range differing {
		ew.printf("  %s:\n", column)
		ew.printf("    A: %s\n", hexOrNone(a[column]))
		ew.printf("    B: %s\n", hexOrNone(b[column]))
	}
}

// writeHashLogText renders one record's version (when present) and its hashes, sorted by hash type for stable
// output. A nil hash (the type was registered but not recorded for this block) prints as "<none>".
func writeHashLogText(ew *errWriter, log *hashlog.HashLog, indent string) {
	if log.Version != "" {
		ew.printf("%sversion: %s\n", indent, log.Version)
	}
	for _, hashType := range sortedKeys(log.Hashes) {
		ew.printf("%s%s: %s\n", indent, hashType, hexOrNone(log.Hashes[hashType]))
	}
}

// hexOrNone hex-encodes a hash, or returns "<none>" for a nil hash (the type was registered but not recorded).
func hexOrNone(hash []byte) string {
	if hash == nil {
		return "<none>"
	}
	return hex.EncodeToString(hash)
}

// sortedKeys returns the keys of a hash map in sorted order, for stable output.
func sortedKeys(hashes map[string][]byte) []string {
	keys := make([]string, 0, len(hashes))
	for key := range hashes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// unionKeys returns the sorted union of the keys of two hash maps.
func unionKeys(a map[string][]byte, b map[string][]byte) []string {
	set := make(map[string]struct{}, len(a)+len(b))
	for key := range a {
		set[key] = struct{}{}
	}
	for key := range b {
		set[key] = struct{}{}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// writeHashLogSet renders one side of a comparison, which may hold several records (rollback) or none at all (the
// block is present only in the other archive).
func writeHashLogSet(ew *errWriter, logs []*hashlog.HashLog, indent string) {
	if len(logs) == 0 {
		ew.printf("%s<no records>\n", indent)
		return
	}
	for i, log := range logs {
		if len(logs) > 1 {
			ew.printf("%srecord %d:\n", indent, i+1)
			writeHashLogText(ew, log, indent+"  ")
			continue
		}
		writeHashLogText(ew, log, indent)
	}
}

// pairBlock returns the block number a diff pair refers to. A pair always has at least one populated side, so we
// take the block from whichever side has records.
func pairBlock(pair *hashlog.HashLogPair) uint64 {
	if len(pair.HashesFromA) > 0 {
		return pair.HashesFromA[0].BlockNumber
	}
	if len(pair.HashesFromB) > 0 {
		return pair.HashesFromB[0].BlockNumber
	}
	return 0
}

// hashLogJSON is the JSON shape for a single record. Hashes map to a hex string, or null when the hash was not
// recorded for that type.
type hashLogJSON struct {
	BlockNumber uint64             `json:"block_number"`
	Version     string             `json:"version,omitempty"`
	Hashes      map[string]*string `json:"hashes"`
}

// hashLogPairJSON is the JSON shape for one differing block.
type hashLogPairJSON struct {
	Block       uint64        `json:"block"`
	HashesFromA []hashLogJSON `json:"hashes_from_a"`
	HashesFromB []hashLogJSON `json:"hashes_from_b"`
}

// toHashLogJSON converts a record to its JSON shape. When keep is non-nil, only those columns are emitted (used
// for compact compare output); a nil keep emits every column.
func toHashLogJSON(log *hashlog.HashLog, keep map[string]struct{}) hashLogJSON {
	hashes := make(map[string]*string, len(log.Hashes))
	for hashType, hash := range log.Hashes {
		if keep != nil {
			if _, ok := keep[hashType]; !ok {
				continue
			}
		}
		if hash == nil {
			hashes[hashType] = nil
			continue
		}
		encoded := hex.EncodeToString(hash)
		hashes[hashType] = &encoded
	}
	return hashLogJSON{BlockNumber: log.BlockNumber, Version: log.Version, Hashes: hashes}
}

func toHashLogJSONSlice(logs []*hashlog.HashLog, keep map[string]struct{}) []hashLogJSON {
	out := make([]hashLogJSON, 0, len(logs))
	for _, log := range logs {
		out = append(out, toHashLogJSON(log, keep))
	}
	return out
}

func toHashLogPairJSON(pair *hashlog.HashLogPair, full bool) hashLogPairJSON {
	keep := diffColumnSet(pair, full)
	return hashLogPairJSON{
		Block:       pairBlock(pair),
		HashesFromA: toHashLogJSONSlice(pair.HashesFromA, keep),
		HashesFromB: toHashLogJSONSlice(pair.HashesFromB, keep),
	}
}

// diffColumnSet returns the set of columns to emit for a compact diff, or nil to emit every column. It returns
// nil (all columns) when full is requested, or when the record counts differ (a rollback) and there is no single
// pair of records to diff column-by-column — matching the text renderer's fallback to the full record set.
func diffColumnSet(pair *hashlog.HashLogPair, full bool) map[string]struct{} {
	if full || len(pair.HashesFromA) != 1 || len(pair.HashesFromB) != 1 {
		return nil
	}
	a := pair.HashesFromA[0].Hashes
	b := pair.HashesFromB[0].Hashes
	keep := make(map[string]struct{})
	for _, column := range unionKeys(a, b) {
		if !bytes.Equal(a[column], b[column]) {
			keep[column] = struct{}{}
		}
	}
	return keep
}

func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}
