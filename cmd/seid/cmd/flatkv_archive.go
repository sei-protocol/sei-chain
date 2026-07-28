package cmd

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/klauspost/compress/zstd"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss"
	sscomposite "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	tmstatesync "github.com/sei-protocol/sei-chain/sei-tendermint/statesync"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

const (
	flatKVArchiveFormatVersion = 1
	flatKVArchiveManifestName  = "manifest.json"
	flatKVArchiveSnapshotPref  = "snapshot-"
)

type flatKVArchiveManifest struct {
	FormatVersion int    `json:"format_version"`
	ChainID       string `json:"chain_id"`
	Height        int64  `json:"height"`
	AppHash       string `json:"app_hash"`
	CreatedAt     string `json:"created_at"`
	SnapshotName  string `json:"snapshot_name"`
	// StateStoreSnapshot records which online state-store checkpoint the
	// archive was packed from (empty when packed from the live directory,
	// which requires a quiesced donor).
	StateStoreSnapshot string                   `json:"state_store_snapshot,omitempty"`
	Files              []flatKVArchiveFileEntry `json:"files"`
}

type flatKVArchiveFileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Mode   int64  `json:"mode"`
	SHA256 string `json:"sha256"`
}

type tendermintBlockResponse struct {
	BlockID struct {
		Hash string `json:"hash"`
	} `json:"block_id"`
	Block struct {
		Header struct {
			ChainID string `json:"chain_id"`
			AppHash string `json:"app_hash"`
		} `json:"header"`
	} `json:"block"`
	Result struct {
		BlockID struct {
			Hash string `json:"hash"`
		} `json:"block_id"`
		Block struct {
			Header struct {
				ChainID string `json:"chain_id"`
				AppHash string `json:"app_hash"`
			} `json:"header"`
		} `json:"block"`
	} `json:"result"`
}

// FlatKVArchiveCmd creates and restores out-of-band FlatKV checkpoint archives.
func FlatKVArchiveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flatkv-archive",
		Short: "Create or restore FlatKV-only checkpoint archives",
	}
	cmd.AddCommand(flatKVArchiveCreateCmd(), flatKVArchiveRestoreCmd())
	return cmd
}

func flatKVArchiveCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Archive the current FlatKV checkpoint and optional wasm directory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)
			homeDir := serverCtx.Config.RootDir
			appOpts := serverCtx.Viper
			if err := requireFlatKVOnly(appOpts.Get(app.FlagSCWriteMode), appOpts.Get(app.FlagSCWriteModeEnableAuto)); err != nil {
				return err
			}

			rpc, err := cmd.Flags().GetString("archive-rpc")
			if err != nil {
				return fmt.Errorf("get rpc flag: %w", err)
			}
			out, err := cmd.Flags().GetString("out")
			if err != nil {
				return fmt.Errorf("get out flag: %w", err)
			}
			upload, err := cmd.Flags().GetString("upload")
			if err != nil {
				return fmt.Errorf("get upload flag: %w", err)
			}
			flatKVRoot, err := cmd.Flags().GetString("flatkv-dir")
			if err != nil {
				return fmt.Errorf("get flatkv-dir flag: %w", err)
			}
			if flatKVRoot == "" {
				flatKVRoot = filepath.Join(homeDir, "data", "state_commit", "flatkv")
			}
			wasmDir, err := cmd.Flags().GetString("wasm-dir")
			if err != nil {
				return fmt.Errorf("get wasm-dir flag: %w", err)
			}
			if wasmDir == "" {
				wasmDir = filepath.Join(homeDir, "wasm")
			}
			stateStoreDir, err := cmd.Flags().GetString("state-store-dir")
			if err != nil {
				return fmt.Errorf("get state-store-dir flag: %w", err)
			}
			if stateStoreDir == "" {
				stateStoreDir = filepath.Join(homeDir, "data", "state_store")
			}

			includeSS, err := cmd.Flags().GetBool("include-state-store")
			if err != nil {
				return fmt.Errorf("get include-state-store flag: %w", err)
			}
			includeWasm, err := cmd.Flags().GetBool("include-wasm")
			if err != nil {
				return fmt.Errorf("get include-wasm flag: %w", err)
			}
			if !includeWasm {
				wasmDir = ""
			}

			// Default archive shape is SC-only: the FlatKV checkpoint is the
			// single source of truth and restore rebuilds state_store from it.
			// --include-state-store keeps the legacy copy path, pairing an
			// immutable FlatKV snapshot with an immutable state-store
			// checkpoint labeled >= the FlatKV height H so the restored node
			// has every version <= H; holes above H are refilled by block
			// replay from H+1.
			var ssSource, ssSnapshotName string
			var snapshotDir, snapshotName string
			var height int64
			if includeSS {
				var ssVersion int64
				ssSource, ssSnapshotName, ssVersion, err = selectStateStoreSource(stateStoreDir)
				if err != nil {
					return err
				}
				if ssSnapshotName != "" {
					snapshotDir, snapshotName, height, err = selectFlatKVSnapshotAtMost(flatKVRoot, ssVersion)
					if err != nil {
						return err
					}
					fmt.Printf("Using state-store checkpoint %s (version %d) with FlatKV snapshot %s\n",
						ssSnapshotName, ssVersion, snapshotName)
				} else {
					snapshotDir, snapshotName, height, err = selectFlatKVSnapshot(flatKVRoot)
					if err != nil {
						return err
					}
					if info, statErr := os.Stat(ssSource); statErr == nil && info.IsDir() {
						fmt.Println("WARNING: no online state-store checkpoint found; packing the live " +
							"state_store directory. This is only safe when the donor process is stopped. " +
							"Enable state-store.ss-checkpoint-interval to archive from a live node.")
					}
				}
			} else {
				snapshotDir, snapshotName, height, err = selectFlatKVSnapshot(flatKVRoot)
				if err != nil {
					return err
				}
			}
			chainID, appHash, err := queryArchivedAppHash(cmd.Context(), rpc, height)
			if err != nil {
				return err
			}
			if flagChainID := cast.ToString(appOpts.Get(flags.FlagChainID)); flagChainID != "" && chainID != flagChainID {
				return fmt.Errorf("RPC chain ID %q does not match configured chain ID %q", chainID, flagChainID)
			}

			manifest, fileSources, err := buildFlatKVArchiveManifest(chainID, height, appHash, snapshotName, snapshotDir, wasmDir, ssSource)
			if err != nil {
				return err
			}
			manifest.StateStoreSnapshot = ssSnapshotName
			if out == "" {
				out = filepath.Join(".", fmt.Sprintf("flatkv-archive-%s-%d.tar.zst", chainID, height))
			}
			if err := writeFlatKVArchive(out, manifest, fileSources); err != nil {
				return err
			}
			fmt.Printf("Created FlatKV archive %s height=%d app_hash=%s files=%d\n",
				out, manifest.Height, manifest.AppHash, len(manifest.Files))

			if upload != "" {
				if err := uploadFileToS3(cmd.Context(), out, upload); err != nil {
					return err
				}
				fmt.Printf("Uploaded FlatKV archive to %s\n", upload)
			}
			return nil
		},
	}
	cmd.Flags().String("archive-rpc", "http://localhost:26657", "RPC endpoint used to fetch the trusted chain ID and app hash")
	cmd.Flags().String("out", "", "Output archive path (default ./flatkv-archive-<chain-id>-<height>.tar.zst)")
	cmd.Flags().String("upload", "", "Optional s3://bucket/key destination")
	cmd.Flags().String("flatkv-dir", "", "FlatKV root directory (default <home>/data/state_commit/flatkv)")
	cmd.Flags().String("wasm-dir", "", "Wasm directory (default <home>/wasm; only used with --include-wasm)")
	cmd.Flags().String("state-store-dir", "", "State-store query directory (default <home>/data/state_store; only used with --include-state-store)")
	cmd.Flags().Bool("include-state-store", false, "Also pack state_store into the archive instead of relying on restore-side rebuild from FlatKV")
	cmd.Flags().Bool("include-wasm", false, "Also pack the wasm directory into the archive")
	cmd.Flags().String(flags.FlagChainID, "", "Expected network chain ID")
	return cmd
}

func flatKVArchiveRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore a FlatKV checkpoint archive and bootstrap Tendermint state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)
			homeDir := serverCtx.Config.RootDir
			appOpts := serverCtx.Viper
			if err := requireFlatKVOnly(appOpts.Get(app.FlagSCWriteMode), appOpts.Get(app.FlagSCWriteModeEnableAuto)); err != nil {
				return err
			}

			from, err := cmd.Flags().GetString("from")
			if err != nil {
				return fmt.Errorf("get from flag: %w", err)
			}
			if from == "" {
				return fmt.Errorf("--from is required")
			}
			rpcServers, err := cmd.Flags().GetStringSlice("verification-rpc")
			if err != nil {
				return fmt.Errorf("get rpc flag: %w", err)
			}
			if len(rpcServers) < 2 {
				return fmt.Errorf("at least two --rpc endpoints are required")
			}
			trustHeight, err := cmd.Flags().GetInt64("trust-height")
			if err != nil {
				return fmt.Errorf("get trust-height flag: %w", err)
			}
			trustHash, err := cmd.Flags().GetString("trust-hash")
			if err != nil {
				return fmt.Errorf("get trust-hash flag: %w", err)
			}
			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return fmt.Errorf("get force flag: %w", err)
			}
			flatKVRoot, err := cmd.Flags().GetString("flatkv-dir")
			if err != nil {
				return fmt.Errorf("get flatkv-dir flag: %w", err)
			}
			if flatKVRoot == "" {
				flatKVRoot = filepath.Join(homeDir, "data", "state_commit", "flatkv")
			}
			wasmDir, err := cmd.Flags().GetString("wasm-dir")
			if err != nil {
				return fmt.Errorf("get wasm-dir flag: %w", err)
			}
			if wasmDir == "" {
				wasmDir = filepath.Join(homeDir, "wasm")
			}
			stateStoreDir, err := cmd.Flags().GetString("state-store-dir")
			if err != nil {
				return fmt.Errorf("get state-store-dir flag: %w", err)
			}
			if stateStoreDir == "" {
				stateStoreDir = filepath.Join(homeDir, "data", "state_store")
			}

			localArchive, cleanup, err := materializeArchive(cmd.Context(), from)
			if err != nil {
				return err
			}
			defer cleanup()

			extractDir, err := os.MkdirTemp("", "flatkv-archive-restore-*")
			if err != nil {
				return fmt.Errorf("create temp dir: %w", err)
			}
			defer func() { _ = os.RemoveAll(extractDir) }()

			manifest, err := extractFlatKVArchive(localArchive, extractDir)
			if err != nil {
				return err
			}
			chainID := cast.ToString(appOpts.Get(flags.FlagChainID))
			if chainID == "" {
				chainID = manifest.ChainID
			}
			if manifest.ChainID != chainID {
				return fmt.Errorf("archive chain ID %q does not match configured chain ID %q", manifest.ChainID, chainID)
			}
			appHash, err := hex.DecodeString(manifest.AppHash)
			if err != nil {
				return fmt.Errorf("decode manifest app hash: %w", err)
			}

			if err := installFlatKVArchive(extractDir, manifest, flatKVRoot, wasmDir, stateStoreDir, force); err != nil {
				return err
			}
			if err := tmstatesync.BootstrapFromRPC(
				cmd.Context(),
				serverCtx.Config,
				chainID,
				manifest.Height,
				appHash,
				rpcServers,
				trustHeight,
				trustHash,
				serverCtx.Config.StateSync.TrustPeriod,
			); err != nil {
				return err
			}
			fmt.Printf("Restored FlatKV archive height=%d app_hash=%s into %s and bootstrapped Tendermint state\n",
				manifest.Height, manifest.AppHash, flatKVRoot)

			skipVerify, err := cmd.Flags().GetBool("skip-content-verification")
			if err != nil {
				return fmt.Errorf("get skip-content-verification flag: %w", err)
			}
			rebuildSS, err := cmd.Flags().GetBool("rebuild-state-store")
			if err != nil {
				return fmt.Errorf("get rebuild-state-store flag: %w", err)
			}
			ssConfig := app.ParseSSConfigs(appOpts)
			// Rebuild only when the archive carries no state_store payload
			// (SC-only shape) and this node runs with state-store enabled.
			needRebuild := rebuildSS && ssConfig.Enable && !archiveHasStateStore(manifest)

			if skipVerify {
				fmt.Println("WARNING: --skip-content-verification set; installed FlatKV content was NOT " +
					"re-hashed against the checkpoint's committed LtHash. Content integrity now rests " +
					"entirely on the archive producer.")
			}
			if skipVerify && !needRebuild {
				return nil
			}

			store, err := openRestoredFlatKV(cmd.Context(), flatKVRoot, manifest.Height)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			// Content verification: recompute the LtHash from every installed
			// physical row and check it against the checkpoint's persisted
			// LtHash metadata. The manifest SHA-256s and the light-client
			// AppHash check above only cover metadata self-consistency; this
			// full scan is what binds the installed bytes to the commitment.
			// The remaining link — persisted LtHash to the light-client
			// verified AppHash — is enforced by the ABCI handshake on first
			// start (the app derives its reported hash from this same
			// metadata and Tendermint compares it to the bootstrapped
			// state.AppHash), so a mismatch on either link refuses to serve.
			if !skipVerify {
				start := time.Now()
				fmt.Println("Verifying installed FlatKV content against the checkpoint's committed LtHash (full scan)...")
				if err := flatkv.VerifyLtHash(store); err != nil {
					return fmt.Errorf("FlatKV content verification failed; the installed archive does not match "+
						"its own commitment metadata and must not be used: %w", err)
				}
				fmt.Printf("Verified FlatKV content (full LtHash re-scan) in %s\n", time.Since(start).Round(time.Second))
			}

			// The FlatKV checkpoint is the single source of truth: rebuild
			// the query store locally by iterating the now-verified
			// checkpoint. Runs after content verification so a bad archive
			// fails before the (long) rebuild.
			if needRebuild {
				if _, statErr := os.Lstat(stateStoreDir); statErr == nil {
					if !force {
						return fmt.Errorf("destination state-store dir %s exists (use --force to replace)", stateStoreDir)
					}
					if err := os.RemoveAll(stateStoreDir); err != nil {
						return fmt.Errorf("remove existing state-store dir: %w", err)
					}
				}
				start := time.Now()
				entries, err := rebuildStateStoreFromFlatKV(homeDir, store, manifest.Height, ssConfig)
				if err != nil {
					return fmt.Errorf("rebuild state_store from FlatKV: %w", err)
				}
				fmt.Printf("Rebuilt state_store at height %d from the FlatKV checkpoint (%d entries) in %s\n",
					manifest.Height, entries, time.Since(start).Round(time.Second))
			}
			return nil
		},
	}
	cmd.Flags().String("from", "", "Archive path or s3://bucket/key source")
	cmd.Flags().StringSlice("verification-rpc", nil, "RPC endpoints for light-client verification (repeat or comma-separate; at least two)")
	cmd.Flags().Int64("trust-height", 0, "Trusted block height for light-client verification")
	cmd.Flags().String("trust-hash", "", "Trusted block hash for light-client verification")
	cmd.Flags().Bool("force", false, "Replace existing FlatKV snapshot/current and wasm directories")
	cmd.Flags().String("flatkv-dir", "", "FlatKV root directory (default <home>/data/state_commit/flatkv)")
	cmd.Flags().String("wasm-dir", "", "Wasm directory to restore if archived (default <home>/wasm)")
	cmd.Flags().String("state-store-dir", "", "State-store query directory to restore or rebuild (default <home>/data/state_store)")
	cmd.Flags().Bool("rebuild-state-store", true, "Rebuild state_store at the archive height by iterating the restored FlatKV checkpoint (skipped when the archive carries a state_store payload or state-store is disabled)")
	cmd.Flags().Bool("skip-content-verification", false, "Skip the full LtHash re-scan that binds installed FlatKV content to the checkpoint's commitment (NOT recommended: content integrity then rests on the archive producer)")
	cmd.Flags().String(flags.FlagChainID, "", "Expected network chain ID")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("trust-height")
	_ = cmd.MarkFlagRequired("trust-hash")
	return cmd
}

