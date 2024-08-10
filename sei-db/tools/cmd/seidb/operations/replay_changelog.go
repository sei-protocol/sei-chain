package operations

import (
	"fmt"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/stream/changelog"
	"path/filepath"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/spf13/cobra"
)

func ReplayChangelogCmd() *cobra.Command {
	dumpDbCmd := &cobra.Command{
		Use:   "replay-changelog",
		Short: "Scan the changelog to replay and recover pebbledb data",
		Run:   executeReplayChangelog,
	}

	dumpDbCmd.PersistentFlags().StringP("db-dir", "d", "", "Database Directory")
	dumpDbCmd.PersistentFlags().Int64P("start-offset", "s", 0, "From offset")
	dumpDbCmd.PersistentFlags().Int64P("end-offset", "e", 1, "To offset")

	return dumpDbCmd
}

func executeReplayChangelog(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	start, _ := cmd.Flags().GetInt64("start-offset")
	end, _ := cmd.Flags().GetInt64("end-offset")
	if dbDir == "" {
		panic("Must provide database dir")
	}
	if start > end || start < 0 {
		panic("Must provide a valid start/end offset")
	}
	logDir := filepath.Join(dbDir, "changelog")
	stream, err := changelog.NewStream(logger.NewNopLogger(), logDir, changelog.Config{})
	if err != nil {
		panic(err)
	}
	err = stream.Replay(uint64(start), uint64(end), processChangelogEntry)
	if err != nil {
		panic(err)
	}
}

func processChangelogEntry(index uint64, entry proto.ChangelogEntry) error {
	fmt.Printf("Offset: %d, Height: %d\n", index, entry.Version)
	for _, store := range entry.Changesets {
		storeName := store.Name
		for _, kv := range store.Changeset.Pairs {
			fmt.Printf("store: %s, key: %X\n", storeName, kv.Key)
		}
	}
	return nil
}
