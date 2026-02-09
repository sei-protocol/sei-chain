package apphash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

// DumpStoreCmd returns the cobra command for dumping store contents.
func DumpStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-store <store-name> <height>",
		Short: "Dump all key-value pairs from a specific store at a given height",
		Long: `Dump all key-value pairs from a specific store (e.g., bank, evm) at a given height.

This is useful for debugging store hash mismatches between nodes. The output shows
each key (hex encoded) and either the full value (if small) or a hash of the value.

Example:
  $ seid tools dump-store bank 12345
  $ seid tools dump-store bank 12345 --home-dir /path/to/.sei
  $ seid tools dump-store bank 12345 --output /tmp/bank_dump.txt
`,
		Args: cobra.ExactArgs(2),
		RunE: runDumpStore,
	}

	cmd.Flags().String("home-dir", "", "Sei home directory (default: $HOME/.sei)")
	cmd.Flags().String("output", "", "Output file path (default: stdout)")
	cmd.Flags().Int("max-value-len", 64, "Maximum value length to print in full (longer values show hash)")
	cmd.Flags().String("key-prefix", "", "Only dump keys with this hex prefix")

	return cmd
}

func runDumpStore(cmd *cobra.Command, args []string) error {
	storeName := args[0]
	height, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid height %q: %w", args[1], err)
	}

	homeDir, err := cmd.Flags().GetString("home-dir")
	if err != nil {
		return err
	}
	if homeDir == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user home directory: %w", err)
		}
		homeDir = filepath.Join(userHome, ".sei")
	}

	outputPath, _ := cmd.Flags().GetString("output")
	maxValueLen, _ := cmd.Flags().GetInt("max-value-len")
	keyPrefixHex, _ := cmd.Flags().GetString("key-prefix")

	var keyPrefix []byte
	if keyPrefixHex != "" {
		keyPrefix, err = hex.DecodeString(keyPrefixHex)
		if err != nil {
			return fmt.Errorf("invalid key-prefix hex: %w", err)
		}
	}

	dataDir := filepath.Join(homeDir, "data")
	committerPath := filepath.Join(dataDir, "committer.db")

	// Check if SeiDB mode
	if _, err := os.Stat(committerPath); os.IsNotExist(err) {
		return fmt.Errorf("this tool currently only supports SeiDB/giga mode (committer.db not found at %s)", committerPath)
	}

	// Set up output
	var out *os.File
	if outputPath != "" {
		out, err = os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer out.Close()
		fmt.Fprintf(os.Stderr, "Writing output to: %s\n", outputPath)
	} else {
		out = os.Stdout
	}

	fmt.Fprintf(os.Stderr, "Opening memiavl commit store: %s\n", committerPath)
	fmt.Fprintf(os.Stderr, "Store: %s, Height: %d\n", storeName, height)

	log := logger.NewNopLogger()
	opts := memiavl.Options{
		Dir:             committerPath,
		ZeroCopy:        true,
		ReadOnly:        true,
		CreateIfMissing: false,
	}

	db, err := memiavl.OpenDB(log, height, opts)
	if err != nil {
		return fmt.Errorf("failed to open memiavl DB at height %d: %w", height, err)
	}
	defer db.Close()

	// Get the tree for the specified store
	tree := db.TreeByName(storeName)
	if tree == nil {
		return fmt.Errorf("store %q not found", storeName)
	}

	fmt.Fprintf(out, "=== Store Dump: %s at height %d ===\n", storeName, height)
	fmt.Fprintf(out, "Store Version: %d\n", tree.Version())
	fmt.Fprintf(out, "Store Hash: %X\n", tree.RootHash())
	fmt.Fprintf(out, "\n")

	// Create an iterator over all key-value pairs
	var iter = tree.Iterator(keyPrefix, nil, true)
	defer iter.Close()

	count := 0
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()

		// Filter by prefix if specified (for end bound)
		if keyPrefix != nil && len(key) >= len(keyPrefix) {
			match := true
			for i := range keyPrefix {
				if key[i] != keyPrefix[i] {
					match = false
					break
				}
			}
			if !match {
				break // We've passed the prefix range
			}
		}

		count++

		// Print key
		fmt.Fprintf(out, "Key[%d]: %X\n", count, key)

		// Print value (full if small, hash if large)
		if len(value) <= maxValueLen {
			fmt.Fprintf(out, "  Value (%d bytes): %X\n", len(value), value)
		} else {
			valueHash := sha256.Sum256(value)
			fmt.Fprintf(out, "  Value (%d bytes, hash): %X\n", len(value), valueHash)
		}
	}

	fmt.Fprintf(out, "\nTotal keys: %d\n", count)
	fmt.Fprintf(os.Stderr, "Dumped %d keys\n", count)

	return nil
}
