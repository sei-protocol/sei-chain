package mvcc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

	"github.com/cockroachdb/pebble/v2"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

const (
	// mergeBatchBytes is the encoded size at which Merge commits a batch, well
	// below the memtable size.
	mergeBatchBytes = 16 << 20
	// progressEvery is the number of entries Merge and Scan read between
	// Progress calls.
	progressEvery = 1_000_000
)

// MergeOptions configures Merge.
type MergeOptions struct {
	// StoreKeys lists the store keys either DB may hold entries under. Empty
	// accepts any.
	StoreKeys []string
	// AllowOverlap accepts DBs whose version ranges overlap. A source entry the
	// target already holds is then left as the target holds it.
	AllowOverlap bool
	// AllowGap accepts DBs whose version ranges leave a run of versions between
	// them that neither covers. The merged markers then span that run, so a
	// read inside it finds no entry instead of being refused as pruned.
	AllowGap bool
	// DryRun makes every check and reads the whole source without writing.
	DryRun bool
	// VerifyEvery is the interval, in written entries, at which entries are read
	// back through Get once the merge is committed. 0 reads none back.
	VerifyEvery int64
	// Progress, when set, is called with the running report as the source is read.
	Progress func(MergeReport)
}

// DBInfo is a DB's MVCC encoding, version markers and store keys.
type DBInfo struct {
	Dir        string
	Descending bool
	Earliest   int64
	Latest     int64
	// StoreKeys lists the store keys the DB holds entries under.
	StoreKeys []string
}

// VersionRange is an inclusive run of versions. The zero value holds none.
type VersionRange struct {
	From, To int64
}

// MergeReport is what Merge found and did.
type MergeReport struct {
	Source DBInfo
	Target DBInfo
	// Gap is the run of versions between the two DBs that neither covers.
	Gap VersionRange
	// Entries is the number of source entries read.
	Entries int64
	// Written is the number of entries written to the target, and Bytes their
	// encoded size. A dry run counts what it would write.
	Written int64
	Bytes   int64
	// Held is the number of source entries the target already held, and
	// Conflicting the number of those it held with a different value.
	Held        int64
	Conflicting int64
	// Verified is the number of written entries read back through Get.
	Verified int64
	// Earliest and Latest are the target's version markers after the merge.
	Earliest int64
	Latest   int64
}

// Merge copies every versioned entry of the DB at sourceDir into the DB at
// targetDir, in the target's MVCC encoding, and sets the target's earliest and
// latest version markers to span both. The source is opened read-only. Neither
// DB may be open elsewhere, the source must hold versions, and the two version
// ranges must neither overlap unless opts.AllowOverlap is set nor leave a gap
// between them unless opts.AllowGap is set. Metadata other than the version
// markers stays as each DB has it.
func Merge(targetDir, sourceDir string, opts MergeOptions) (MergeReport, error) {
	if err := requireDistinctDirs(targetDir, sourceDir); err != nil {
		return MergeReport{}, err
	}
	source, err := openReplayedDB(sourceDir, true)
	if err != nil {
		return MergeReport{}, fmt.Errorf("source %s: %w", sourceDir, err)
	}
	defer func() { _ = source.close() }()
	target, err := openReplayedDB(targetDir, opts.DryRun)
	if err != nil {
		return MergeReport{}, fmt.Errorf("target %s: %w", targetDir, err)
	}
	defer func() { _ = target.close() }()

	report := MergeReport{Source: source.info, Target: target.info}
	if report.Gap, err = checkMergeable(source.info, target.info, opts); err != nil {
		return report, err
	}
	report.Earliest, report.Latest = mergedRange(source.info, target.info)

	samples, err := copyEntries(&report, source, target, opts)
	if err != nil {
		return report, err
	}
	if opts.DryRun {
		return report, nil
	}
	if err := writeVersionMarkers(target.db, report.Earliest, report.Latest); err != nil {
		return report, fmt.Errorf("write target version markers: %w", err)
	}
	if err := target.close(); err != nil {
		return report, fmt.Errorf("close target: %w", err)
	}
	report.Verified, err = verifyEntries(targetDir, samples)
	return report, err
}

// offlineDB is a DB opened straight through pebble by a tool, while no node has
// it open.
type offlineDB struct {
	db   *pebble.DB
	info DBInfo
}

// openReplayedDB opens the DB at dir as openOfflineDB does, and fails if the
// DB's changelog holds a version it has not applied.
func openReplayedDB(dir string, readOnly bool) (*offlineDB, error) {
	m, err := openOfflineDB(dir, readOnly)
	if err != nil {
		return nil, err
	}
	if err := requireChangelogReplayed(dir, m.info.Latest); err != nil {
		_ = m.close()
		return nil, err
	}
	return m, nil
}

