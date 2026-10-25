package operations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/audit"
)

// AuditFlatKVCmd is the seidb subcommand that audits one FlatKV snapshot: it recomputes the snapshot's
// per-module, per-database and store-wide lattice hashes and stats from a full scan, and compares them against
// the snapshot's metadata. It exits non-zero when any comparison fails.
func AuditFlatKVCmd() *cobra.Command {
	defaults := audit.DefaultSnapshotAuditConfig()
	cmd := &cobra.Command{
		Use:   "audit-flatkv <flatkv-dir>",
		Short: "Verify a FlatKV snapshot's lattice hashes and key counts against a full scan of its rows",
		Args:  cobra.ExactArgs(1),
		RunE:  executeAuditFlatKV,
		// A failed audit has nothing to do with how the command was invoked.
		SilenceUsage: true,
	}
	cmd.Flags().Uint64("snapshot-version", 0, "Snapshot to audit; 0 selects the snapshot the current link names")
	cmd.Flags().Float64("read-limit-mb", 0, "Cap on key and value bytes read, in MiB/s; 0 means no cap")
	cmd.Flags().Uint32("hashers", defaults.Hashers, "Number of goroutines hashing rows")
	cmd.Flags().Bool("no-color", false, "Print the report without ANSI colors")
	cmd.Flags().Bool("keep-stale-audit-dirs", false,
		"Leave the directories earlier audits left under <flatkv-dir> in place; otherwise they are removed, "+
			"including that of an audit still running")
	return cmd
}

func executeAuditFlatKV(cmd *cobra.Command, args []string) error {
	// The FlatKV root directory, the one holding current and snapshot-*.
	dbDir := args[0]
	version, _ := cmd.Flags().GetUint64("snapshot-version")
	readLimitMiBps, _ := cmd.Flags().GetFloat64("read-limit-mb")
	hashers, _ := cmd.Flags().GetUint32("hashers")
	noColor, _ := cmd.Flags().GetBool("no-color")
	keepStaleAuditDirs, _ := cmd.Flags().GetBool("keep-stale-audit-dirs")
	if readLimitMiBps < 0 {
		return errors.New("--read-limit-mb must be at least 0")
	}

	config := audit.DefaultSnapshotAuditConfig()
	config.Root = dbDir
	config.Version = version
	config.ReadBytesPerSecond = uint64(readLimitMiBps * bytesPerMiB)
	config.Hashers = hashers
	config.KeepStaleAuditDirs = keepStaleAuditDirs
	// Kept off stdout, which carries the report.
	config.Log = cmd.ErrOrStderr()

	// A cancelled audit still removes the directory it linked the snapshot into.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report, err := audit.AuditSnapshot(ctx, config)
	if err != nil {
		return fmt.Errorf("audit flatkv snapshot: %w", err)
	}
	printer := &auditReportPrinter{w: cmd.OutOrStdout(), color: !noColor}
	printer.printReport(report, config.Hashers)
	if !report.Passed() {
		return fmt.Errorf("flatkv snapshot %d failed its audit", report.Version)
	}
	return nil
}
