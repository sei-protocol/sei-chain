package operations

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/spf13/cobra"
)

func FlatKVInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flatkv-info",
		Short: "Show FlatKV store metadata: version, root hash, snapshots, WAL range",
		Run:   executeFlatKVInfo,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "FlatKV data directory (e.g. /root/.sei/data/committer.db/flatkv)")
	return cmd
}

func executeFlatKVInfo(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	if dbDir == "" {
		panic("Must provide database dir")
	}

	fmt.Printf("=== FlatKV Info: %s ===\n\n", dbDir)

	// Current symlink
	currentTarget, _ := os.Readlink(filepath.Join(dbDir, "current"))
	if currentTarget != "" {
		fmt.Printf("Current symlink:  %s\n", currentTarget)
	} else {
		fmt.Printf("Current symlink:  (not found)\n")
	}

	// Snapshots
	snapshots := listFlatKVSnapshots(dbDir)
	if len(snapshots) > 0 {
		fmt.Printf("Snapshots:        %d\n", len(snapshots))
		for _, v := range snapshots {
			marker := ""
			name := fmt.Sprintf("snapshot-%020d", v)
			if name == currentTarget {
				marker = " (current)"
			}
			fmt.Printf("  - version %d%s\n", v, marker)
		}
	} else {
		fmt.Printf("Snapshots:        none\n")
	}

	// WAL info
	changelogDir := filepath.Join(dbDir, "changelog")
	if _, err := os.Stat(changelogDir); err == nil {
		stream, err := wal.NewChangelogWAL(changelogDir, wal.Config{})
		if err != nil {
			fmt.Printf("WAL:              error opening: %v\n", err)
		} else {
			first, firstErr := stream.FirstOffset()
			last, lastErr := stream.LastOffset()
			if firstErr == nil && lastErr == nil && last >= first {
				fmt.Printf("WAL offsets:      %d .. %d\n", first, last)
			} else {
				fmt.Printf("WAL offsets:      (empty or error)\n")
			}
			_ = stream.Close()
		}
	} else {
		fmt.Printf("WAL:              (no changelog directory)\n")
	}

	fmt.Println()

	// Open store read-only for metadata
	store, err := openFlatKVReadOnly(dbDir, 0)
	if err != nil {
		fmt.Printf("Could not open store: %v\n", err)
		return
	}
	defer func() { _ = store.Close() }()

	fmt.Printf("Committed version:  %d\n", store.Version())
	fmt.Printf("Root hash:          %X\n", store.RootHash())
	fmt.Printf("Committed hash:     %X\n", store.CommittedRootHash())

	// Count keys per DB via iterator
	iter := store.RawGlobalIterator()
	defer func() { _ = iter.Close() }()

	var totalKeys uint64
	if iter.First() {
		for iter.Valid() {
			totalKeys++
			iter.Next()
		}
	}
	if err := iter.Error(); err != nil {
		fmt.Printf("Iterator error:     %v\n", err)
	}
	fmt.Printf("Total keys:         %d\n", totalKeys)
}