// openOfflineDB opens the pebble DB at dir with the options OpenDB uses,
// read-only when asked, and reads what DBInfo reports about it.
func openOfflineDB(dir string, readOnly bool) (*offlineDB, error) {
	cache := pebble.NewCache(32 << 20)
	defer cache.Unref()
	opts := newPebbleOptions(config.DefaultStateStoreConfig(), cache)
	opts.ErrorIfNotExists = true
	opts.ReadOnly = readOnly
	db, err := pebble.Open(dir, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open PebbleDB: %w", err)
	}
	m := &offlineDB{db: db, info: DBInfo{Dir: dir}}
	if err := m.readInfo(); err != nil {
		_ = m.close()
		return nil, err
	}
	return m, nil
}

// readInfo fills in m.info from the DB's markers and keys.
func (m *offlineDB) readInfo() error {
	descending, fresh, err := readMVCCMode(m.db)
	if err != nil {
		return err
	}
	if fresh {
		return errors.New("holds no MVCC data")
	}
	m.info.Descending = descending
	if m.info.Earliest, err = retrieveEarliestVersion(m.db); err != nil {
		return fmt.Errorf("failed to retrieve earliest version: %w", err)
	}
	if m.info.Latest, err = retrieveLatestVersion(m.db); err != nil {
		return fmt.Errorf("failed to retrieve latest version: %w", err)
	}
	m.info.StoreKeys, err = storeKeys(m.db, descending)
	return err
}

// close closes the DB. Later calls do nothing.
func (m *offlineDB) close() error {
	if m.db == nil {
		return nil
	}
	err := m.db.Close()
	m.db = nil
	return err
}

// requireDistinctDirs fails unless targetDir and sourceDir are two existing
// directories.
func requireDistinctDirs(targetDir, sourceDir string) error {
	target, err := os.Stat(targetDir)
	if err != nil {
		return fmt.Errorf("target: %w", err)
	}
	source, err := os.Stat(sourceDir)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	if os.SameFile(target, source) {
		return fmt.Errorf("source and target are the same directory %s", targetDir)
	}
	return nil
}

// storeKeys returns, in key order, the store keys db holds entries under.
func storeKeys(db *pebble.DB, descending bool) ([]string, error) {
	itr, err := db.NewIter(&pebble.IterOptions{LowerBound: MVCCEncode([]byte(PrefixStore), 0, descending)})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	defer func() { _ = itr.Close() }()

	var keys []string
	for valid := itr.First(); valid; {
		userKey, _, ok := SplitMVCCKey(itr.Key())
		if !ok {
			return nil, fmt.Errorf("invalid MVCC key %x", itr.Key())
		}
		rest, inStore := bytes.CutPrefix(userKey, []byte(PrefixStore))
		if !inStore {
			break
		}
		storeKey, _, found := bytes.Cut(rest, []byte("/"))
		if !found {
			return nil, fmt.Errorf("key %q has no store separator", userKey)
		}
		keys = append(keys, string(storeKey))
		valid = itr.SeekGE(MVCCEncode(prefixEnd(storePrefix(string(storeKey))), 0, descending))
	}
	return keys, itr.Error()
}

// requireChangelogReplayed fails if the changelog kept beside the DB at dir
// holds a version above latest, which only opening the DB would apply.
func requireChangelogReplayed(dir string, latest int64) error {
	ahead, err := changelogAhead(dir, latest)
	if err != nil {
		return err
	}
	if ahead > 0 {
		return fmt.Errorf("changelog holds version %d above latest version %d; open the DB once to replay it",
			ahead, latest)
	}
	return nil
}

// changelogAhead returns the version of the last entry in the changelog kept
// beside the DB at dir when it is above latest, and 0 otherwise.
func changelogAhead(dir string, latest int64) (int64, error) {
	path := utils.GetChangelogPath(dir)
	segments, err := os.ReadDir(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read changelog directory: %w", err)
	}
	if len(segments) == 0 {
		return 0, nil
	}
	changelog, err := wal.NewChangelogWAL(path, wal.Config{NoRepairOnOpen: true})
	if err != nil {
		return 0, fmt.Errorf("open changelog: %w", err)
	}
	defer func() { _ = changelog.Close() }()
	last, err := changelog.LastOffset()
	if err != nil {
		return 0, fmt.Errorf("read changelog last offset: %w", err)
	}
	if last == 0 {
		return 0, nil
	}
	entry, err := changelog.ReadAt(last)
	if err != nil {
		return 0, fmt.Errorf("read changelog entry %d: %w", last, err)
	}
	if entry.Version > latest {
		return entry.Version, nil
	}
	return 0, nil
}

