package operations

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/spf13/cobra"
)

// appHashColumn is the hash log column that holds the app hash returned to consensus.
const appHashColumn = "appHash"

func hashLogAppHashOverridesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apphash-overrides <prod-archive> <reserve-archive>",
		Short: "Write the app hash override table that accepts a reserve's state in place of production's",
		Long: "Reads the production and reserve hash log archives over [--low, --high] and writes a Tendermint " +
			"app hash override file. Each row pairs the production app hash (the one the chain's headers record) " +
			"with the reserve app hash for the same block. The command fails when a block is missing from either " +
			"archive, or when any --match column differs between them, because the reserve then did not replay " +
			"the same history.",
		Args: cobra.ExactArgs(2),
		Run:  executeHashLogAppHashOverrides,
	}
	cmd.PersistentFlags().String("chain-id", "", "Chain ID the overrides apply to (required)")
	cmd.PersistentFlags().Uint64("low", 0, "Lowest block to cover (inclusive, required)")
	cmd.PersistentFlags().Uint64("high", 0, "Highest block to cover (inclusive, required)")
	cmd.PersistentFlags().StringSlice("match", []string{"blockHash", "resultHash", hashlog.ChangesetHashType},
		"Columns that must be equal in both archives for every block")
	cmd.PersistentFlags().String("source", "", "Free text recorded in the file, e.g. the nodes and heights used")
	cmd.PersistentFlags().StringP("output", "o", "", "Output file (default stdout)")
	for _, name := range []string{"chain-id", "low", "high"} {
		if err := cmd.MarkPersistentFlagRequired(name); err != nil {
			panic(err)
		}
	}
	return cmd
}

func executeHashLogAppHashOverrides(cmd *cobra.Command, args []string) {
	chainID, _ := cmd.Flags().GetString("chain-id")
	low, _ := cmd.Flags().GetUint64("low")
	high, _ := cmd.Flags().GetUint64("high")
	match, _ := cmd.Flags().GetStringSlice("match")
	source, _ := cmd.Flags().GetString("source")
	output, _ := cmd.Flags().GetString("output")

	rows, err := buildAppHashOverrides(args[0], args[1], low, high, match)
	if err != nil {
		panic(fmt.Errorf("build app hash overrides: %w", err))
	}
	file := appHashOverrideFileJSON{ChainID: chainID, Source: source, Overrides: rows}

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
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%d override(s) for %s over blocks [%d, %d]\n", len(rows), chainID, low, high)
}

// appHashOverrideFileJSON is the file format that sei-tendermint/types loads from apphash_overrides/.
type appHashOverrideFileJSON struct {
	ChainID   string                   `json:"chain_id"`
	Source    string                   `json:"source,omitempty"`
	Overrides []appHashOverrideRowJSON `json:"overrides"`
}

type appHashOverrideRowJSON struct {
	Height      uint64 `json:"height"`
	Recorded    string `json:"recorded"`
	Replacement string `json:"replacement"`
}

// buildAppHashOverrides returns one row for each block in [low, high] whose reserve app hash differs from the
// production app hash. It fails when either archive lacks a block, when a block's records in one archive
// disagree, or when a match column differs between the archives.
func buildAppHashOverrides(prodArchive, reserveArchive string, low, high uint64, match []string) ([]appHashOverrideRowJSON, error) {
	columns := append([]string{appHashColumn}, match...)
	var rows []appHashOverrideRowJSON
	err := hashlog.WalkHashesInRange(prodArchive, reserveArchive, low, high,
		func(block uint64, prodLogs, reserveLogs []*hashlog.HashLog) error {
			prod, err := recordedHashes(prodLogs, columns)
			if err != nil {
				return fmt.Errorf("block %d in the production archive: %w", block, err)
			}
			reserve, err := recordedHashes(reserveLogs, columns)
			if err != nil {
				return fmt.Errorf("block %d in the reserve archive: %w", block, err)
			}
			for _, column := range match {
				if !bytes.Equal(prod[column], reserve[column]) {
					return fmt.Errorf("block %d: %s differs: production %x, reserve %x",
						block, column, prod[column], reserve[column])
				}
			}
			if bytes.Equal(prod[appHashColumn], reserve[appHashColumn]) {
				return nil
			}
			rows = append(rows, appHashOverrideRowJSON{
				Height:      block,
				Recorded:    strings.ToUpper(hex.EncodeToString(prod[appHashColumn])),
				Replacement: strings.ToUpper(hex.EncodeToString(reserve[appHashColumn])),
			})
			return nil
		})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("the app hashes agree over blocks [%d, %d]; no override is needed", low, high)
	}
	return rows, nil
}

// recordedHashes returns the hashes a block recorded, after checking that every column in columns is present and
// that every record of the block holds the same value for it.
func recordedHashes(logs []*hashlog.HashLog, columns []string) (map[string][]byte, error) {
	if len(logs) == 0 {
		return nil, errors.New("no record")
	}
	hashes := logs[0].Hashes
	for _, column := range columns {
		if hashes[column] == nil {
			return nil, fmt.Errorf("%s is not recorded", column)
		}
		for _, log := range logs[1:] {
			if !bytes.Equal(log.Hashes[column], hashes[column]) {
				return nil, fmt.Errorf("%d records disagree on %s", len(logs), column)
			}
		}
	}
	return hashes, nil
}
