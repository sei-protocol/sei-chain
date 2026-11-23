package operations

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/audit"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiRed   = "\x1b[31m"
	ansiGreen = "\x1b[32m"
	ansiCyan  = "\x1b[36m"

	// auditReportValueColumn is the column every value in the report starts at, so values line up regardless
	// of how deeply their label is indented.
	auditReportValueColumn = 20
)

// auditReportPrinter writes a SnapshotAuditReport as indented sections of one value per line.
type auditReportPrinter struct {
	// w receives the report.
	w io.Writer

	// color is whether the report is painted with ANSI escapes.
	color bool
}

// printReport writes report: a section per database holding a section per module, then the store-wide totals,
// the scan's throughput, and the verdict.
func (p *auditReportPrinter) printReport(
	report *audit.SnapshotAuditReport,
	// How many hashers the audit ran, which the hasher idle time is a fraction of.
	hashers uint32,
) {
	p.section(0, fmt.Sprintf("FlatKV snapshot %d audit", report.Version))
	for i := range report.DBs {
		p.blank()
		p.printDB(&report.DBs[i])
	}

	p.blank()
	stats := report.ScannedStats()
	p.section(0, "global")
	p.check(1, "root", "scanned", p.hash(report.Global.Scanned), p.hash(report.Global.Recorded),
		report.Global.Matches())
	p.field(1, "keys", fmt.Sprint(stats.KeyCount))
	p.field(1, "logical size", formatAuditBytes(float64(stats.Bytes)))
	p.field(1, "disk size", formatAuditBytes(float64(report.DiskBytes())))
	p.field(1, "logical/disk", fmt.Sprintf("%.2f", report.LogicalToDiskRatio()))

	p.blank()
	scanSeconds := max(report.ScanElapsed.Seconds(), 1e-9)
	p.section(0, "scan")
	p.field(1, "rows", fmt.Sprint(report.Rows))
	p.field(1, "read", formatAuditBytes(float64(report.Bytes)))
	p.field(1, "duration", report.ScanElapsed.Truncate(time.Millisecond).String())
	p.field(1, "throughput", formatAuditBytes(float64(report.Bytes)/scanSeconds)+"/s")
	p.field(1, "reader blocked",
		fmt.Sprintf("%.1f%% of the scan", 100*report.ReaderBlocked.Seconds()/scanSeconds))
	p.field(1, "hashers idle",
		fmt.Sprintf("%.1f%% of hasher time", 100*report.HasherIdle.Seconds()/(scanSeconds*float64(hashers))))
	p.field(1, "audit duration", report.Elapsed.Truncate(time.Millisecond).String())

	p.blank()
	if report.Passed() {
		p.line(0, p.paint(ansiBold, "result: ")+p.paint(ansiBold+ansiGreen, "PASS"))
	} else {
		p.line(0, p.paint(ansiBold, "result: ")+p.paint(ansiBold+ansiRed, "FAIL"))
	}
}

// printDB writes one database's section, with a nested section per module.
func (p *auditReportPrinter) printDB(db *audit.DBAudit) {
	scanned := db.ScannedStats()
	recorded := db.RecordedStats()
	p.section(0, db.Name)
	p.check(1, "version", "snapshot", fmt.Sprint(db.Version.Expected), fmt.Sprint(db.Version.Recorded),
		db.Version.Matches())
	p.check(1, "root", "scanned", p.hash(db.Root.Scanned), p.hash(db.Root.Recorded), db.Root.Matches())
	p.printStats(1, scanned, recorded)
	p.field(1, "disk size", formatAuditBytes(float64(db.DiskBytes)))
	p.field(1, "logical/disk", fmt.Sprintf("%.2f", db.LogicalToDiskRatio()))
	for i := range db.Modules {
		module := &db.Modules[i]
		p.blank()
		p.section(1, "module "+module.Module)
		p.check(2, "hash", "scanned", p.hash(module.Hash.Scanned), p.hash(module.Hash.Recorded),
			module.Hash.Matches())
		p.printStats(2, module.Scanned, module.Recorded)
	}
}

// printStats writes the key count and logical size the scan found, each checked against what the metadata
// records.
func (p *auditReportPrinter) printStats(indent int, scanned lthash.ModuleStats, recorded lthash.ModuleStats) {
	p.check(indent, "keys", "scanned", fmt.Sprint(scanned.KeyCount), fmt.Sprint(recorded.KeyCount),
		scanned.KeyCount == recorded.KeyCount)
	p.check(indent, "logical size", "scanned", formatAuditBytes(float64(scanned.Bytes)),
		formatAuditBytes(float64(recorded.Bytes)), scanned.Bytes == recorded.Bytes)
}

// check writes a compared value. A match is one line holding the value and OK. A mismatch is a line holding
// MISMATCH, followed by one line for each side.
func (p *auditReportPrinter) check(
	indent int,
	label string,
	// The name of the side value belongs to, printed only on a mismatch. The other side is always "recorded".
	side string,
	value string,
	recorded string,
	ok bool,
) {
	if ok {
		p.field(indent, label, value+"  "+p.paint(ansiGreen, "OK"))
		return
	}
	p.field(indent, label, p.paint(ansiBold+ansiRed, "MISMATCH"))
	p.field(indent+1, side, value)
	p.field(indent+1, "recorded", recorded)
}

// section writes a bold heading, colored cyan when it is a top-level one.
func (p *auditReportPrinter) section(indent int, title string) {
	if indent == 0 {
		p.line(indent, p.paint(ansiBold+ansiCyan, title))
		return
	}
	p.line(indent, p.paint(ansiBold, title))
}

// field writes a label followed by its value, which starts at auditReportValueColumn.
func (p *auditReportPrinter) field(indent int, label string, value string) {
	padding := max(auditReportValueColumn-2*indent-len(label), 2)
	p.line(indent, label+strings.Repeat(" ", padding)+value)
}

// line writes text indented by indent levels of two spaces.
func (p *auditReportPrinter) line(indent int, text string) {
	_, _ = fmt.Fprintln(p.w, strings.Repeat("  ", indent)+text)
}

// blank writes an empty line.
func (p *auditReportPrinter) blank() {
	_, _ = fmt.Fprintln(p.w)
}

// hash renders a lattice hash checksum, dimmed.
func (p *auditReportPrinter) hash(checksum [32]byte) string {
	return p.paint(ansiDim, fmt.Sprintf("%x", checksum))
}

// paint wraps text in the ANSI escape code, or returns it unchanged when color is off.
func (p *auditReportPrinter) paint(code string, text string) string {
	if !p.color {
		return text
	}
	return code + text + ansiReset
}

// formatAuditBytes renders a byte count in binary units.
func formatAuditBytes(value float64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%.0f B", value)
	}
	suffixes := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	i := -1
	for value >= unit && i < len(suffixes)-1 {
		value /= unit
		i++
	}
	return fmt.Sprintf("%.2f %s", value, suffixes[i])
}