func requireFlatKVOnly(writeMode, autoMode interface{}) error {
	if cast.ToBool(autoMode) {
		return fmt.Errorf("flatkv-archive requires %s=false and %s=flatkv_only", app.FlagSCWriteModeEnableAuto, app.FlagSCWriteMode)
	}
	if cast.ToString(writeMode) != "flatkv_only" {
		return fmt.Errorf("flatkv-archive requires %s=flatkv_only, got %q", app.FlagSCWriteMode, cast.ToString(writeMode))
	}
	return nil
}

// archiveHasStateStore reports whether the manifest carries a state_store
// payload (legacy full-archive shape).
func archiveHasStateStore(manifest *flatKVArchiveManifest) bool {
	for _, f := range manifest.Files {
		if strings.HasPrefix(f.Path, "state_store/") {
			return true
		}
	}
	return false
}

// openRestoredFlatKV opens the restored FlatKV checkpoint readonly and checks
// it loads at the archive height. The caller owns closing the returned store.
func openRestoredFlatKV(ctx context.Context, flatKVRoot string, height int64) (*flatkv.CommitStore, error) {
	fcfg := flatkvconfig.DefaultConfig()
	fcfg.DataDir = flatKVRoot
	base, err := flatkv.NewCommitStore(ctx, fcfg)
	if err != nil {
		return nil, fmt.Errorf("open flatkv store: %w", err)
	}
	loaded, err := base.LoadVersion(0, true)
	if err != nil {
		_ = base.Close()
		return nil, fmt.Errorf("load flatkv version: %w", err)
	}
	store, ok := loaded.(*flatkv.CommitStore)
	if !ok {
		_ = base.Close()
		return nil, fmt.Errorf("unexpected flatkv store type %T", loaded)
	}
	if v := store.Version(); v != height {
		_ = store.Close()
		return nil, fmt.Errorf("restored flatkv version %d does not match archive height %d", v, height)
	}
	return store, nil
}

