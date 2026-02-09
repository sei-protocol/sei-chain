package apphash

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/spf13/cobra"
	dbm "github.com/tendermint/tm-db"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/tendermint/tendermint/crypto/merkle"
)

const (
	// commitInfoKeyFmt matches the key format used in sei-cosmos/store/rootmulti/store.go
	commitInfoKeyFmt = "s/%d"
)

// GetAppHashCmd returns the cobra command for getting AppHash details.
func GetAppHashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-app-hash <height>",
		Short: "Print the AppHash computation details for a given block height",
		Long: `Print all fields that go into the AppHash calculation for a given block height.

This is useful for debugging consensus breaks where nodes have mismatched AppHash values.
The command loads the CommitInfo from the application database (for IAVL nodes) or from 
the memiavl commit store (for SeiDB/giga mode nodes) and prints each store's name and 
hash that contributes to the final AppHash merkle root.

Example:
  $ seid tools get-app-hash 12345
  $ seid tools get-app-hash 12345 --home-dir /path/to/.sei
`,
		Args: cobra.ExactArgs(1),
		RunE: runGetAppHash,
	}

	cmd.Flags().String("home-dir", "", "Sei home directory (default: $HOME/.sei)")

	return cmd
}

func runGetAppHash(cmd *cobra.Command, args []string) error {
	height, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid height %q: %w", args[0], err)
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

	dataDir := filepath.Join(homeDir, "data")

	// Try SeiDB/memiavl first (committer.db)
	committerPath := filepath.Join(dataDir, "committer.db")
	if _, err := os.Stat(committerPath); err == nil {
		fmt.Printf("Detected SeiDB mode, opening memiavl commit store: %s\n", committerPath)
		return runGetAppHashSeiDB(committerPath, height)
	}

	// Fall back to traditional IAVL (application.db)
	fmt.Printf("Opening application database in: %s\n", dataDir)
	return runGetAppHashIAVL(dataDir, height)
}

func runGetAppHashSeiDB(committerPath string, height int64) error {
	// Create a nop logger for the memiavl store
	log := logger.NewNopLogger()

	// Configure options for read-only access
	opts := memiavl.Options{
		Dir:             committerPath,
		ZeroCopy:        true,
		ReadOnly:        true,
		CreateIfMissing: false,
	}

	// Open the DB at the target version
	db, err := memiavl.OpenDB(log, height, opts)
	if err != nil {
		return fmt.Errorf("failed to open memiavl DB at height %d: %w", height, err)
	}
	defer db.Close()

	// Get the commit info
	commitInfo := db.LastCommitInfo()
	if commitInfo == nil {
		return fmt.Errorf("CommitInfo is nil for height %d", height)
	}

	// Convert proto.CommitInfo to storetypes.CommitInfo for hash computation
	storeInfos := make([]storetypes.StoreInfo, len(commitInfo.StoreInfos))
	for i, si := range commitInfo.StoreInfos {
		storeInfos[i] = storetypes.StoreInfo{
			Name: si.Name,
			CommitId: storetypes.CommitID{
				Version: si.CommitId.Version,
				Hash:    si.CommitId.Hash,
			},
		}
	}

	sdkCommitInfo := &storetypes.CommitInfo{
		Version:    commitInfo.Version,
		StoreInfos: storeInfos,
	}

	return printAppHashDetails(sdkCommitInfo, height)
}

func runGetAppHashIAVL(dataDir string, height int64) error {
	db, err := dbm.NewGoLevelDB("application", dataDir)
	if err != nil {
		return fmt.Errorf("failed to open application database: %w", err)
	}
	defer db.Close()

	// Load the CommitInfo for the given height
	commitInfo, err := loadCommitInfo(db, height)
	if err != nil {
		return fmt.Errorf("failed to load CommitInfo for height %d: %w", height, err)
	}

	if commitInfo == nil {
		return fmt.Errorf("CommitInfo is nil for height %d", height)
	}

	return printAppHashDetails(commitInfo, height)
}

