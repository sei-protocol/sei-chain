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

func ReplayChangelogCmd() *cobra.Command {
	dumpDbCmd := &cobra.Command{
		Use:   "replay-changelog",
		Short: "Scan the changelog to replay and recover pebbledb data",
		Run:   executeReplayChangelog,
	}

	dumpDbCmd.PersistentFlags().StringP("db-dir", "d", "", "Database Directory")
	dumpDbCmd.PersistentFlags().Int64P("start-offset", "s", 0, "From offset")
	dumpDbCmd.PersistentFlags().Int64P("end-offset", "e", 1, "End offset")
	dumpDbCmd.PersistentFlags().Bool("dry-run", true, "Whether to dry run or re-apply the changelog to DB")

	return dumpDbCmd
}

func executeReplayChangelog(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	start, _ := cmd.Flags().GetInt64("start-offset")
	end, _ := cmd.Flags().GetInt64("end-offset")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
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

	// open the database if apply is true
	if !dryRun {
		ssConfig := config.DefaultStateStoreConfig()
		ssConfig.KeepRecent = 0
		ssConfig.DBDirectory = dbDir
		ssStore, err = ss.NewStateStore(logger.NewNopLogger(), dbDir, ssConfig)
		if err != nil {
			panic(err)
		}
	}

	// replay the changelog
	err = stream.Replay(uint64(start), uint64(end), processChangelogEntry)
	if err != nil {
		panic(err)
	}

	// close the database
	ssStore.Close()

}

func processChangelogEntry(index uint64, entry proto.ChangelogEntry) error {
	fmt.Printf("Offset: %d, Height: %d\n", index, entry.Version)
	for _, changeset := range entry.Changesets {
		storeName := changeset.Name
		for _, kv := range changeset.Changeset.Pairs {
			fmt.Printf("store: %s, key: %X\n", storeName, kv.Key)
		}
		if ssStore != nil {
			err := ssStore.ApplyChangeset(entry.Version, changeset)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
