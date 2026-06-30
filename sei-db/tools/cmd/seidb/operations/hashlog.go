package operations

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"

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
		Use:   "get-block",
		Short: "Print every hash recorded for a single block in a hash log archive",
		Run:   executeHashLogGetBlock,
	}
	cmd.PersistentFlags().StringP("archive", "a", "", "Hash log archive directory")
	cmd.PersistentFlags().Uint64P("block", "n", 0, "Block number to look up")
	cmd.PersistentFlags().Bool("json", false, "Emit JSON instead of human-readable text")
	return cmd
}

func executeHashLogGetBlock(cmd *cobra.Command, _ []string) {
	archive, _ := cmd.Flags().GetString("archive")
	block, _ := cmd.Flags().GetUint64("block")
	asJSON, _ := cmd.Flags().GetBool("json")

	if archive == "" {
		panic("Must provide --archive pointing at a hash log archive directory")
	}

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
		Use:   "compare",
		Short: "Compare two hash log archives and report blocks whose hashes differ",
		Run:   executeHashLogCompare,
	}
	cmd.PersistentFlags().StringP("archive-a", "a", "", "First hash log archive directory")
	cmd.PersistentFlags().StringP("archive-b", "b", "", "Second hash log archive directory")
	cmd.PersistentFlags().Uint64("low", 0, "Lowest block to compare (inclusive); requires --high")
	cmd.PersistentFlags().Uint64("high", 0, "Highest block to compare (inclusive); requires --low")
	cmd.PersistentFlags().Int("max-diffs", -1, "Maximum number of differing blocks to report, or -1 for all")
	cmd.PersistentFlags().Bool("json", false, "Emit JSON instead of human-readable text")
	return cmd
}

func executeHashLogCompare(cmd *cobra.Command, _ []string) {
	archiveA, _ := cmd.Flags().GetString("archive-a")
	archiveB, _ := cmd.Flags().GetString("archive-b")
	maxDiffs, _ := cmd.Flags().GetInt("max-diffs")
	asJSON, _ := cmd.Flags().GetBool("json")

	if archiveA == "" || archiveB == "" {
		panic("Must provide both --archive-a and --archive-b")
	}

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

	if err := renderCompare(cmd.OutOrStdout(), result, asJSON); err != nil {
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
		return encodeJSON(w, toHashLogJSONSlice(logs))
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

// renderCompare writes the result of comparing two archives, either as JSON or human-readable text.
func renderCompare(w io.Writer, result compareResult, asJSON bool) error {
	if asJSON {
		out := make([]hashLogPairJSON, 0, len(result.diffs))
		for _, pair := range result.diffs {
			out = append(out, toHashLogPairJSON(pair))
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
		ew.printf("block %d differs:\n", pairBlock(pair))
		ew.printf("  archive A:\n")
		writeHashLogSet(ew, pair.HashesFromA, "    ")
		ew.printf("  archive B:\n")
		writeHashLogSet(ew, pair.HashesFromB, "    ")
	}
	ew.printf("%d differing block(s) reported.\n", len(result.diffs))
	// CompareHashes stops as soon as it has collected maxDiffs diffs, so an exactly-full result may be a
	// truncation rather than the true total. Warn so the operator knows to widen the cap if needed.
	if result.maxDiffs >= 0 && len(result.diffs) == result.maxDiffs {
		ew.printf("Output truncated at --max-diffs=%d; there may be more differing blocks.\n", result.maxDiffs)
	}
	return ew.err
}

// writeHashLogText renders one record's version (when present) and its hashes, sorted by hash type for stable
// output. A nil hash (the type was registered but not recorded for this block) prints as "<none>".
func writeHashLogText(ew *errWriter, log *hashlog.HashLog, indent string) {
	if log.Version != "" {
		ew.printf("%sversion: %s\n", indent, log.Version)
	}
	hashTypes := make([]string, 0, len(log.Hashes))
	for hashType := range log.Hashes {
		hashTypes = append(hashTypes, hashType)
	}
	sort.Strings(hashTypes)
	for _, hashType := range hashTypes {
		hash := log.Hashes[hashType]
		if hash == nil {
			ew.printf("%s%s: <none>\n", indent, hashType)
			continue
		}
		ew.printf("%s%s: %s\n", indent, hashType, hex.EncodeToString(hash))
	}
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

func toHashLogJSON(log *hashlog.HashLog) hashLogJSON {
	hashes := make(map[string]*string, len(log.Hashes))
	for hashType, hash := range log.Hashes {
		if hash == nil {
			hashes[hashType] = nil
			continue
		}
		encoded := hex.EncodeToString(hash)
		hashes[hashType] = &encoded
	}
	return hashLogJSON{BlockNumber: log.BlockNumber, Version: log.Version, Hashes: hashes}
}

func toHashLogJSONSlice(logs []*hashlog.HashLog) []hashLogJSON {
	out := make([]hashLogJSON, 0, len(logs))
	for _, log := range logs {
		out = append(out, toHashLogJSON(log))
	}
	return out
}

func toHashLogPairJSON(pair *hashlog.HashLogPair) hashLogPairJSON {
	return hashLogPairJSON{
		Block:       pairBlock(pair),
		HashesFromA: toHashLogJSONSlice(pair.HashesFromA),
		HashesFromB: toHashLogJSONSlice(pair.HashesFromB),
	}
}

func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}
