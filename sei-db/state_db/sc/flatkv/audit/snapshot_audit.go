package audit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

const (
	// snapshotAuditDirPrefix names the temporary directory, under the FlatKV root, that an audit links its
	// snapshot into.
	snapshotAuditDirPrefix = ".snapshot-audit-"

	// snapshotAuditLinkName is the name of the linked snapshot inside that temporary directory.
	snapshotAuditLinkName = "snapshot"

	// snapshotAuditMinBurstBytes is the smallest burst the read limiter is given.
	snapshotAuditMinBurstBytes = 4 << 20
)

// snapshotAuditor reads the databases of one linked snapshot and hashes their rows on a pool of hashers.
type snapshotAuditor struct {
	// config is the audit's configuration.
	config *SnapshotAuditConfig

	// limiter caps the read rate. Nil when the read rate is uncapped.
	limiter *rate.Limiter

	// batches carries the reader's batches to the hashers. The reader closes it once it stops reading.
	batches chan *auditBatch

	// results carries each hasher's totals back after batches is closed.
	results chan *auditHasherResult

	// rows is how many data rows the reader has read.
	rows uint64

	// bytes is the key and value bytes of those rows.
	bytes uint64

	// readerBlocked is how long the reader has spent waiting for a hasher to take a batch.
	readerBlocked time.Duration

	// hasherIdle is how long the hashers spent waiting for a batch, summed over all of them. Set by finish().
	hasherIdle time.Duration
}

// auditBatch is a run of data rows from one database.
type auditBatch struct {
	// db is the name of the database the rows were read from.
	db string

	// rows are the rows, each with its key and value copied out of the iterator.
	rows []lthash.KeyMutation
}

// auditedDatabase is what the reader learned about one database besides its rows.
type auditedDatabase struct {
	// meta is the metadata the database records.
	meta *flatkv.LocalMeta

	// diskBytes is the total size of the database's files.
	diskBytes uint64
}

// auditHasherResult is what one hasher computed over every batch it took.
type auditHasherResult struct {
	// modules holds the hash and stats of every (database, module) the hasher saw rows for.
	modules map[lthash.ModuleKey]*lthash.ModuleHashInfo

	// idle is how long the hasher spent waiting for a batch.
	idle time.Duration

	// err is the first row the hasher could not attribute to a module, nil when there was none.
	err error
}

// AuditSnapshot recomputes the lattice hashes and stats of a FlatKV snapshot from a full scan of its rows, and
// compares them against the per-module, per-database and store-wide values the snapshot's metadata records.
//
// The snapshot is hard-linked into a temporary directory under the root, which is removed before returning, and
// its databases are opened read-only, so it is safe to audit a snapshot of a running node. A mismatch is reported
// in the returned report, and an error means the audit could not be completed.
func AuditSnapshot(ctx context.Context, config *SnapshotAuditConfig) (_ *SnapshotAuditReport, retErr error) {
	started := time.Now()
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid snapshot audit config: %w", err)
	}

	version, err := resolveAuditVersion(config)
	if err != nil {
		return nil, err
	}

	// The temporary directory must share a filesystem with the snapshot, or it cannot be hard-linked.
	linkDir, err := os.MkdirTemp(config.Root, snapshotAuditDirPrefix+"*")
	if err != nil {
		return nil, fmt.Errorf("create the audit directory under %q: %w", config.Root, err)
	}
	defer func() {
		// A directory left behind keeps the linked snapshot's disk space in use after the node prunes it.
		if err := os.RemoveAll(linkDir); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("remove the audit directory %q: %w", linkDir, err))
		}
	}()

	snapshotDir := filepath.Join(linkDir, snapshotAuditLinkName)
	//nolint:gosec // Validate() bounds the version, and a resolved one came from an int64
	if err := flatkv.HardLinkSnapshot(config.Root, int64(version), snapshotDir); err != nil {
		return nil, fmt.Errorf("link the snapshot: %w", err)
	}

	report, err := auditLinkedSnapshot(ctx, config, snapshotDir, version)
	if err != nil {
		return nil, err
	}
	report.Elapsed = time.Since(started)
	return report, nil
}

// resolveAuditVersion returns the version of the snapshot the config selects.
func resolveAuditVersion(config *SnapshotAuditConfig) (uint64, error) {
	if config.Version != 0 {
		return config.Version, nil
	}
	version, err := flatkv.ActiveSnapshotVersion(config.Root)
	if err != nil {
		return 0, fmt.Errorf("resolve the snapshot to audit: %w", err)
	}
	return uint64(version), nil //nolint:gosec // a snapshot version is never negative
}