func printAppHashDetails(commitInfo *storetypes.CommitInfo, height int64) error {
	// Compute the AppHash using the same algorithm as the SDK
	computedHash := computeAppHash(commitInfo)

	fmt.Printf("\n=== AppHash Computation Details ===\n")
	fmt.Printf("Height: %d\n", height)
	fmt.Printf("Version: %d\n", commitInfo.Version)
	fmt.Printf("StoreCount: %d\n", len(commitInfo.StoreInfos))
	fmt.Printf("ComputedAppHash: %X\n", computedHash)
	fmt.Printf("\n")

	if len(commitInfo.StoreInfos) == 0 {
		fmt.Printf("No stores in this commit.\n")
		return nil
	}

	// Sort stores by name for consistent output
	sortedStores := make([]storetypes.StoreInfo, len(commitInfo.StoreInfos))
	copy(sortedStores, commitInfo.StoreInfos)
	sort.Slice(sortedStores, func(i, j int) bool {
		return sortedStores[i].Name < sortedStores[j].Name
	})

	fmt.Printf("=== Store Hashes (Sorted by Name) ===\n")
	fmt.Printf("The AppHash is computed as a merkle root from these store hashes.\n")
	fmt.Printf("Each leaf is: length_prefix(key) || length_prefix(sha256(value))\n")
	fmt.Printf("Where key = store name and value = store hash.\n")
	fmt.Printf("\n")

	for i, storeInfo := range sortedStores {
		fmt.Printf("--- Store[%d] ---\n", i)
		fmt.Printf("  Name:    %s\n", storeInfo.Name)
		fmt.Printf("  Version: %d\n", storeInfo.CommitId.Version)
		fmt.Printf("  Hash:    %X\n", storeInfo.CommitId.Hash)
		// Also show what goes into the merkle leaf
		valueHash := sha256.Sum256(storeInfo.CommitId.Hash)
		fmt.Printf("  LeafValueHash (sha256 of Hash): %X\n", valueHash)
		fmt.Printf("\n")
	}

	return nil
}

func loadCommitInfo(db dbm.DB, height int64) (*storetypes.CommitInfo, error) {
	cInfoKey := fmt.Sprintf(commitInfoKeyFmt, height)
	bz, err := db.Get([]byte(cInfoKey))
	if err != nil {
		return nil, err
	}
	if len(bz) == 0 {
		return nil, fmt.Errorf("no CommitInfo found for height %d", height)
	}

	commitInfo := &storetypes.CommitInfo{}
	err = commitInfo.Unmarshal(bz)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal CommitInfo: %w", err)
	}

	return commitInfo, nil
}

// computeAppHash computes the AppHash from CommitInfo using the same algorithm
// as sei-cosmos/store/types/commit_info.go Hash() method.
// This is a merkle root where each leaf is built from key-value pairs where:
// - key = store name
// - value = store hash
// Each leaf is: length_prefix(key) || length_prefix(sha256(value))
func computeAppHash(ci *storetypes.CommitInfo) []byte {
	if len(ci.StoreInfos) == 0 {
		return nil
	}

	// Build the map of store name -> store hash
	m := make(map[string][]byte, len(ci.StoreInfos))
	for _, storeInfo := range ci.StoreInfos {
		m[storeInfo.Name] = storeInfo.CommitId.Hash
	}

	// Sort keys
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build leaves: each leaf is length_prefix(key) || length_prefix(sha256(value))
	leaves := make([][]byte, len(keys))
	for i, key := range keys {
		value := m[key]
		valueHash := sha256.Sum256(value)
		leaves[i] = kvPairBytes([]byte(key), valueHash[:])
	}

	return merkle.HashFromByteSlices(leaves)
}

// kvPairBytes returns the bytes representation of a key-value pair
// as: length_prefix(key) || length_prefix(value)
func kvPairBytes(key, value []byte) []byte {
	buf := make([]byte, 8+len(key)+8+len(value))

	// Encode the key, prefixed with its length
	nlk := binary.PutUvarint(buf, uint64(len(key)))
	nk := copy(buf[nlk:], key)

	// Encode the value, prefixed with its length
	nlv := binary.PutUvarint(buf[nlk+nk:], uint64(len(value)))
	nv := copy(buf[nlk+nk+nlv:], value)

	return buf[:nlk+nk+nlv+nv]
}

// Ensure proto package is used (for SeiDB mode)
var _ = proto.CommitInfo{}
var _ = context.Background
