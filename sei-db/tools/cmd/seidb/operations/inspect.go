package operations

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
)

// versionBuckets are the upper bounds of the version-depth histogram --scan prints, chosen to
// span a single-version keyspace through a deeply-versioned one on a log scale.
var versionBuckets = []int{1, 2, 5, 10, 50, 100, 500, 1000, 10000}

func InspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Print entry counts, sizes and LSM shape for a PebbleDB state store",
		Long: "Reports what each PebbleDB sub-store physically holds, in the style of `geth db inspect`.\n" +
			"The default report is metadata only and returns immediately. --scan adds the distinct\n" +
			"logical key count and the version-depth histogram, which require a full iteration.\n" +
			"The database must not be open in another process: PebbleDB takes a directory lock even\n" +
			"in read-only mode, so a running node or benchmark has to be stopped first.",
		RunE: executeInspect,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "PebbleDB state store directory")
	cmd.PersistentFlags().Bool("scan", false, "iterate every key to count distinct logical keys and their version depth")

	return cmd
}

// storeStats is what one PebbleDB sub-store holds. Entries and the raw sizes come from Pebble's
// table stats, which its background collector maintains, so everything up to scanned is free to
// read; the scanned fields are filled only by --scan.
type storeStats struct {
	name string

	entries    uint64 // physical entries across all sstables: one per key version, plus tombstones
	tombstones uint64
	rawKeys    uint64 // uncompressed key bytes
	rawValues  uint64 // uncompressed value bytes
	diskSize   uint64
	tables     int64
	levels     []int64 // tables per LSM level, indexed by level
	memtable   uint64  // bytes replayed from the WAL into a memtable, which no sstable holds yet
	statsStale int     // tables that exposed no properties block

	scanned     bool
	distinct    uint64 // distinct logical keys, ignoring versions
	scannedRows uint64
	histogram   []uint64 // counts per versionBuckets entry, plus one overflow bucket
	scanElapsed time.Duration
}

func executeInspect(cmd *cobra.Command, _ []string) error {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	scan, _ := cmd.Flags().GetBool("scan")
	if dbDir == "" {
		return errors.New("must provide --db-dir")
	}

	dirs, err := pebbleDirs(dbDir)
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		return fmt.Errorf("no PebbleDB store found in %s", dbDir)
	}

	stats := make([]storeStats, 0, len(dirs))
	for _, dir := range dirs {
		s, err := inspectStore(dir, scan)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", dir, err)
		}
		stats = append(stats, s)
	}

	printReport(os.Stdout, dbDir, stats, scan)
	return nil
}