// checkMergeable fails if opts do not allow merging source into target, and
// returns the run of versions between the two DBs that neither covers.
func checkMergeable(source, target DBInfo, opts MergeOptions) (VersionRange, error) {
	if source.Latest == 0 {
		return VersionRange{}, errors.New("source holds no versions")
	}
	for _, side := range []DBInfo{source, target} {
		for _, storeKey := range side.StoreKeys {
			if len(opts.StoreKeys) > 0 && !slices.Contains(opts.StoreKeys, storeKey) {
				return VersionRange{}, fmt.Errorf("%s holds store %q, outside the accepted stores %q",
					side.Dir, storeKey, opts.StoreKeys)
			}
		}
	}
	if target.Latest == 0 {
		return VersionRange{}, nil
	}
	lower, upper := source, target
	if lower.Latest > upper.Latest {
		lower, upper = upper, lower
	}
	// An unset earliest marker reads as 0, so a DB that never recorded one
	// claims every version below its latest and overlaps rather than leaving a
	// gap.
	if upper.Earliest <= lower.Latest {
		if !opts.AllowOverlap {
			return VersionRange{}, fmt.Errorf("source versions %d-%d overlap target versions %d-%d",
				source.Earliest, source.Latest, target.Earliest, target.Latest)
		}
		return VersionRange{}, nil
	}
	if upper.Earliest == lower.Latest+1 {
		return VersionRange{}, nil
	}
	gap := VersionRange{From: lower.Latest + 1, To: upper.Earliest - 1}
	if !opts.AllowGap {
		return gap, fmt.Errorf("neither DB covers versions %d-%d, between source versions %d-%d and target versions %d-%d",
			gap.From, gap.To, source.Earliest, source.Latest, target.Earliest, target.Latest)
	}
	return gap, nil
}

// mergedRange returns the earliest and latest version markers of the merged
// target. A target holding no versions takes the source's.
func mergedRange(source, target DBInfo) (earliest, latest int64) {
	if target.Latest == 0 {
		return source.Earliest, source.Latest
	}
	return min(source.Earliest, target.Earliest), max(source.Latest, target.Latest)
}