// auditLinkedSnapshot audits the snapshot directory dir, which holds the databases of the snapshot at version.
func auditLinkedSnapshot(
	ctx context.Context,
	config *SnapshotAuditConfig,
	// The snapshot directory, which the audit reads and never modifies.
	dir string,
	// The version of the snapshot in dir.
	version uint64,
) (*SnapshotAuditReport, error) {
	scanStarted := time.Now()
	auditor := newSnapshotAuditor(config)
	databases, readErr := auditor.readDatabases(ctx, dir)
	// The hashers have to be stopped whether or not the read finished.
	scanned, hashErr := auditor.finish()
	if err := errors.Join(readErr, hashErr); err != nil {
		return nil, fmt.Errorf("scan snapshot %d: %w", version, err)
	}

	report := buildSnapshotAuditReport(version, databases, scanned)
	report.Rows = auditor.rows
	report.Bytes = auditor.bytes
	report.ScanElapsed = time.Since(scanStarted)
	report.ReaderBlocked = auditor.readerBlocked
	report.HasherIdle = auditor.hasherIdle
	return report, nil
}

// newSnapshotAuditor returns an auditor with its hashers running.
func newSnapshotAuditor(config *SnapshotAuditConfig) *snapshotAuditor {
	auditor := &snapshotAuditor{
		config:  config,
		batches: make(chan *auditBatch, 2*int(config.Hashers)),
		results: make(chan *auditHasherResult, config.Hashers),
	}
	if config.ReadBytesPerSecond > 0 {
		// A burst of one second's budget, but never so small that WaitN refuses an ordinary large row.
		burst := min(max(config.ReadBytesPerSecond, snapshotAuditMinBurstBytes), math.MaxInt)
		//nolint:gosec // burst is capped at math.MaxInt above
		auditor.limiter = rate.NewLimiter(rate.Limit(config.ReadBytesPerSecond), int(burst))
	}
	for range config.Hashers {
		go auditor.hash()
	}
	return auditor
}

// readDatabases reads every data database under the snapshot directory dir, handing its data rows to the
// hashers, and returns what it learned about each database besides its rows, keyed by database name.
func (a *snapshotAuditor) readDatabases(ctx context.Context, dir string) (map[string]auditedDatabase, error) {
	names := flatkv.DataDBDirs()
	databases := make(map[string]auditedDatabase, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("audit cancelled: %w", err)
		}
		dbDir := filepath.Join(dir, name)
		// Measured before the open, which adds a LOCK file.
		diskBytes, err := directoryBytes(dbDir)
		if err != nil {
			return nil, fmt.Errorf("measure the %s database: %w", name, err)
		}
		meta, err := a.readDatabase(ctx, name, dbDir)
		if err != nil {
			return nil, err
		}
		databases[name] = auditedDatabase{meta: meta, diskBytes: diskBytes}
	}
	return databases, nil
}

// directoryBytes returns the total size of the regular files under dir.
func directoryBytes(dir string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat %q: %w", path, err)
		}
		total += uint64(info.Size()) //nolint:gosec // a file size is never negative
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("walk %q: %w", dir, err)
	}
	return total, nil
}

// readDatabase reads every row of one database, handing its data rows to the hashers in batches, and returns
// the metadata the database records.
func (a *snapshotAuditor) readDatabase(
	ctx context.Context,
	// The database's name.
	name string,
	// The database's directory.
	dbDir string,
) (_ *flatkv.LocalMeta, retErr error) {
	// A read-write open could write into a file that is hard-linked to the live snapshot.
	db, err := openPebbleReadOnly(dbDir)
	if err != nil {
		return nil, fmt.Errorf("open the %s database: %w", name, err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close the %s database: %w", name, err))
		}
	}()

	iter, err := db.NewIter(nil)
	if err != nil {
		return nil, fmt.Errorf("iterate the %s database: %w", name, err)
	}
	defer func() {
		if err := iter.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close the %s database iterator: %w", name, err))
		}
	}()

	var decoder flatkv.LocalMetaDecoder
	batch := a.newBatch(name)
	for valid := iter.First(); valid; valid = iter.Next() {
		key := iter.Key()
		value := iter.Value()
		if ktype.IsMetaKey(key) {
			decoder.Add(key, value)
			continue
		}
		if err := a.throttle(ctx, len(key)+len(value)); err != nil {
			return nil, err
		}
		batch.rows = append(batch.rows, lthash.KeyMutation{Key: bytes.Clone(key), Value: bytes.Clone(value)})
		a.rows++
		a.bytes += uint64(len(key)) + uint64(len(value))
		if len(batch.rows) == int(a.config.BatchSize) {
			if err := a.send(ctx, batch); err != nil {
				return nil, err
			}
			batch = a.newBatch(name)
		}
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterate the %s database: %w", name, err)
	}
	if len(batch.rows) > 0 {
		if err := a.send(ctx, batch); err != nil {
			return nil, err
		}
	}

	meta, err := decoder.Finish()
	if err != nil {
		return nil, fmt.Errorf("decode the %s database's metadata: %w", name, err)
	}
	return meta, nil
}