// rebuildStateStoreFromFlatKV populates a fresh state_store at the archive
// height by iterating every committed physical row of the restored FlatKV
// checkpoint and feeding it through the same FlatKV-node conversion path the
// state sync SS importer uses. The resulting query store has exactly one
// version (the archive height); history accumulates from there as the node
// block-syncs forward.
func rebuildStateStoreFromFlatKV(
	homeDir string,
	store *flatkv.CommitStore,
	height int64,
	ssConfig seidbconfig.StateStoreConfig,
) (int64, error) {
	ssStore, err := ss.NewStateStore(homeDir, ssConfig)
	if err != nil {
		return 0, fmt.Errorf("open state store: %w", err)
	}

	var entries int64
	ch := make(chan dbtypes.SnapshotNode, 10000)
	iterDone := make(chan error, 1)
	go func() {
		defer close(ch)
		iter, err := store.RawGlobalIterator()
		if err != nil {
			iterDone <- fmt.Errorf("flatkv raw iterator: %w", err)
			return
		}
		defer func() { _ = iter.Close() }()
		for ; iter.Valid(); iter.Next() {
			// Emit under the FlatKV module header so the composite SS
			// importer runs its physical->logical conversion, exactly as it
			// does for a FlatKV-only state sync stream.
			ch <- dbtypes.SnapshotNode{
				StoreKey: keys.FlatKVStoreKey,
				Key:      append([]byte(nil), iter.Key()...),
				Value:    append([]byte(nil), iter.Value()...),
			}
			entries++
		}
		iterDone <- iter.Error()
	}()

	importErr := ssStore.Import(height, ch)
	iterErr := <-iterDone
	if importErr != nil {
		_ = ssStore.Close()
		return entries, fmt.Errorf("state store import: %w", importErr)
	}
	if iterErr != nil {
		_ = ssStore.Close()
		return entries, iterErr
	}
	// Mirror the state sync restore path: stamp the version watermarks so the
	// store reports the archive height instead of 0 until the first commit.
	if err := ssStore.SetEarliestVersion(height, false); err != nil {
		_ = ssStore.Close()
		return entries, fmt.Errorf("set earliest version: %w", err)
	}
	if err := ssStore.SetLatestVersion(height); err != nil {
		_ = ssStore.Close()
		return entries, fmt.Errorf("set latest version: %w", err)
	}
	if err := ssStore.Close(); err != nil {
		return entries, fmt.Errorf("close state store: %w", err)
	}
	return entries, nil
}

// selectStateStoreSource picks the newest online state-store checkpoint under
// <stateStoreDir>/snapshots. When no checkpoint exists (including when the
// state-store directory itself is absent) it returns the live directory with
// an empty snapshot name, which is only safe to pack from a stopped donor.
func selectStateStoreSource(stateStoreDir string) (string, string, int64, error) {
	root := filepath.Join(stateStoreDir, sscomposite.CheckpointsDirName)
	versions, err := sscomposite.ListCheckpointVersions(root)
	if err != nil {
		return "", "", 0, err
	}
	if len(versions) == 0 {
		return stateStoreDir, "", 0, nil
	}
	newest := versions[len(versions)-1]
	name := sscomposite.CheckpointDirName(newest)
	return filepath.Join(root, name), name, newest, nil
}

// selectFlatKVSnapshotAtMost picks the newest FlatKV snapshot with height <=
// maxHeight. Pairing the archive's FlatKV height H with a state-store
// checkpoint labeled >= H guarantees the restored query store has every
// version <= H; anything above H is refilled by block replay.
func selectFlatKVSnapshotAtMost(flatKVRoot string, maxHeight int64) (string, string, int64, error) {
	entries, err := os.ReadDir(flatKVRoot)
	if err != nil {
		return "", "", 0, fmt.Errorf("read FlatKV root %q: %w", flatKVRoot, err)
	}
	best := int64(-1)
	bestName := ""
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), flatKVArchiveSnapshotPref) {
			continue
		}
		h, err := strconv.ParseInt(strings.TrimPrefix(entry.Name(), flatKVArchiveSnapshotPref), 10, 64)
		if err != nil {
			continue
		}
		if h <= maxHeight && h > best {
			best = h
			bestName = entry.Name()
		}
	}
	if best < 0 {
		return "", "", 0, fmt.Errorf(
			"no FlatKV snapshot at height <= state-store checkpoint version %d in %q; wait for the next state-store checkpoint",
			maxHeight, flatKVRoot)
	}
	return filepath.Join(flatKVRoot, bestName), bestName, best, nil
}

func selectFlatKVSnapshot(flatKVRoot string) (string, string, int64, error) {
	target, err := os.Readlink(filepath.Join(flatKVRoot, "current"))
	if err != nil {
		return "", "", 0, fmt.Errorf("read FlatKV current symlink: %w", err)
	}
	if strings.Contains(target, string(os.PathSeparator)) {
		target = filepath.Base(target)
	}
	if !strings.HasPrefix(target, flatKVArchiveSnapshotPref) {
		return "", "", 0, fmt.Errorf("FlatKV current target %q is not a snapshot directory", target)
	}
	height, err := strconv.ParseInt(strings.TrimPrefix(target, flatKVArchiveSnapshotPref), 10, 64)
	if err != nil {
		return "", "", 0, fmt.Errorf("parse FlatKV snapshot height from %q: %w", target, err)
	}
	dir := filepath.Join(flatKVRoot, target)
	if info, err := os.Stat(dir); err != nil {
		return "", "", 0, fmt.Errorf("stat FlatKV snapshot dir %q: %w", dir, err)
	} else if !info.IsDir() {
		return "", "", 0, fmt.Errorf("FlatKV snapshot path %q is not a directory", dir)
	}
	return dir, target, height, nil
}

