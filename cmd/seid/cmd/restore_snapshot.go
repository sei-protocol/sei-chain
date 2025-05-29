package cmd

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/snapshots/types"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/snapshots"
	"github.com/sei-protocol/sei-db/config"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

// runRestoreSnapshotCmd is the command runner for RestoreSnapshotCmd
func runRestoreSnapshotCmd(cmd *cobra.Command, args []string) error {
	snapshotDir, err := cmd.Flags().GetString("snapshot-dir")
	if err != nil {
		return err
	}

	//snapshotHeight, err := cmd.Flags().GetInt64("snapshot-height")
	//if err != nil {
	//	return err
	//}
	//
	//homeDir, err := cmd.Flags().GetString("home")
	//if err != nil {
	//	return err
	//}

	// Get server context to access app options
	serverCtx := server.GetServerContextFromCmd(cmd)
	viper := serverCtx.Viper

	// Access SeiDB SC (State Commit) configurations
	scEnabled := cast.ToBool(viper.Get("state-commit.sc-enable"))

	// Access SeiDB SS (State Store) configurations
	ssEnabled := cast.ToBool(viper.Get("state-store.ss-enable"))

	if scEnabled {
		// Create v2 CMS
		scConfig := config.DefaultStateCommitConfig()
		scConfig.Enable = true
		ssConfig := config.DefaultStateStoreConfig()
		ssConfig.Enable = ssEnabled
		ssConfig.KeepRecent = 0
		//rootmulti2.NewStore(homeDir, log.NewNopLogger(), scConfig, ssConfig, false)

		// Create a channel to read chunks
		ch := make(chan io.ReadCloser)

		// Start a goroutine to read chunks from files
		go func() {
			defer close(ch)
			snapshotFormat := ""
			chunksDir := filepath.Join(snapshotDir, snapshotFormat)
			entries, err := os.ReadDir(chunksDir)
			if err != nil {
				fmt.Printf("Error reading directory: %v\n", err)
				return
			}

			// Sort files by number
			var fileNames []string
			for _, entry := range entries {
				if !entry.IsDir() {
					fileNames = append(fileNames, entry.Name())
				}
			}
			sort.Slice(fileNames, func(i, j int) bool {
				numI, _ := strconv.Atoi(fileNames[i])
				numJ, _ := strconv.Atoi(fileNames[j])
				return numI < numJ
			})

			// Read files in order
			for _, fileName := range fileNames {
				f, err := os.Open(filepath.Join(chunksDir, fileName))
				if err != nil {
					fmt.Printf("Error opening file %s: %v\n", fileName, err)
					continue
				}
				ch <- f
			}
		}()

		// Create a stream reader
		streamReader, err := snapshots.NewStreamReader(ch)
		if err != nil {
			fmt.Printf("Error creating stream reader: %v\n", err)
			os.Exit(1)
		}
		defer streamReader.Close()

		// Read and print items
		var currentStore string
		for {
			item := &types.SnapshotItem{}
			err := streamReader.ReadMsg(item)
			if err == io.EOF {
				break
			}
			if err != nil {
				fmt.Printf("Error reading item: %v\n", err)
				os.Exit(1)
			}

			switch item.Item.(type) {
			case *types.SnapshotItem_Store:
				store := item.GetStore()
				currentStore = store.Name
				fmt.Printf("\nStore: %s\n", currentStore)
			case *types.SnapshotItem_IAVL:
				iavl := item.GetIAVL()
				if iavl.Height > math.MaxInt8 {
					fmt.Printf("Error: node height %v exceeds %v\n", iavl.Height, math.MaxInt8)
					os.Exit(1)
				}
				// Protobuf does not differentiate between []byte{} as nil, but fortunately IAVL does
				// not allow nil keys nor nil values for leaf nodes, so we can always set them to empty.
				key := iavl.Key
				if key == nil {
					key = []byte{}
				}
				value := iavl.Value
				if iavl.Height == 0 && value == nil {
					value = []byte{}
				}
				fmt.Printf("  IAVL Node: key=%x value=%x version=%d height=%d\n",
					key, value, iavl.Version, iavl.Height)
			case *types.SnapshotItem_Extension:
				ext := item.GetExtension()
				fmt.Printf("  Extension: name=%s format=%d\n", ext.Name, ext.Format)
			case *types.SnapshotItem_ExtensionPayload:
				fmt.Printf("  Extension Payload: %x\n", item.GetExtensionPayload().Payload)
			default:
				fmt.Printf("  Unknown item type\n")
			}
		}

		//// Call Restore with the snapshot height, format version, and reader
		//cmsV2.Restore(uint64(snapshotHeight), 1, streamReader)
	}

	return nil
}

// RestoreSnapshotCmd returns a new cobra command for restoring a snapshot
func RestoreSnapshotCmd(defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore-snapshot [--snapshot-dir <directory>] [--snapshot-height <height>]",
		Short: "Restore application state from a local state sync snapshot",
		Long:  `Restore application state from a local state sync snapshot. This command is a placeholder and not yet implemented.`,
		Args:  cobra.ExactArgs(0),
		RunE:  runRestoreSnapshotCmd,
	}

	cmd.Flags().String("snapshot-dir", "", "Directory containing the snapshot to restore")
	cmd.Flags().Int64("snapshot-height", 0, "Height of the snapshot to restore")
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")

	return cmd
}