// pebbleDirs returns the PebbleDB directories to report on: dbDir itself when it is one store,
// otherwise every immediate subdirectory that is. Both layouts occur, since SeparateEVMSubDBs
// splits the EVM store into one PebbleDB per key kind.
func pebbleDirs(dbDir string) ([]string, error) {
	if isPebbleDir(dbDir) {
		return []string{dbDir}, nil
	}

	children, err := os.ReadDir(dbDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dbDir, err)
	}
	var dirs []string
	for _, child := range children {
		if !child.IsDir() {
			continue
		}
		if path := filepath.Join(dbDir, child.Name()); isPebbleDir(path) {
			dirs = append(dirs, path)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// isPebbleDir reports whether dir holds a PebbleDB. A manifest is the one file every store has
// from the moment it is created, whichever format version wrote it.
func isPebbleDir(dir string) bool {
	matches, err := filepath.Glob(filepath.Join(dir, "MANIFEST-*"))
	return err == nil && len(matches) > 0
}

// silentLogger keeps Pebble's open-time WAL replay chatter out of the report.
type silentLogger struct{}

func (silentLogger) Infof(string, ...interface{})  {}
func (silentLogger) Errorf(string, ...interface{}) {}
func (silentLogger) Fatalf(string, ...interface{}) {}

// openReadOnly opens dir without writing to it. The comparer name is recorded in the manifest and
// Pebble refuses to open on a mismatch, so a store written with the default comparer is retried
// with it rather than reported as unopenable.
func openReadOnly(dir string) (*pebble.DB, error) {
	db, err := pebble.Open(dir, &pebble.Options{Comparer: mvcc.MVCCComparer, ReadOnly: true, Logger: silentLogger{}})
	if err == nil {
		return db, nil
	}
	if !strings.Contains(err.Error(), "comparer name") {
		return nil, err
	}
	return pebble.Open(dir, &pebble.Options{ReadOnly: true, Logger: silentLogger{}})
}

func inspectStore(dir string, scan bool) (_ storeStats, _err error) {
	db, err := openReadOnly(dir)
	if err != nil {
		return storeStats{}, err
	}
	defer func() {
		if cerr := db.Close(); cerr != nil && _err == nil {
			_err = cerr
		}
	}()

	s := storeStats{name: filepath.Base(dir)}

	m := db.Metrics()
	s.diskSize = m.DiskSpaceUsage()
	s.tables = m.Total().TablesCount
	s.memtable = m.MemTable.Size
	// Pebble's own ReadAmp() sums L0 sublevels, which only its compaction picker computes and a
	// read-only open never starts, so the level shape is reported directly instead.
	s.levels = make([]int64, len(m.Levels))
	for level := range m.Levels {
		s.levels[level] = m.Levels[level].TablesCount
	}

	// WithProperties reads each table's properties block rather than taking the manifest's cached
	// table stats. It costs one small read per sstable, but the cached stats are maintained by a
	// background collector that a read-only open never starts, so they would all read zero here.
	levels, err := db.SSTables(pebble.WithProperties())
	if err != nil {
		return storeStats{}, fmt.Errorf("list sstables: %w", err)
	}
	for _, level := range levels {
		for _, table := range level {
			if table.Properties == nil {
				s.statsStale++
				continue
			}
			s.entries += table.Properties.NumEntries
			s.tombstones += table.Properties.NumDeletions
			s.rawKeys += table.Properties.RawKeySize
			s.rawValues += table.Properties.RawValueSize
		}
	}

	if scan {
		if err := scanStore(db, &s); err != nil {
			return storeStats{}, fmt.Errorf("scan: %w", err)
		}
	}
	return s, nil
}

// scanStore walks every live key to separate logical keys from their versions, which no metadata
// Pebble keeps can distinguish: the version is encoded into the key, so an sstable entry count
// counts versions. This is the same full forward scan a prune pass makes, and costs the same.
func scanStore(db *pebble.DB, s *storeStats) error {
	start := time.Now()
	itr, err := db.NewIter(nil)
	if err != nil {
		return err
	}
	defer func() { _ = itr.Close() }()

	s.histogram = make([]uint64, len(versionBuckets)+1)

	var prev []byte
	var versions int
	for itr.First(); itr.Valid(); itr.Next() {
		s.scannedRows++
		key, _, ok := mvcc.SplitMVCCKey(itr.Key())
		if !ok {
			continue
		}
		if prev != nil && string(key) == string(prev) {
			versions++
			continue
		}
		if prev != nil {
			s.histogram[bucketOf(versions)]++
		}
		prev = key
		versions = 1
		s.distinct++
	}
	if prev != nil {
		s.histogram[bucketOf(versions)]++
	}

	s.scanned = true
	s.scanElapsed = time.Since(start)
	return itr.Error()
}

func bucketOf(versions int) int {
	for i, bound := range versionBuckets {
		if versions <= bound {
			return i
		}
	}
	return len(versionBuckets)
}

// printf writes one piece of the report. A write to stdout or to a tabwriter over it has no
// failure this tool could act on, so the error is dropped here rather than at every call site.
func printf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func printReport(out io.Writer, dbDir string, stats []storeStats, scan bool) {
	var total storeStats
	for _, s := range stats {
		total.entries += s.entries
		total.tombstones += s.tombstones
		total.rawKeys += s.rawKeys
		total.rawValues += s.rawValues
		total.diskSize += s.diskSize
		total.tables += s.tables
		total.memtable += s.memtable
		total.distinct += s.distinct
		total.statsStale += s.statsStale
	}

	printf(out, "\n%s\n\n", dbDir)
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)

	header := "STORE\tENTRIES\tSIZE\tSHARE\tTOMBSTONES\tRAW KEY\tRAW VALUE\tCOMPR\tTABLES\t"
	if scan {
		header += "KEYS\tVER/KEY\t"
	}
	printf(w, "%s\n", header)

	for _, s := range stats {
		row := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t",
			s.name, count(s.entries), size(s.diskSize), share(s.diskSize, total.diskSize),
			count(s.tombstones), size(s.rawKeys), size(s.rawValues),
			ratio(s.diskSize, s.rawKeys+s.rawValues), s.tables)
		if scan {
			row += fmt.Sprintf("%s\t%s\t", count(s.distinct), perKey(s.scannedRows, s.distinct))
		}
		printf(w, "%s\n", row)
	}

	totalRow := fmt.Sprintf("TOTAL\t%s\t%s\t\t%s\t%s\t%s\t%s\t%d\t",
		count(total.entries), size(total.diskSize), count(total.tombstones),
		size(total.rawKeys), size(total.rawValues),
		ratio(total.diskSize, total.rawKeys+total.rawValues), total.tables)
	if scan {
		totalRow += fmt.Sprintf("%s\t\t", count(total.distinct))
	}
	printf(w, "%s\n", totalRow)
	_ = w.Flush()

	printf(out, "\nENTRIES counts key versions, not keys: the version is part of the on-disk key.\n")
	if !scan {
		printf(out, "Pass --scan for the distinct key count and version-depth histogram.\n")
	}
	for _, s := range stats {
		if s.tables == 0 && s.memtable > 0 {
			printf(out, "%s holds %s in an unflushed memtable that no sstable covers yet.\n", s.name, size(s.memtable))
		}
	}
	if total.statsStale > 0 {
		printf(out, "%d table(s) exposed no properties, so ENTRIES is an undercount.\n", total.statsStale)
	}
	printLevels(out, stats)
	if scan {
		printHistogram(out, stats)
	}
	printf(out, "\n")
}