func queryArchivedAppHash(ctx context.Context, rpc string, height int64) (string, string, error) {
	if rpc == "" {
		return "", "", fmt.Errorf("rpc endpoint is required")
	}
	nextHeight := height + 1
	endpoint := strings.TrimRight(rpc, "/") + "/block?height=" + strconv.FormatInt(nextHeight, 10)
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", "", fmt.Errorf("build RPC request: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("query RPC block %d: %w", nextHeight, err)
		} else {
			chainID, appHash, err := decodeBlockAppHash(resp, nextHeight)
			if err == nil {
				return chainID, appHash, nil
			}
			lastErr = err
		}
		if time.Now().After(deadline) {
			return "", "", lastErr
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func decodeBlockAppHash(resp *http.Response, height int64) (string, string, error) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", "", fmt.Errorf("RPC block %d returned %s: %s", height, resp.Status, string(body))
	}
	var out tendermintBlockResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", fmt.Errorf("decode RPC block response: %w", err)
	}
	chainID := out.Result.Block.Header.ChainID
	appHash := strings.ToUpper(out.Result.Block.Header.AppHash)
	if chainID == "" && appHash == "" {
		chainID = out.Block.Header.ChainID
		appHash = strings.ToUpper(out.Block.Header.AppHash)
	}
	if chainID == "" || appHash == "" {
		return "", "", fmt.Errorf("RPC block %d did not include chain_id/app_hash", height)
	}
	return chainID, appHash, nil
}

type flatKVArchiveSource struct {
	archivePath string
	sourcePath  string
	info        os.FileInfo
}

func buildFlatKVArchiveManifest(
	chainID string,
	height int64,
	appHash string,
	snapshotName string,
	snapshotDir string,
	wasmDir string,
	stateStoreDir string,
) (*flatKVArchiveManifest, []flatKVArchiveSource, error) {
	var sources []flatKVArchiveSource
	manifest := &flatKVArchiveManifest{
		FormatVersion: flatKVArchiveFormatVersion,
		ChainID:       chainID,
		Height:        height,
		AppHash:       strings.ToUpper(appHash),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		SnapshotName:  snapshotName,
	}
	if err := collectArchiveSources(snapshotDir, filepath.ToSlash(filepath.Join("flatkv", snapshotName)), &sources); err != nil {
		return nil, nil, err
	}
	if info, err := os.Stat(wasmDir); err == nil && info.IsDir() {
		if err := collectArchiveSources(wasmDir, "wasm", &sources); err != nil {
			return nil, nil, err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("stat wasm dir %q: %w", wasmDir, err)
	}
	if info, err := os.Stat(stateStoreDir); err == nil && info.IsDir() {
		if err := collectArchiveSources(stateStoreDir, "state_store", &sources); err != nil {
			return nil, nil, err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("stat state-store dir %q: %w", stateStoreDir, err)
	}
	for _, src := range sources {
		sum, err := sha256File(src.sourcePath)
		if err != nil {
			return nil, nil, err
		}
		manifest.Files = append(manifest.Files, flatKVArchiveFileEntry{
			Path:   src.archivePath,
			Size:   src.info.Size(),
			Mode:   int64(src.info.Mode().Perm()),
			SHA256: hex.EncodeToString(sum),
		})
	}
	return manifest, sources, nil
}

func collectArchiveSources(root string, archivePrefix string, sources *[]flatKVArchiveSource) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && strings.Contains(filepath.ToSlash(archivePrefix), "state_store") &&
			(info.Name() == "changelog" || info.Name() == sscomposite.CheckpointsDirName) {
			// Live WAL segments rotate underfoot and online checkpoints are
			// packed explicitly (or not at all); neither belongs in the
			// state_store payload.
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported non-regular file in archive source: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		archivePath := filepath.ToSlash(filepath.Join(archivePrefix, rel))
		*sources = append(*sources, flatKVArchiveSource{
			archivePath: archivePath,
			sourcePath:  path,
			info:        info,
		})
		return nil
	})
}

