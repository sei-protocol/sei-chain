package operations

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/audit"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// newAuditReportForTest returns a report with one database holding a module that matches and a module whose
// hash and key count do not.
func newAuditReportForTest() *audit.SnapshotAuditReport {
	good := [32]byte{0x01}
	bad := [32]byte{0x02}
	return &audit.SnapshotAuditReport{
		Version: 7,
		DBs: []audit.DBAudit{{
			Name:    "misc",
			Version: audit.VersionAudit{Expected: 7, Recorded: 7},
			Root:    audit.HashAudit{Scanned: good, Recorded: bad},
			Modules: []audit.ModuleAudit{
				{
					Module:   "bank",
					Hash:     audit.HashAudit{Scanned: good, Recorded: good},
					Scanned:  lthash.ModuleStats{KeyCount: 3, Bytes: 30},
					Recorded: lthash.ModuleStats{KeyCount: 3, Bytes: 30},
				},
				{
					Module:   "gov",
					Hash:     audit.HashAudit{Scanned: good, Recorded: bad},
					Scanned:  lthash.ModuleStats{KeyCount: 2, Bytes: 20},
					Recorded: lthash.ModuleStats{KeyCount: 5, Bytes: 20},
				},
			},
		}},
		Global: audit.HashAudit{Scanned: good, Recorded: bad},
	}
}

func TestAuditReportWithoutColor(t *testing.T) {
	var out bytes.Buffer
	printer := &auditReportPrinter{w: &out, color: false}
	printer.printReport(newAuditReportForTest(), 4)
	text := out.String()

	require.NotContains(t, text, "\x1b")
	require.Contains(t, text, "module bank\n")
	require.Contains(t, text, "result: FAIL\n")

	lines := strings.Split(text, "\n")
	gov := indexOfLine(t, lines, "  module gov")
	require.Equal(t, []string{
		"    hash            MISMATCH",
		fmt.Sprintf("      scanned       %x", [32]byte{0x01}),
		fmt.Sprintf("      recorded      %x", [32]byte{0x02}),
		"    keys            MISMATCH",
		"      scanned       2",
		"      recorded      5",
		"    logical size    20 B  OK",
	}, lines[gov+1:gov+8])
}

func TestAuditReportWithColor(t *testing.T) {
	var out bytes.Buffer
	printer := &auditReportPrinter{w: &out, color: true}
	printer.printReport(newAuditReportForTest(), 4)
	text := out.String()

	require.Contains(t, text, ansiGreen+"OK"+ansiReset)
	require.Contains(t, text, ansiBold+ansiRed+"MISMATCH"+ansiReset)
	require.Contains(t, text, ansiBold+ansiRed+"FAIL"+ansiReset)
}

// indexOfLine returns the index of the line equal to want.
func indexOfLine(t *testing.T, lines []string, want string) int {
	t.Helper()
	for i, line := range lines {
		if line == want {
			return i
		}
	}
	require.FailNow(t, "line not found", want)
	return -1
}