// openPebbleReadOnly opens the Pebble DB at dbDir read-only: it modifies none of the database's files, and
// creates only a LOCK file when there is none.
func openPebbleReadOnly(dbDir string) (*pebble.DB, error) {
	db, err := pebble.Open(dbDir, &pebble.Options{
		ReadOnly:           true,
		FormatMajorVersion: pebble.FormatVirtualSSTables,
	})
	if err != nil {
		return nil, fmt.Errorf("open pebble read-only: %w", err)
	}
	return db, nil
}

// newBatch returns an empty batch for the named database.
func (a *snapshotAuditor) newBatch(db string) *auditBatch {
	return &auditBatch{db: db, rows: make([]lthash.KeyMutation, 0, a.config.BatchSize)}
}

// throttle waits until the read budget allows n more bytes. It returns at once when the read rate is uncapped.
func (a *snapshotAuditor) throttle(ctx context.Context, n int) error {
	if a.limiter == nil {
		return nil
	}
	// WaitN refuses a request larger than the burst, so a row larger than the burst costs a whole burst.
	if err := a.limiter.WaitN(ctx, min(n, a.limiter.Burst())); err != nil {
		return fmt.Errorf("wait for read budget: %w", err)
	}
	return nil
}

// send hands a batch to the hashers, waiting while every hasher is busy and the channel is full.
func (a *snapshotAuditor) send(ctx context.Context, batch *auditBatch) error {
	started := time.Now()
	select {
	case a.batches <- batch:
		a.readerBlocked += time.Since(started)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("audit cancelled: %w", ctx.Err())
	}
}

// hash is the body of one hasher: it hashes every batch it takes until batches is closed, then reports its
// totals on results.
func (a *snapshotAuditor) hash() {
	result := &auditHasherResult{modules: make(map[lthash.ModuleKey]*lthash.ModuleHashInfo)}
	for {
		waitStarted := time.Now()
		batch, ok := <-a.batches
		result.idle += time.Since(waitStarted)
		if !ok {
			break
		}
		// After a failure the hasher keeps taking batches, so the reader is never left waiting on it.
		if result.err == nil {
			result.err = hashAuditBatch(batch, result.modules)
		}
	}
	a.results <- result
}

// finish closes batches, waits for every hasher, and returns the merged hash and stats of every
// (database, module) the hashers saw rows for.
func (a *snapshotAuditor) finish() (map[lthash.ModuleKey]*lthash.ModuleHashInfo, error) {
	close(a.batches)
	merged := make(map[lthash.ModuleKey]*lthash.ModuleHashInfo)
	errs := make([]error, 0, a.config.Hashers)
	for range a.config.Hashers {
		result := <-a.results
		a.hasherIdle += result.idle
		errs = append(errs, result.err)
		for key, info := range result.modules {
			acc := merged[key]
			if acc == nil {
				merged[key] = info
				continue
			}
			acc.Hash.MixIn(info.Hash)
			acc.KeyCount += info.KeyCount
			acc.Bytes += info.Bytes
		}
	}
	return merged, errors.Join(errs...)
}

// hashAuditBatch folds the rows of one batch into the per-(database, module) hashes and stats in modules.
func hashAuditBatch(batch *auditBatch, modules map[lthash.ModuleKey]*lthash.ModuleHashInfo) error {
	byModule, err := lthash.BucketByModule(batch.rows, moduleOfKey)
	if err != nil {
		return fmt.Errorf("route a row of the %s database: %w", batch.db, err)
	}
	for module, rows := range byModule {
		key := lthash.ModuleKey{DBName: batch.db, Module: module}
		info := modules[key]
		if info == nil {
			info = &lthash.ModuleHashInfo{Hash: lthash.New()}
			modules[key] = info
		}
		info.Hash.MixIn(lthash.ComputeLtHash(nil, rows))
		for _, row := range rows {
			// The membership rule the hash applies: a row with an empty key or value is not a member.
			if len(row.Key) == 0 || len(row.Value) == 0 {
				continue
			}
			info.KeyCount++
			info.Bytes += int64(len(row.Key)) + int64(len(row.Value))
		}
	}
	return nil
}