func writeFlatKVArchive(path string, manifest *flatKVArchiveManifest, sources []flatKVArchiveSource) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil && filepath.Dir(path) != "." {
		return fmt.Errorf("create archive parent dir: %w", err)
	}
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create archive %q: %w", path, err)
	}
	defer func() { _ = out.Close() }()

	zw, err := zstd.NewWriter(out)
	if err != nil {
		return fmt.Errorf("create zstd writer: %w", err)
	}
	defer func() { _ = zw.Close() }()
	tw := tar.NewWriter(zw)
	defer func() { _ = tw.Close() }()

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := tw.WriteHeader(&tar.Header{
		Name: flatKVArchiveManifestName,
		Mode: 0644,
		Size: int64(len(manifestBytes)),
	}); err != nil {
		return fmt.Errorf("write manifest header: %w", err)
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	for _, src := range sources {
		if err := tw.WriteHeader(&tar.Header{
			Name: src.archivePath,
			Mode: int64(src.info.Mode().Perm()),
			Size: src.info.Size(),
		}); err != nil {
			return fmt.Errorf("write tar header for %s: %w", src.archivePath, err)
		}
		in, err := os.Open(filepath.Clean(src.sourcePath))
		if err != nil {
			return fmt.Errorf("open %s: %w", src.sourcePath, err)
		}
		if _, err := io.Copy(tw, in); err != nil {
			_ = in.Close()
			return fmt.Errorf("archive %s: %w", src.sourcePath, err)
		}
		if err := in.Close(); err != nil {
			return fmt.Errorf("close %s: %w", src.sourcePath, err)
		}
	}
	return nil
}

func materializeArchive(ctx context.Context, from string) (string, func(), error) {
	if strings.HasPrefix(from, "s3://") {
		tmp, err := os.CreateTemp("", "flatkv-archive-*.tar.zst")
		if err != nil {
			return "", func() {}, fmt.Errorf("create temp archive: %w", err)
		}
		tmpPath := tmp.Name()
		if err := tmp.Close(); err != nil {
			return "", func() { _ = os.Remove(tmpPath) }, err
		}
		if err := downloadFileFromS3(ctx, from, tmpPath); err != nil {
			_ = os.Remove(tmpPath)
			return "", func() {}, err
		}
		return tmpPath, func() { _ = os.Remove(tmpPath) }, nil
	}
	return from, func() {}, nil
}

func extractFlatKVArchive(path string, dest string) (*flatKVArchiveManifest, error) {
	in, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open archive %q: %w", path, err)
	}
	defer func() { _ = in.Close() }()
	zr, err := zstd.NewReader(in)
	if err != nil {
		return nil, fmt.Errorf("open zstd archive: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)

	var manifest *flatKVArchiveManifest
	hashes := make(map[string]hashingWriter)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		cleanName, err := safeArchivePath(hdr.Name)
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("unsupported tar entry type for %s", hdr.Name)
		}
		if cleanName == flatKVArchiveManifestName {
			buf, err := io.ReadAll(io.LimitReader(tr, 16<<20))
			if err != nil {
				return nil, fmt.Errorf("read manifest: %w", err)
			}
			var m flatKVArchiveManifest
			if err := json.Unmarshal(buf, &m); err != nil {
				return nil, fmt.Errorf("decode manifest: %w", err)
			}
			manifest = &m
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(cleanName))
		if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			return nil, fmt.Errorf("create parent for %s: %w", cleanName, err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
		if err != nil {
			return nil, fmt.Errorf("create extracted file %s: %w", cleanName, err)
		}
		hw := hashingWriter{h: sha256.New()}
		_, copyErr := io.Copy(io.MultiWriter(out, hw.h), tr)
		closeErr := out.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("extract %s: %w", cleanName, copyErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close extracted file %s: %w", cleanName, closeErr)
		}
		hashes[cleanName] = hw
	}
	if manifest == nil {
		return nil, fmt.Errorf("archive missing %s", flatKVArchiveManifestName)
	}
	if manifest.FormatVersion != flatKVArchiveFormatVersion {
		return nil, fmt.Errorf("unsupported archive format version %d", manifest.FormatVersion)
	}
	listed := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		listed[file.Path] = struct{}{}
		hw, ok := hashes[file.Path]
		if !ok {
			return nil, fmt.Errorf("archive missing manifest file %s", file.Path)
		}
		if got := hex.EncodeToString(hw.h.Sum(nil)); got != strings.ToLower(file.SHA256) {
			return nil, fmt.Errorf("sha256 mismatch for %s: got %s want %s", file.Path, got, file.SHA256)
		}
	}
	// Verification must be bidirectional: an entry present in the archive but
	// absent from the manifest would otherwise be extracted and installed
	// (the install step renames whole directories) without any hash covering
	// it. Reject unlisted entries outright.
	for name := range hashes {
		if _, ok := listed[name]; !ok {
			return nil, fmt.Errorf("archive entry %s is not listed in the manifest", name)
		}
	}
	return manifest, nil
}

type hashingWriter struct {
	h hashHash
}

type hashHash interface {
	io.Writer
	Sum([]byte) []byte
}

func safeArchivePath(name string) (string, error) {
	name = filepath.ToSlash(name)
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "../") || name == ".." {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return clean, nil
}

