package operations

import (
	"fmt"
	"path/filepath"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/ss"
	"github.com/sei-protocol/sei-db/ss/types"
	"github.com/sei-protocol/sei-db/stream/changelog"
	"github.com/spf13/cobra"
)

var ssStore types.StateStore
var dryRun = true

func ReplayChangelogCmd() *cobra.Command {
	dumpDbCmd := &cobra.Command{
		Use:   "replay-changelog",
		Short: "Scan the changelog to replay and recover pebbledb data",
		Run:   executeReplayChangelog,
	}

	dumpDbCmd.PersistentFlags().StringP("db-dir", "d", "", "Database Directory")
	dumpDbCmd.PersistentFlags().Int64P("start-offset", "s", 0, "From offset")
	dumpDbCmd.PersistentFlags().Int64P("end-offset", "e", 0, "End offset, default is latest")
	dumpDbCmd.PersistentFlags().Bool("no-dry-run", false, "Whether to dry run or re-apply the changelog to DB")

	return dumpDbCmd
}

func executeReplayChangelog(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	start, _ := cmd.Flags().GetUint64("start-offset")
	end, _ := cmd.Flags().GetUint64("end-offset")
	noDryRun, _ := cmd.Flags().GetBool("no-dry-run")
	if dbDir == "" {
		panic("Must provide database dir")
	}

	logDir := filepath.Join(dbDir, "changelog")
	stream, err := changelog.NewStream(logger.NewNopLogger(), logDir, changelog.Config{})
	if err != nil {
		panic(err)
	}

	// use first available offset
	if start <= 0 {
		startOffset, err := stream.FirstOffset()
		if err != nil {
			panic(err)
		}
		start = startOffset
	}

	if end <= 0 {
		// use latest offset
		endOffset, err := stream.LastOffset()
		if err != nil {
			panic(err)
		}
		end = endOffset
	}

	// open the database if this is not a dry run
	if noDryRun {
		dryRun = false
		ssConfig := config.DefaultStateStoreConfig()
		ssConfig.KeepRecent = 0
		ssConfig.DBDirectory = dbDir
		ssStore, err = ss.NewStateStore(logger.NewNopLogger(), dbDir, ssConfig)
		if err != nil {
			panic(err)
		}
	}

	// replay the changelog
	err = stream.Replay(start, end, processChangelogEntry)
	if err != nil {
		panic(err)
	}

	// close the database
	if ssStore != nil {
		ssStore.Close()
	}

}

func processChangelogEntry(index uint64, entry proto.ChangelogEntry) error {
	fmt.Printf("Offset: %d, Height: %d\n", index, entry.Version)
	for _, changeset := range entry.Changesets {
		storeName := changeset.Name
		for _, kv := range changeset.Changeset.Pairs {
			if dryRun {
				fmt.Printf("store: %s, key: %X\n", storeName, kv.Key)
			}
		}
		if ssStore != nil {
			fmt.Printf("Re-applied changeset for height %d\n", entry.Version)
			err := ssStore.ApplyChangeset(entry.Version, changeset)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