// moduleOfKey returns the module that owns a physical key, the routing the store's write path uses.
func moduleOfKey(physicalKey []byte) (string, error) {
	module, _, err := ktype.StripModulePrefix(physicalKey)
	if err != nil {
		return "", fmt.Errorf("strip the module prefix from key %x: %w", physicalKey, err)
	}
	return module, nil
}

// buildSnapshotAuditReport compares what the scan computed against what each database's metadata records.
func buildSnapshotAuditReport(
	// The version of the audited snapshot.
	version uint64,
	// What the reader learned about each database besides its rows, keyed by database name.
	databases map[string]auditedDatabase,
	// The hash and stats the scan computed for every (database, module) it saw rows for.
	scanned map[lthash.ModuleKey]*lthash.ModuleHashInfo,
) *SnapshotAuditReport {
	names := flatkv.DataDBDirs()
	scannedByDB := make(map[string]map[string]*lthash.ModuleHashInfo, len(names))
	for key, info := range scanned {
		if scannedByDB[key.DBName] == nil {
			scannedByDB[key.DBName] = make(map[string]*lthash.ModuleHashInfo)
		}
		scannedByDB[key.DBName][key.Module] = info
	}

	report := &SnapshotAuditReport{Version: version}
	scannedRoots := make(map[string]*lthash.LtHash, len(names))
	recordedRoots := make(map[string]*lthash.LtHash, len(names))
	for _, name := range names {
		database := databases[name]
		audit, scannedRoot, recordedRoot := auditDatabase(name, version, database.meta, scannedByDB[name])
		audit.DiskBytes = database.diskBytes
		report.DBs = append(report.DBs, audit)
		scannedRoots[name] = scannedRoot
		recordedRoots[name] = recordedRoot
	}
	report.Global = HashAudit{
		Scanned:  lthash.SumDBHashes(names, scannedRoots).Checksum(),
		Recorded: lthash.SumDBHashes(names, recordedRoots).Checksum(),
	}
	return report
}

// auditDatabase compares one database's scan against its metadata, and returns the comparison together with the
// database's scanned and recorded roots. A module present on only one side is compared against the identity
// hash and zero stats.
func auditDatabase(
	// The database's name.
	name string,
	// The version of the audited snapshot.
	version uint64,
	// The metadata the database records.
	meta *flatkv.LocalMeta,
	// The hash and stats the scan computed for each module of the database, keyed by module name.
	scanned map[string]*lthash.ModuleHashInfo,
) (DBAudit, *lthash.LtHash, *lthash.LtHash) {
	audit := DBAudit{
		Name: name,
		//nolint:gosec // a recorded version is never negative
		Version: VersionAudit{Expected: version, Recorded: uint64(meta.CommittedVersion)},
	}

	modules := make(map[string]struct{}, len(scanned)+len(meta.ModuleLtHashes))
	for module := range scanned {
		modules[module] = struct{}{}
	}
	for module := range meta.ModuleLtHashes {
		modules[module] = struct{}{}
	}
	for module := range meta.ModuleStats {
		modules[module] = struct{}{}
	}

	scannedRoot := lthash.New()
	for _, module := range slices.Sorted(maps.Keys(modules)) {
		moduleAudit := ModuleAudit{Module: module, Recorded: meta.ModuleStats[module]}
		scannedHash := lthash.New()
		if info := scanned[module]; info != nil {
			scannedHash = info.Hash
			moduleAudit.Scanned = lthash.ModuleStats{KeyCount: info.KeyCount, Bytes: info.Bytes}
		}
		recordedHash := lthash.New()
		if h := meta.ModuleLtHashes[module]; h != nil {
			recordedHash = h
		}
		moduleAudit.Hash = HashAudit{Scanned: scannedHash.Checksum(), Recorded: recordedHash.Checksum()}
		audit.Modules = append(audit.Modules, moduleAudit)
		scannedRoot.MixIn(scannedHash)
	}

	// A database that has never been written records no root, which reads as the identity.
	recordedRoot := lthash.New()
	if meta.LtHash != nil {
		recordedRoot = meta.LtHash
	}
	audit.Root = HashAudit{Scanned: scannedRoot.Checksum(), Recorded: recordedRoot.Checksum()}
	return audit, scannedRoot, recordedRoot
}