// copyEntries writes every source entry into the target in the target's
// encoding, counting into report, and returns the written entries to read back.
func copyEntries(report *MergeReport, source, target *offlineDB, opts MergeOptions) ([]mergeSample, error) {
	itr, err := source.db.NewIter(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	defer func() { _ = itr.Close() }()
	writer := &mergeWriter{db: target.db}
	defer writer.discard()

	var samples []mergeSample
	for valid := itr.First(); valid; valid = itr.Next() {
		if isMetadataKey(itr.Key()) {
			continue
		}
		report.Entries++
		if opts.Progress != nil && report.Entries%progressEvery == 0 {
			opts.Progress(*report)
		}
		value, err := itr.ValueAndErr()
		if err != nil {
			return nil, fmt.Errorf("read source value: %w", err)
		}
		key, value, err := encodeForTarget(itr.Key(), value, source.info.Descending, target.info.Descending)
		if err != nil {
			return nil, err
		}
		if opts.AllowOverlap {
			held, same, err := heldBy(target.db, key, value)
			if err != nil {
				return nil, fmt.Errorf("read target: %w", err)
			}
			if held {
				report.Held++
				if !same {
					report.Conflicting++
				}
				continue
			}
		}
		report.Written++
		report.Bytes += int64(len(key) + len(value))
		if opts.DryRun {
			continue
		}
		if err := writer.set(key, value); err != nil {
			return nil, fmt.Errorf("write target: %w", err)
		}
		if opts.VerifyEvery > 0 && report.Written%opts.VerifyEvery == 0 {
			sample, err := newMergeSample(key, value, target.info.Descending)
			if err != nil {
				return nil, err
			}
			// Get answers nothing below the earliest marker, and never addresses a
			// version-less entry.
			if sample.version > 0 && sample.version >= report.Earliest {
				samples = append(samples, sample)
			}
		}
	}
	if err := itr.Error(); err != nil {
		return nil, fmt.Errorf("iterate source: %w", err)
	}
	return samples, writer.commit()
}

// encodeForTarget returns a source entry in the target's MVCC encoding.
func encodeForTarget(key, value []byte, sourceDescending, targetDescending bool) ([]byte, []byte, error) {
	if sourceDescending == targetDescending {
		return key, value, nil
	}
	key, err := reencodeVersion(key, sourceDescending, targetDescending)
	if err != nil {
		return nil, nil, fmt.Errorf("re-encode key: %w", err)
	}
	value, err = reencodeVersion(value, sourceDescending, targetDescending)
	if err != nil {
		return nil, nil, fmt.Errorf("re-encode value of %x: %w", key, err)
	}
	return key, value, nil
}

// reencodeVersion returns b, an MVCC-encoded key or value, with its version
// moved from one MVCC encoding to the other. A b without a version reads the
// same in both.
func reencodeVersion(b []byte, fromDescending, toDescending bool) ([]byte, error) {
	body, vBz, ok := SplitMVCCKey(b)
	if !ok {
		return nil, fmt.Errorf("invalid MVCC encoding %x", b)
	}
	if len(vBz) == 0 {
		return b, nil
	}
	version, err := decodeMVCCVersion(vBz, fromDescending)
	if err != nil {
		return nil, err
	}
	return MVCCEncode(body, version, toDescending), nil
}

// heldBy reports whether db holds key, and if so whether it holds it with value.
func heldBy(db *pebble.DB, key, value []byte) (held, same bool, err error) {
	existing, closer, err := db.Get(key)
	if errors.Is(err, pebble.ErrNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	same = bytes.Equal(existing, value)
	return true, same, closer.Close()
}

// mergeWriter commits the entries it is given in batches of mergeBatchBytes.
type mergeWriter struct {
	db    *pebble.DB
	batch *pebble.Batch
}

func (w *mergeWriter) set(key, value []byte) error {
	if w.batch == nil {
		w.batch = w.db.NewBatch()
	}
	if err := w.batch.Set(key, value, nil); err != nil {
		return err
	}
	if w.batch.Len() < mergeBatchBytes {
		return nil
	}
	return w.commit()
}

// commit commits the pending batch, if there is one.
func (w *mergeWriter) commit() error {
	if w.batch == nil {
		return nil
	}
	err := w.batch.Commit(pebble.NoSync)
	w.discard()
	return err
}

// discard releases the pending batch, if there is one, without committing it.
func (w *mergeWriter) discard() {
	if w.batch == nil {
		return
	}
	_ = w.batch.Close()
	w.batch = nil
}

// writeVersionMarkers sets db's earliest and latest version markers in one
// synced batch.
func writeVersionMarkers(db *pebble.DB, earliest, latest int64) error {
	batch := db.NewBatch()
	defer func() { _ = batch.Close() }()
	for _, marker := range []struct {
		key     string
		version int64
	}{{earliestVersionKey, earliest}, {latestVersionKey, latest}} {
		var bz [VersionSize]byte
		binary.LittleEndian.PutUint64(bz[:], uint64(marker.version)) //nolint:gosec // versions are non-negative
		if err := batch.Set([]byte(marker.key), bz[:], nil); err != nil {
			return err
		}
	}
	return batch.Commit(pebble.Sync)
}

// mergeSample is a written entry as Get reads it back.
type mergeSample struct {
	storeKey string
	key      []byte
	version  int64
	// want is the value Get returns at version, nil for a deletion.
	want []byte
}

// newMergeSample decodes an entry written in the given encoding into the Get
// call that reads it back and the value that call returns.
func newMergeSample(key, value []byte, descending bool) (mergeSample, error) {
	userKey, vBz, ok := SplitMVCCKey(key)
	if !ok {
		return mergeSample{}, fmt.Errorf("invalid MVCC key %x", key)
	}
	payload, tombstoneBz, ok := SplitMVCCKey(value)
	if !ok {
		return mergeSample{}, fmt.Errorf("invalid MVCC value of %x", key)
	}
	sample := mergeSample{key: userKey, want: payload}
	if rest, inStore := bytes.CutPrefix(userKey, []byte(PrefixStore)); inStore {
		if storeKey, storeRelative, found := bytes.Cut(rest, []byte("/")); found {
			sample.storeKey, sample.key = string(storeKey), storeRelative
		}
	}
	var err error
	if len(vBz) > 0 {
		if sample.version, err = decodeMVCCVersion(vBz, descending); err != nil {
			return mergeSample{}, err
		}
	}
	if len(tombstoneBz) > 0 {
		tombstone, err := decodeMVCCVersion(tombstoneBz, descending)
		if err != nil {
			return mergeSample{}, err
		}
		if tombstone <= sample.version {
			sample.want = nil
		}
	}
	return sample, nil
}

// verifyEntries reads every sample back through Get on the DB at dir.
func verifyEntries(dir string, samples []mergeSample) (int64, error) {
	if len(samples) == 0 {
		return 0, nil
	}
	cfg := config.DefaultStateStoreConfig()
	cfg.DisableInternalWAL = true
	store, err := OpenDB(dir, cfg)
	if err != nil {
		return 0, fmt.Errorf("reopen target: %w", err)
	}
	defer func() { _ = store.Close() }()
	for i, sample := range samples {
		got, err := store.Get(sample.storeKey, sample.version, sample.key)
		if err != nil {
			return int64(i), fmt.Errorf("read back %s/%x at version %d: %w",
				sample.storeKey, sample.key, sample.version, err)
		}
		if !bytes.Equal(got, sample.want) {
			return int64(i), fmt.Errorf("read back %s/%x at version %d: got %d bytes that differ from the %d written",
				sample.storeKey, sample.key, sample.version, len(got), len(sample.want))
		}
	}
	return int64(len(samples)), nil
}
