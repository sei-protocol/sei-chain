package operations

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/spf13/cobra"
)

// blockHashColumn is the hash log column that holds the Tendermint block hash.
const blockHashColumn = "blockHash"

func hashLogRewindCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rewind <archive>",
		Short: "Write the rewind file that makes nodes refuse the blocks a restart abandons",
		Long: "Reads the block hashes in (--safe-height, --high] from a hash log archive of the abandoned chain " +
			"and writes a Tendermint rewind file. --high must be the highest block the network committed; the " +
			"command fails when the archive holds a block above --high or lacks any block in the range.",
		Args: cobra.ExactArgs(1),
		Run:  executeHashLogRewind,
	}
	cmd.PersistentFlags().String("chain-id", "", "Chain ID of the rewind (required)")
	cmd.PersistentFlags().Uint64("safe-height", 0, "Height the chain restarts from; blocks above it are discarded (required)")
	cmd.PersistentFlags().Uint64("high", 0, "Highest discarded block (inclusive, required)")
	cmd.PersistentFlags().String("source", "", "Free text recorded in the file, e.g. the node the archive came from")
	cmd.PersistentFlags().StringP("output", "o", "", "Output file (default stdout)")
	for _, name := range []string{"chain-id", "safe-height", "high"} {
		if err := cmd.MarkPersistentFlagRequired(name); err != nil {
			panic(err)
		}
	}
	return cmd
}

func executeHashLogRewind(cmd *cobra.Command, args []string) {
	chainID, _ := cmd.Flags().GetString("chain-id")
	safeHeight, _ := cmd.Flags().GetUint64("safe-height")
	high, _ := cmd.Flags().GetUint64("high")
	source, _ := cmd.Flags().GetString("source")
	output, _ := cmd.Flags().GetString("output")
	if chainID == "" {
		panic("--chain-id must not be empty")
	}

	discarded, err := buildDiscardedBlocks(args[0], safeHeight, high)
	if err != nil {
		panic(fmt.Errorf("build rewind: %w", err))
	}
	file := rewindFileJSON{ChainID: chainID, Source: source, SafeHeight: safeHeight, Discarded: discarded}

	out := cmd.OutOrStdout()
	if output != "" {
		f, err := os.Create(output) //nolint:gosec // the operator chooses the output path
		if err != nil {
			panic(err)
		}
		defer func() { _ = f.Close() }()
		out = f
	}
	if err := encodeJSON(out, file); err != nil {
		panic(err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%d discarded block(s) for %s above safe height %d\n",
		len(discarded), chainID, safeHeight)
}

// rewindFileJSON is the file format that sei-tendermint/types loads from rewinds/.
type rewindFileJSON struct {
	ChainID    string               `json:"chain_id"`
	Source     string               `json:"source,omitempty"`
	SafeHeight uint64               `json:"safe_height"`
	Discarded  []discardedBlockJSON `json:"discarded"`
}

type discardedBlockJSON struct {
	Height uint64 `json:"height"`
	Hash   string `json:"hash"`
}

// buildDiscardedBlocks returns the block hash the archive recorded for each block in (safeHeight, high]. It fails
// when the archive holds a block above high, lacks a block in the range, or records one block with different
// hashes.
func buildDiscardedBlocks(archive string, safeHeight, high uint64) ([]discardedBlockJSON, error) {
	if safeHeight == 0 || high <= safeHeight {
		return nil, fmt.Errorf("need 0 < safe height (%d) < high (%d)", safeHeight, high)
	}
	// A block above high that the list leaves out stays unguarded: validators
	// sign that height again on the new chain, and the two votes become valid
	// double-sign evidence.
	_, archiveHigh, ok, err := hashlog.ArchiveBlockRange(archive)
	if err != nil {
		return nil, err
	}
	if ok && archiveHigh > high {
		return nil, fmt.Errorf("the archive holds block %d above --high %d; the list must reach the abandoned chain's tip",
			archiveHigh, high)
	}
	discarded := make([]discardedBlockJSON, 0, high-safeHeight)
	err = hashlog.WalkArchiveRange(archive, safeHeight+1, high, func(block uint64, logs []*hashlog.HashLog) error {
		hashes, err := recordedHashes(logs, []string{blockHashColumn})
		if err != nil {
			return fmt.Errorf("block %d: %w", block, err)
		}
		discarded = append(discarded, discardedBlockJSON{
			Height: block,
			Hash:   strings.ToUpper(hex.EncodeToString(hashes[blockHashColumn])),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return discarded, nil
}
