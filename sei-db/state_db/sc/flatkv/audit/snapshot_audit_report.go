package audit

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// SnapshotAuditReport is the outcome of AuditSnapshot(): what a full scan of a snapshot found, beside what the
// snapshot's metadata records.
type SnapshotAuditReport struct {
	// Version is the version of the audited snapshot.
	Version uint64

	// DBs holds one entry per data database, in DataDBDirs() order.
	DBs []DBAudit

	// Global compares the store-wide root.
	Global HashAudit

	// Rows is how many data rows the scan read, metadata excluded.
	Rows uint64

	// Bytes is the key and value bytes of those rows.
	Bytes uint64

	// Elapsed is how long the whole audit took.
	Elapsed time.Duration

	// ScanElapsed is how long the scan took, from the first read to the last hasher finishing.
	ScanElapsed time.Duration

	// ReaderBlocked is how long the reader spent waiting for a hasher to take a batch.
	ReaderBlocked time.Duration

	// HasherIdle is how long the hashers spent waiting for a batch, summed over all of them.
	HasherIdle time.Duration
}

// DBAudit compares one data database's scan against its metadata.
type DBAudit struct {
	// Name is the database's directory name.
	Name string

	// Version compares the snapshot's version against the version the database records.
	Version VersionAudit

	// Root compares the database's root.
	Root HashAudit

	// DiskBytes is the total size of the database's files in the snapshot.
	DiskBytes uint64

	// Modules holds one entry per module that either the scan found or the metadata records, sorted by
	// module name.
	Modules []ModuleAudit
}

// ModuleAudit compares one module's scan against its metadata within one database.
type ModuleAudit struct {
	// Module is the module's name.
	Module string

	// Hash compares the module's hash.
	Hash HashAudit

	// Scanned is the key count and byte total the scan found.
	Scanned lthash.ModuleStats

	// Recorded is the key count and byte total the metadata records.
	Recorded lthash.ModuleStats
}

// HashAudit compares a lattice hash computed by the scan against the one the metadata records, as checksums.
type HashAudit struct {
	// Scanned is the checksum of the hash the scan computed.
	Scanned [32]byte

	// Recorded is the checksum of the hash the metadata records.
	Recorded [32]byte
}

// VersionAudit compares the version a database is expected to be at against the version it records.
type VersionAudit struct {
	// Expected is the version of the snapshot the database belongs to.
	Expected uint64

	// Recorded is the version the database's metadata records.
	Recorded uint64
}

// Passed reports whether every comparison in the report matched.
func (r *SnapshotAuditReport) Passed() bool {
	for i := range r.DBs {
		if !r.DBs[i].Passed() {
			return false
		}
	}
	return r.Global.Matches()
}

// ScannedStats returns the key count and logical byte total the scan found across every data database.
func (r *SnapshotAuditReport) ScannedStats() lthash.ModuleStats {
	var total lthash.ModuleStats
	for i := range r.DBs {
		total = total.Add(r.DBs[i].ScannedStats())
	}
	return total
}

// DiskBytes returns the total size of the files of every data database in the snapshot.
func (r *SnapshotAuditReport) DiskBytes() uint64 {
	var total uint64
	for i := range r.DBs {
		total += r.DBs[i].DiskBytes
	}
	return total
}

// LogicalToDiskRatio returns the scanned logical byte total of every data database divided by their total size
// on disk, or 0 when nothing is on disk.
func (r *SnapshotAuditReport) LogicalToDiskRatio() float64 {
	return logicalToDiskRatio(r.ScannedStats().Bytes, r.DiskBytes())
}

// ScannedStats returns the key count and logical byte total the scan found across the database's modules.
func (d *DBAudit) ScannedStats() lthash.ModuleStats {
	var total lthash.ModuleStats
	for i := range d.Modules {
		total = total.Add(d.Modules[i].Scanned)
	}
	return total
}

// RecordedStats returns the key count and logical byte total the metadata records across the database's modules.
func (d *DBAudit) RecordedStats() lthash.ModuleStats {
	var total lthash.ModuleStats
	for i := range d.Modules {
		total = total.Add(d.Modules[i].Recorded)
	}
	return total
}

// LogicalToDiskRatio returns the database's scanned logical byte total divided by its size on disk, or 0 when
// nothing is on disk.
func (d *DBAudit) LogicalToDiskRatio() float64 {
	return logicalToDiskRatio(d.ScannedStats().Bytes, d.DiskBytes)
}

// Passed reports whether the database's version, root and every module matched.
func (d *DBAudit) Passed() bool {
	if !d.Version.Matches() || !d.Root.Matches() {
		return false
	}
	for i := range d.Modules {
		if !d.Modules[i].Matches() {
			return false
		}
	}
	return true
}

// Matches reports whether the module's hash and stats both matched.
func (m *ModuleAudit) Matches() bool {
	return m.Hash.Matches() && m.Scanned == m.Recorded
}

// Matches reports whether the scanned and recorded hashes are equal.
func (h HashAudit) Matches() bool {
	return h.Scanned == h.Recorded
}

// Matches reports whether the database records the expected version.
func (v VersionAudit) Matches() bool {
	return v.Expected == v.Recorded
}

// logicalToDiskRatio returns logicalBytes divided by diskBytes, or 0 when diskBytes is 0.
func logicalToDiskRatio(logicalBytes int64, diskBytes uint64) float64 {
	if diskBytes == 0 {
		return 0
	}
	return float64(logicalBytes) / float64(diskBytes)
}
