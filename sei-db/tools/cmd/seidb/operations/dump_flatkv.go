package operations

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/tools/utils"
	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
)

const (
	// defaultReadLimitMiBps is the default read-throughput ceiling for the
	// scan, in MiB/s. The dump opens an independent read-only clone but still
	// shares the underlying disk (and page cache) with a running node, so an
	// unthrottled full scan can starve block processing of read bandwidth /
	// IOPS. 64 MiB/s is a deliberately conservative default: negligible on
	// local-NVMe nodes (GB/s class) yet still finishes a few-hundred-GiB scan
	// in well under a couple of hours. Lower it on provisioned-throughput EBS
	// (e.g. gp3 with a ~125 MB/s baseline); set 0 to disable throttling.
	defaultReadLimitMiBps = 64.0

	// minReadBurstBytes is the floor for the limiter burst so a single large
	// row (e.g. ~24 KiB of contract bytecode) never exceeds the burst and
	// stalls the limiter. Also keeps very low --read-limit-mb values usable.
	minReadBurstBytes = 4 << 20

	bytesPerMiB = 1 << 20

	flatkvMetadataDir = "metadata"
)

const (
	flatkvBucketAccount = "account"
	flatkvBucketCode    = "code"
	flatkvBucketStorage = "storage"
	flatkvBucketMisc    = "misc"
)

// flatkvBucketOrder lists the logical bucket names for dump output files.
// RawGlobalIterator emits keys in global lex order; this order is used only
// for CLI validation and per-bucket file allocation.
var flatkvBucketOrder = []string{flatkvBucketAccount, flatkvBucketCode, flatkvBucketStorage, flatkvBucketMisc}

// DumpFlatKVCmd dumps every (physical key, value) pair of a FlatKV store into
// per-bucket files, formatted to match dump-iavl so the same diff tooling works
// on both. It optionally also computes the per-bucket and total LtHash (lattice
// hash) of the scanned state and verifies that total against the committed root
// recorded in snapshot metadata.
//
// It opens an independent read-only clone of the store (snapshot hard-linked +
// changelog WAL replayed into a temp dir under db-dir), so it is safe to run
// against a live, block-producing node. The scan is throttled by --read-limit-mb
// to avoid starving the node of disk bandwidth.
//
// Flags:
//
//	-d, --db-dir        FlatKV data dir (the dir containing current/, snapshot-*,
//	                    changelog/). Required.
//	-o, --output-dir    Where to write per-bucket dump files (one file per bucket).
//	                    Required.
//	    --height        Target version. 0 (default) selects the latest available
//	                    version (replays the WAL to the tip).
//	-b, --bucket        Restrict the on-disk dump to a single bucket
//	                    (account|code|storage|misc). Default: all buckets.
//	                    NOTE: this only filters which hex files are WRITTEN; the
//	                    full keyspace is always scanned, and --lthash always
//	                    covers all four buckets, so the LtHash total stays valid.
//	    --lthash        Compute per-bucket + total LtHash and verify the total
//	                    against committed snapshot metadata. Default: true.
//	    --lthash-only   Compute and verify LtHash without writing any KV dump
//	                    files. Requires --lthash=true and ignores --output-dir.
//	    --read-limit-mb Throttle the scan to at most this many MiB/s of
//	                    (key+value) bytes read. Default: 64. 0 = unlimited.
//	                    Keep it low (default or less) on a shared/live node;
//	                    raise it only for offline runs on idle disks.
//
// Output: per-bucket "Key: <HEX>, Value: <HEX>" files, then (with --lthash) a
// "LtHash (lattice hash)" block listing each bucket's count + checksum and the
// TOTAL, followed by a "LtHash verification vs snapshot metadata" PASS/FAIL line
// (non-zero exit on mismatch).
//
// Examples:
//
//	# Latest version, all buckets, default 64 MiB/s throttle, LtHash + verify.
//	seidb dump-flatkv -d /.sei/data/state_commit/flatkv -o /tmp/flatkv-dump
//
//	# LtHash + verify only, minimizing disk writes by writing just one small
//	# bucket (the scan + total LtHash still cover all buckets).
//	seidb dump-flatkv -d /.sei/data/state_commit/flatkv -o /tmp/flatkv-dump \
//	    --bucket code
//
//	# Pin a specific height.
//	seidb dump-flatkv -d /.sei/data/state_commit/flatkv -o /tmp/flatkv-dump \
//	    --height 216890000
//
//	# Offline / idle disk: go full speed, skip LtHash.
//	seidb dump-flatkv -d /.sei/data/state_commit/flatkv -o /tmp/flatkv-dump \
//	    --read-limit-mb 0 --lthash=false
//
//	# Verify the FlatKV lattice hash only, without writing key/value dump files.
//	seidb dump-flatkv -d /.sei/data/state_commit/flatkv --lthash-only
//
//	# Against a running node in Kubernetes (build a static linux/amd64 binary,
//	# copy it in, run in the background, then read the result):
//	#   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/seidb ./sei-db/tools/cmd/seidb
//	#   kubectl cp /tmp/seidb <ns>/<pod>:/tmp/seidb -c seid
//	#   kubectl exec -n <ns> <pod> -c seid -- sh -lc '\
//	#     nohup /tmp/seidb dump-flatkv -d /.sei/data/state_commit/flatkv \
//	#       -o /.sei/data/flatkv-dump --bucket code > /tmp/dump.log 2>&1 &'
func DumpFlatKVCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-flatkv",
		Short: "Iterate and dump physical FlatKV (key, value) pairs into per-bucket files",
		Run:   executeDumpFlatKV,
	}
	cmd.PersistentFlags().StringP("db-dir", "d", "", "FlatKV database directory")
	cmd.PersistentFlags().StringP("output-dir", "o", "", "Output directory (one file per bucket)")
	cmd.PersistentFlags().Int64("height", 0, "FlatKV target version; 0 selects the latest available version")
	cmd.PersistentFlags().StringP("bucket", "b", "", "Restrict dump to a single bucket (account|code|storage|misc). Default: all buckets")
	cmd.PersistentFlags().Bool("lthash", true, "Also compute per-bucket and total LtHash (lattice hash) over the scanned state. Computed for all buckets regardless of --bucket so the total matches the node's committed LtHash")
	cmd.PersistentFlags().Bool("lthash-only", false, "Only compute and verify LtHash; do not write bucket dump files. Requires --lthash=true and does not require --output-dir")
	cmd.PersistentFlags().Float64("read-limit-mb", defaultReadLimitMiBps, "Throttle the scan to at most this many MiB/s of (key+value) bytes read, so a dump against a running node does not starve the chain of disk bandwidth. 0 = unlimited")
	return cmd
}

