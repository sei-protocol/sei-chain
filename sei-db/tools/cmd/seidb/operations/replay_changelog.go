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
	dumpDbCmd.PersistentFlags().Int64P("start-height", "s", 0, "From offset")
	dumpDbCmd.PersistentFlags().Int64P("end-height", "e", 1, "To offset")

	return dumpDbCmd
}

func executeReplayChangelog(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	start, _ := cmd.Flags().GetInt64("start-height")
	end, _ := cmd.Flags().GetInt64("end-height")
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
	// Read first entry to compute the difference between offset and height
	firstOffset, err := stream.FirstOffset()
	if err != nil {
		panic(err)
	}
	firstEntry, err := stream.ReadAt(firstOffset)
	if err != nil {
		panic(err)
	}
	gap := firstEntry.Version - int64(firstOffset)

	err = stream.Replay(uint64(start-gap), uint64(end-gap), processChangelogEntry)
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