func installFlatKVArchive(
	extractDir string,
	manifest *flatKVArchiveManifest,
	flatKVRoot string,
	wasmDir string,
	stateStoreDir string,
	force bool,
) error {
	srcSnapshot := filepath.Join(extractDir, "flatkv", manifest.SnapshotName)
	if info, err := os.Stat(srcSnapshot); err != nil {
		return fmt.Errorf("archive missing FlatKV snapshot %q: %w", manifest.SnapshotName, err)
	} else if !info.IsDir() {
		return fmt.Errorf("archive FlatKV snapshot %q is not a directory", manifest.SnapshotName)
	}
	if err := os.MkdirAll(flatKVRoot, 0750); err != nil {
		return fmt.Errorf("create FlatKV root: %w", err)
	}
	destSnapshot := filepath.Join(flatKVRoot, manifest.SnapshotName)
	if force {
		_ = os.RemoveAll(destSnapshot)
		_ = os.Remove(filepath.Join(flatKVRoot, "current"))
		_ = os.Remove(filepath.Join(flatKVRoot, "current-tmp"))
		_ = os.RemoveAll(filepath.Join(flatKVRoot, "working"))
		_ = os.RemoveAll(filepath.Join(flatKVRoot, "changelog"))
	} else if _, err := os.Lstat(destSnapshot); err == nil {
		return fmt.Errorf("destination snapshot %s exists (use --force to replace)", destSnapshot)
	}
	if err := os.Rename(srcSnapshot, destSnapshot); err != nil {
		return fmt.Errorf("install FlatKV snapshot: %w", err)
	}
	tmpLink := filepath.Join(flatKVRoot, "current-tmp")
	_ = os.Remove(tmpLink)
	if err := os.Symlink(manifest.SnapshotName, tmpLink); err != nil {
		return fmt.Errorf("create FlatKV current symlink: %w", err)
	}
	if err := os.Rename(tmpLink, filepath.Join(flatKVRoot, "current")); err != nil {
		return fmt.Errorf("activate FlatKV current symlink: %w", err)
	}

	srcWasm := filepath.Join(extractDir, "wasm")
	if info, err := os.Stat(srcWasm); err == nil && info.IsDir() {
		if force {
			_ = os.RemoveAll(wasmDir)
		} else if _, err := os.Lstat(wasmDir); err == nil {
			return fmt.Errorf("destination wasm dir %s exists (use --force to replace)", wasmDir)
		}
		if err := os.MkdirAll(filepath.Dir(wasmDir), 0750); err != nil {
			return fmt.Errorf("create wasm parent: %w", err)
		}
		if err := os.Rename(srcWasm, wasmDir); err != nil {
			return fmt.Errorf("install wasm dir: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat extracted wasm dir: %w", err)
	}
	srcStateStore := filepath.Join(extractDir, "state_store")
	if info, err := os.Stat(srcStateStore); err == nil && info.IsDir() {
		if force {
			_ = os.RemoveAll(stateStoreDir)
		} else if _, err := os.Lstat(stateStoreDir); err == nil {
			return fmt.Errorf("destination state-store dir %s exists (use --force to replace)", stateStoreDir)
		}
		if err := os.MkdirAll(filepath.Dir(stateStoreDir), 0750); err != nil {
			return fmt.Errorf("create state-store parent: %w", err)
		}
		if err := os.Rename(srcStateStore, stateStoreDir); err != nil {
			return fmt.Errorf("install state-store dir: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat extracted state-store dir: %w", err)
	}
	return nil
}

func sha256File(path string) ([]byte, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open %s for sha256: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("hash %s: %w", path, err)
	}
	return h.Sum(nil), nil
}

func uploadFileToS3(ctx context.Context, localPath string, dest string) error {
	bucket, key, err := parseS3URI(dest)
	if err != nil {
		return err
	}
	f, err := os.Open(filepath.Clean(localPath))
	if err != nil {
		return fmt.Errorf("open upload file: %w", err)
	}
	defer func() { _ = f.Close() }()
	sess, err := session.NewSession()
	if err != nil {
		return fmt.Errorf("create AWS session: %w", err)
	}
	_, err = s3manager.NewUploader(sess).UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   f,
	})
	if err != nil {
		return fmt.Errorf("upload %s to %s: %w", localPath, dest, err)
	}
	return nil
}

func downloadFileFromS3(ctx context.Context, src string, localPath string) error {
	bucket, key, err := parseS3URI(src)
	if err != nil {
		return err
	}
	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create download file: %w", err)
	}
	defer func() { _ = f.Close() }()
	sess, err := session.NewSession()
	if err != nil {
		return fmt.Errorf("create AWS session: %w", err)
	}
	_, err = s3manager.NewDownloader(sess).DownloadWithContext(ctx, f, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("download %s: %w", src, err)
	}
	return nil
}

func parseS3URI(uri string) (string, string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", fmt.Errorf("parse S3 URI: %w", err)
	}
	if u.Scheme != "s3" || u.Host == "" || strings.TrimPrefix(u.Path, "/") == "" {
		return "", "", fmt.Errorf("invalid S3 URI %q; expected s3://bucket/key", uri)
	}
	return u.Host, strings.TrimPrefix(u.Path, "/"), nil
}