func executeDumpFlatKV(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	height, _ := cmd.Flags().GetInt64("height")
	bucket, _ := cmd.Flags().GetString("bucket")
	withLtHash, _ := cmd.Flags().GetBool("lthash")
	lthashOnly, _ := cmd.Flags().GetBool("lthash-only")
	readLimitMiBps, _ := cmd.Flags().GetFloat64("read-limit-mb")

	if dbDir == "" {
		panic("Must provide --db-dir pointing at a FlatKV data directory")
	}
	if outputDir == "" && !lthashOnly {
		panic("Must provide --output-dir")
	}
	if bucket != "" && !isFlatKVBucket(bucket) {
		panic(fmt.Sprintf("Unknown --bucket %q. Valid: account, code, storage, misc", bucket))
	}
	if lthashOnly && !withLtHash {
		panic("--lthash-only requires --lthash=true")
	}
	if lthashOnly && bucket != "" {
		panic("--bucket cannot be used with --lthash-only (LtHash always covers all buckets)")
	}
	if readLimitMiBps < 0 {
		panic("--read-limit-mb must be >= 0 (0 = unlimited)")
	}

	if err := DumpFlatKVData(dbDir, outputDir, height, bucket, withLtHash, lthashOnly, readLimitMiBps); err != nil {
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
func DumpFlatKVData(dbDir, outputDir string, height int64, bucket string, withLtHash bool, lthashOnly bool, readLimitMiBps float64) error {
	// Determine, before the main scan, whether the snapshot selected for this
	// height carries an LtHash watermark. CommittedRootHash() on the opened
	// store cannot tell a full-state hash apart from a partial WAL-deltas-only
	// hash, so we check the selected snapshot's metadata DB directly. See
	// snapshotCommittedLtHashIsFullState.
	committedIsFullState := true
	if withLtHash {
		var probeErr error
		committedIsFullState, probeErr = snapshotCommittedLtHashIsFullState(dbDir, height)
		if probeErr != nil {
			return fmt.Errorf("probe snapshot lthash watermark: %w", probeErr)
		}
	}

	store, err := openFlatKVReadOnly(dbDir, height)
	if err != nil {
		return fmt.Errorf("open flatkv read-only: %w", err)
	}
	defer func() { _ = store.Close() }()

	version := store.Version()
	fmt.Printf("Opened FlatKV at version %d\n", version)

	return dumpFlatKVFromStore(store.CommitStore, outputDir, version, bucket, withLtHash, lthashOnly, committedIsFullState, readLimitMiBps)
}

// snapshotMetadataMakesCommittedHashFullState decides, from the snapshot's own
// version and whether its metadata DB contains a global LtHash watermark,
// whether a store opened on top of that snapshot will carry a full-state
// committed LtHash (verifiable against a full re-scan).
//
//   - snapshotVersion == 0: a genesis/empty baseline contributes nothing, so
//     the committed hash is built entirely from replayed history and is
//     full-state.
//   - snapshotVersion > 0 without MetaLtHashKey: the snapshot had committed
//     data but no LtHash watermark, so once any WAL replays on top the
//     committed hash becomes a partial (deltas-only) hash. Not full-state.
//   - snapshotVersion > 0 with MetaLtHashKey present (even if the stored
//     checksum is all-zero): the snapshot's watermark exists and seeds the
//     committed hash. Full-state.
func snapshotMetadataMakesCommittedHashFullState(snapshotVersion int64, hasLtHashMetadata bool) bool {
	if snapshotVersion == 0 {
		return true
	}
	return hasLtHashMetadata
}

// snapshotCommittedLtHashIsFullState probes the FlatKV snapshot selected for
// height and reports whether a store opened on top of it will have a
// full-state committed LtHash. It checks the selected snapshot's metadata DB
// for ktype.MetaLtHashKey directly instead of using CommittedRootHash(): a
// legitimate LtHash watermark may be all-zero, so hash value alone cannot
// distinguish "metadata present" from "metadata absent".
func snapshotCommittedLtHashIsFullState(dbDir string, height int64) (bool, error) {
	snapshotName, err := selectFlatKVSnapshot(dbDir, height)
	if err != nil {
		return false, fmt.Errorf("select snapshot: %w", err)
	}
	snapshotVersion, err := strconv.ParseInt(snapshotName[len(flatkvSnapshotPrefix):], 10, 64)
	if err != nil {
		return false, fmt.Errorf("parse snapshot version from %q: %w", snapshotName, err)
	}
	if snapshotVersion == 0 {
		return true, nil
	}

	hasMetadata, err := selectedSnapshotHasLtHashMetadata(dbDir, snapshotName)
	if err != nil {
		return false, err
	}
	return snapshotMetadataMakesCommittedHashFullState(snapshotVersion, hasMetadata), nil
}

// selectedSnapshotHasLtHashMetadata checks whether the selected immutable
// snapshot's metadata DB contains the global LtHash watermark key. The source
// snapshot is not opened directly: Pebble would create lock/log files. Instead
// we hardlink-clone just the metadata DB into a temp dir under dbDir, open that
// clone, and read ktype.MetaLtHashKey.
func selectedSnapshotHasLtHashMetadata(dbDir, snapshotName string) (bool, error) {
	srcMetadataDir := filepath.Join(dbDir, snapshotName, flatkvMetadataDir)
	if _, err := os.Stat(srcMetadataDir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat snapshot metadata dir %s: %w", srcMetadataDir, err)
	}

	tempDir, err := os.MkdirTemp(dbDir, ".seidb-flatkv-meta-probe-*")
	if err != nil {
		return false, fmt.Errorf("create metadata probe dir under %s: %w", dbDir, err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	probeMetadataDir := filepath.Join(tempDir, flatkvMetadataDir)
	if err := cloneDirRecursive(srcMetadataDir, probeMetadataDir); err != nil {
		return false, fmt.Errorf("clone snapshot metadata %s: %w", srcMetadataDir, err)
	}

	cfg := pebbledb.DefaultConfig()
	cfg.DataDir = probeMetadataDir
	cfg.EnableMetrics = false
	db, err := pebbledb.Open(context.Background(), &cfg)
	if err != nil {
		return false, fmt.Errorf("open cloned snapshot metadata %s: %w", probeMetadataDir, err)
	}
	defer func() { _ = db.Close() }()

	_, err = db.Get(ktype.MetaLtHashKey)
	if errorutils.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read snapshot LtHash metadata key: %w", err)
	}
	return true, nil
}

// dumpFlatKVFromStore is the core scan+write path, split out so tests can
// exercise it against an in-memory store without going through the
// snapshot clone machinery used by the CLI.
func dumpFlatKVFromStore(store *flatkv.CommitStore, outputDir string, version int64, bucket string, withLtHash bool, lthashOnly bool, committedIsFullState bool, readLimitMiBps float64) error {
	limiter := newReadLimiter(readLimitMiBps)
	ctx := context.Background()

	var files map[string]*os.File
	var writers map[string]*bufio.Writer
	if !lthashOnly {
		if err := os.MkdirAll(outputDir, fs.ModePerm); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}
		var err error
		files, writers, err = openBucketWriters(outputDir, version, bucket)
		if err != nil {
			return err
		}
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

	iter, err := store.RawGlobalIterator()
	if err != nil {
		return fmt.Errorf("raw global iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	// Per-bucket LtHash accumulators. These are populated for every bucket
	// (independent of the --bucket write filter) so the printed total equals
	// the node's committed LtHash even when only one bucket is written to disk.
	var hashers map[string]*bucketLtHasher
	if withLtHash {
		hashers = make(map[string]*bucketLtHasher, len(flatkvBucketOrder))
		for _, name := range flatkvBucketOrder {
			hashers[name] = newBucketLtHasher()
		}
	}

	counts := make(map[string]uint64, len(flatkvBucketOrder))
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		val := iter.Value()
		if limiter != nil {
			// Cap the wait at the burst: rate.WaitN errors when n exceeds the
			// burst, so an oversized row (key+val larger than minReadBurstBytes)
			// would otherwise abort the whole scan. Clamping makes it cost a
			// full burst of budget instead of failing.
			n := len(key) + len(val)
			if burst := limiter.Burst(); n > burst {
				n = burst
			}
			if err := limiter.WaitN(ctx, n); err != nil {
				return fmt.Errorf("read rate limiter: %w", err)
			}
		}
		bucketName := classifyFlatKVPhysicalKey(key)
		if h := hashers[bucketName]; h != nil {
			h.add(key, val)
		}
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

	if withLtHash {
		printFlatKVLtHash(hashers, version)
		// committedIsFullState is false when the selected snapshot predates
		// LtHash metadata: the store opened with a zero baseline LtHash and
		// catchup only mixed in the deltas of the WAL blocks replayed on top,
		// so CommittedRootHash() is a partial hash (WAL deltas only, not the
		// snapshot's pre-existing rows). Cross-checking a full re-scan against
		// it would falsely fail, so skip verification; it becomes verifiable
		// again once a new snapshot with LtHash metadata exists.
		if !committedIsFullState {
			fmt.Println("\nLtHash verification: skipped (snapshot predates LtHash metadata; committed hash covers only replayed WAL deltas, not full state)")
		} else if err := verifyFlatKVLtHash(store, hashers); err != nil {
			return err
		}
	}
	return nil
}

// newReadLimiter builds a byte-throughput limiter from a MiB/s ceiling, or
// returns nil when mibps <= 0 (unlimited). The burst is one second of budget
// but at least minReadBurstBytes, so a single large row never exceeds the
// burst (which would make WaitN fail) and small ceilings stay usable.
func newReadLimiter(mibps float64) *rate.Limiter {
	if mibps <= 0 {
		return nil
	}
	bytesPerSec := mibps * bytesPerMiB
	burst := int(bytesPerSec)
	if burst < minReadBurstBytes {
		burst = minReadBurstBytes
	}
	fmt.Printf("Read throttle: %.1f MiB/s (burst %d bytes)\n", mibps, burst)
	return rate.NewLimiter(rate.Limit(bytesPerSec), burst)
}

// lthashBatchCap bounds how many (key, value) pairs a bucketLtHasher buffers
// before folding them into its running accumulator. The LtHash group is
// associative, so batching does not change the result; it only bounds memory
// (~lthashBatchCap cloned KV pairs) and lets ComputeLtHash parallelize within
// each batch.
const lthashBatchCap = 8192

// bucketLtHasher incrementally accumulates the LtHash of one bucket from a
// stream of (physical key, serialized value) pairs.
//
// It feeds the raw physical key and serialized value — exactly what
// RawGlobalIterator emits and exactly what the FlatKV store hashes into its
// per-DB working LtHash — so each bucket's checksum here equals the store's
// per-DB committed LtHash, and the MixIn sum of all four equals the global
// committed LtHash.
type bucketLtHasher struct {
	acc   *lthash.LtHash
	batch []lthash.KVPairWithLastValue
	count uint64
}

func newBucketLtHasher() *bucketLtHasher {
	return &bucketLtHasher{
		acc:   lthash.New(),
		batch: make([]lthash.KVPairWithLastValue, 0, lthashBatchCap),
	}
}

// add buffers one (key, value) pair. The iterator may reuse the underlying
// slices on Next(), so both are cloned before being retained in the batch.
func (h *bucketLtHasher) add(key, val []byte) {
	h.batch = append(h.batch, lthash.KVPairWithLastValue{
		Key:   bytes.Clone(key),
		Value: bytes.Clone(val),
	})
	h.count++
	if len(h.batch) >= lthashBatchCap {
		h.flush()
	}
}

func (h *bucketLtHasher) flush() {
	if len(h.batch) == 0 {
		return
	}
	delta, _ := lthash.ComputeLtHash(nil, h.batch)
	h.acc.MixIn(delta)
	h.batch = h.batch[:0]
}

func printFlatKVLtHash(hashers map[string]*bucketLtHasher, version int64) {
	total := lthash.New()
	fmt.Printf("\nLtHash (lattice hash) at version %d\n", version)
	for _, name := range flatkvBucketOrder {
		h := hashers[name]
		if h == nil {
			continue
		}
		h.flush()
		total.MixIn(h.acc)
		fmt.Printf("  bucket %-7s count=%d lthash=%x\n", name, h.count, h.acc.Checksum())
	}
	fmt.Printf("  TOTAL          lthash=%x\n", total.Checksum())
}

// verifyFlatKVLtHash cross-checks the freshly re-scanned total LtHash against
// the committed global LtHash the FlatKV store loaded from snapshot metadata
// (CommittedRootHash). A PASS means the physical bytes on disk hash to exactly
// the committed root recorded at this version. Returns an error on mismatch so
// the CLI exits non-zero.
func verifyFlatKVLtHash(store *flatkv.CommitStore, hashers map[string]*bucketLtHasher) error {
	committedTotal := store.CommittedRootHash()

	// A store that loaded no LtHash from metadata reports the checksum of the
	// zero LtHash. Treat that as "nothing to verify against" rather than a
	// spurious failure (e.g. a snapshot that predates LtHash metadata).
	zero := lthash.New().Checksum()
	if bytes.Equal(committedTotal, zero[:]) {
		fmt.Println("\nLtHash verification: skipped (no committed LtHash in metadata at this version)")
		return nil
	}

	total := lthash.New()
	for _, name := range flatkvBucketOrder {
		h := hashers[name]
		if h == nil {
			continue
		}
		h.flush()
		total.MixIn(h.acc)
	}

	fmt.Println("\nLtHash verification vs snapshot metadata (committed)")
	gotTotal := total.Checksum()
	if bytes.Equal(gotTotal[:], committedTotal) {
		fmt.Printf("  TOTAL OK   re-scanned=%x matches committed metadata\n", gotTotal)
		fmt.Println("  result: PASS")
		return nil
	}
	fmt.Printf("  TOTAL FAIL re-scanned=%x committed=%x\n", gotTotal, committedTotal)
	return fmt.Errorf("LtHash verification FAILED: re-scanned state does not match committed snapshot metadata at this version")
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