// printLevels shows how the tables are spread across the LSM, which says whether a store has
// settled into its bottom level or still has a backlog waiting to be compacted down.
func printLevels(out io.Writer, stats []storeStats) {
	depth := 0
	for _, s := range stats {
		if len(s.levels) > depth {
			depth = len(s.levels)
		}
	}
	if depth == 0 {
		return
	}

	printf(out, "\nTables per level\n\n")
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)

	header := "STORE"
	for level := 0; level < depth; level++ {
		header += fmt.Sprintf("\tL%d", level)
	}
	printf(w, "%s\n", header+"\t")

	for _, s := range stats {
		row := s.name
		for level := 0; level < depth; level++ {
			if level < len(s.levels) {
				row += "\t" + count(s.levels[level])
			} else {
				row += "\t-"
			}
		}
		printf(w, "%s\n", row+"\t")
	}
	_ = w.Flush()
}

func printHistogram(out io.Writer, stats []storeStats) {
	printf(out, "\nVersions per key\n\n")
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)

	labels := make([]string, 0, len(versionBuckets)+1)
	prev := 0
	for _, bound := range versionBuckets {
		if bound == prev+1 {
			labels = append(labels, fmt.Sprintf("%d", bound))
		} else {
			labels = append(labels, fmt.Sprintf("%d-%d", prev+1, bound))
		}
		prev = bound
	}
	labels = append(labels, fmt.Sprintf(">%d", prev))

	header := "STORE"
	for _, label := range labels {
		header += "\t" + label
	}
	printf(w, "%s\n", header+"\tSCAN\t")

	for _, s := range stats {
		if !s.scanned {
			continue
		}
		row := s.name
		for _, n := range s.histogram {
			row += "\t" + count(n)
		}
		printf(w, "%s\n", row+"\t"+s.scanElapsed.Round(time.Millisecond).String()+"\t")
	}
	_ = w.Flush()
}

func count[T uint64 | int64](n T) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func size(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value, exp := float64(bytes)/unit, 0
	for value >= unit && exp < 4 {
		value /= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", value, "KMGTP"[exp])
}

func share(part, whole uint64) string {
	if whole == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(part)/float64(whole))
}

// ratio reports how much smaller the store is on disk than the raw bytes written into it.
func ratio(diskSize, raw uint64) string {
	if diskSize == 0 || raw == 0 {
		return "-"
	}
	return fmt.Sprintf("%.2fx", float64(raw)/float64(diskSize))
}

func perKey(rows, distinct uint64) string {
	if distinct == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(rows)/float64(distinct))
}
