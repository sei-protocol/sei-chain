package operations

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

const (
	flatkvBucketAccount = "account"
	flatkvBucketCode    = "code"
	flatkvBucketStorage = "storage"
	flatkvBucketLegacy  = "legacy"
)

// flatkvBucketOrder lists the logical bucket names for dump output files.
// RawGlobalIterator emits keys in global lex order; this order is used only
// for CLI validation and per-bucket file allocation.
var flatkvBucketOrder = []string{flatkvBucketAccount, flatkvBucketCode, flatkvBucketStorage, flatkvBucketLegacy}

// DumpFlatKVCmd dumps every (physical key, value) pair of a FlatKV store
// into per-bucket files, formatted to match dump-iavl so the same diff
// tooling works on both.
func DumpFlatKVCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-flatkv",
		Short: "Iterate and dump physical FlatKV (key, value) pairs into per-bucket files",
		Run:   executeDumpFlatKV,
	}
	cmd.PersistentFlags().StringP("db-dir", "d", "", "FlatKV database directory")
	cmd.PersistentFlags().StringP("output-dir", "o", "", "Output directory (one file per bucket)")
	cmd.PersistentFlags().Int64("height", 0, "FlatKV target version; 0 selects the latest available version")
	cmd.PersistentFlags().StringP("bucket", "b", "", "Restrict dump to a single bucket (account|code|storage|legacy). Default: all buckets")
	return cmd
}

func executeDumpFlatKV(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	height, _ := cmd.Flags().GetInt64("height")
	bucket, _ := cmd.Flags().GetString("bucket")

	if dbDir == "" {
		panic("Must provide --db-dir pointing at a FlatKV data directory")
	}
	if outputDir == "" {
		panic("Must provide --output-dir")
	}
	if bucket != "" && !isFlatKVBucket(bucket) {
		panic(fmt.Sprintf("Unknown --bucket %q. Valid: account, code, storage, legacy", bucket))
	}

	if err := DumpFlatKVData(dbDir, outputDir, height, bucket); err != nil {
		panic(err)
	}
}

func isFlatKVBucket(name string) bool {
	for _, b := range flatkvBucketOrder {
		if b == name {
			return true
		}
	}
	return false
}

// DumpFlatKVData opens a read-only clone of a FlatKV store at the requested
// version and writes every (physical key, value) pair into per-bucket files
// under outputDir. Each file mirrors the dump-iavl format so downstream
// diff tooling can be shared:
//
//	Bucket <name> at version <V>
//	Key: <HEX>, Value: <HEX>
//	...
//
// Physical keys are emitted verbatim, including their "<module>/" + type
// prefix header, because they are not byte-for-byte comparable with
// memIAVL logical keys anyway (different type prefixes per domain). The
// FlatKV metadataDB and the per-DB _meta/* rows are intentionally excluded:
// they are internal bookkeeping and RawGlobalIterator already filters the
// per-DB ones for us.
func DumpFlatKVData(dbDir, outputDir string, height int64, bucket string) error {
	store, err := openFlatKVReadOnly(dbDir, height)
	if err != nil {
		return fmt.Errorf("open flatkv read-only: %w", err)
	}
	defer func() { _ = store.Close() }()

	version := store.Version()
	fmt.Printf("Opened FlatKV at version %d\n", version)

	return dumpFlatKVFromStore(store.CommitStore, outputDir, version, bucket)
}

// dumpFlatKVFromStore is the core scan+write path, split out so tests can
// exercise it against an in-memory store without going through the
// snapshot clone machinery used by the CLI.
func dumpFlatKVFromStore(store *flatkv.CommitStore, outputDir string, version int64, bucket string) error {
	if err := os.MkdirAll(outputDir, fs.ModePerm); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	files, writers, err := openBucketWriters(outputDir, version, bucket)
	if err != nil {
		return err
	}
	defer func() {
		for _, w := range writers {
			if w != nil {
				_ = w.Flush()
			}
		}
		for _, f := range files {
			if f != nil {
				_ = f.Close()
			}
		}
	}()

	iter := store.RawGlobalIterator()
	defer func() { _ = iter.Close() }()

	counts := make(map[string]uint64, len(flatkvBucketOrder))
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		val := iter.Value()
		bucketName := classifyFlatKVPhysicalKey(key)
		if w := writers[bucketName]; w != nil {
			if _, werr := fmt.Fprintf(w, "Key: %X, Value: %X\n", key, val); werr != nil {
				return fmt.Errorf("write %s: %w", bucketName, werr)
			}
			counts[bucketName]++
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterate: %w", err)
	}

	for _, name := range flatkvBucketOrder {
		if writers[name] == nil {
			continue
		}
		fmt.Printf("Bucket %s: %d keys dumped\n", name, counts[name])
	}
	if err := flushAndCloseBucketWriters(files, writers); err != nil {
		files = nil
		writers = nil
		return err
	}
	files = nil
	writers = nil
	return nil
}

func flushAndCloseBucketWriters(files map[string]*os.File, writers map[string]*bufio.Writer) error {
	var firstErr error
	for _, name := range flatkvBucketOrder {
		w := writers[name]
		if w == nil {
			continue
		}
		if err := w.Flush(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("flush %s: %w", name, err)
		}
	}
	for _, name := range flatkvBucketOrder {
		f := files[name]
		if f == nil {
			continue
		}
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close %s: %w", name, err)
		}
	}
	return firstErr
}

// openBucketWriters creates per-bucket output files inside outputDir. When
// bucket != "" only that bucket's writer is populated; unselected buckets
// are absent from the returned maps, which the scan loop treats as "skip
// writes for this key but keep iterating" over the full merged keyspace.
func openBucketWriters(outputDir string, version int64, bucket string) (map[string]*os.File, map[string]*bufio.Writer, error) {
	files := make(map[string]*os.File, len(flatkvBucketOrder))
	writers := make(map[string]*bufio.Writer, len(flatkvBucketOrder))
	for _, name := range flatkvBucketOrder {
		if bucket != "" && bucket != name {
			continue
		}
		f, err := utils.CreateFile(outputDir, name)
		if err != nil {
			for _, existing := range files {
				_ = existing.Close()
			}
			return nil, nil, fmt.Errorf("create %s: %w", name, err)
		}
		bw := bufio.NewWriterSize(f, 1<<20)
		if _, err := fmt.Fprintf(bw, "Bucket %s at version %d\n", name, version); err != nil {
			_ = bw.Flush()
			_ = f.Close()
			for _, existing := range files {
				_ = existing.Close()
			}
			return nil, nil, fmt.Errorf("write header %s: %w", name, err)
		}
		files[name] = f
		writers[name] = bw
	}
	return files, writers, nil
}
