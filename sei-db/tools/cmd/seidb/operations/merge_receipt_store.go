package operations

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
)

// receiptStoreKey is the store key receipts are written under. It must equal
// x/evm/types.ReceiptStoreKey; importing that package would link the EVM
// module and its dependencies into seidb.
const receiptStoreKey = "receipt"

// MergeReceiptStoreCmd is the seidb subcommand that copies every receipt in one
// pebble receipt store into another.
func MergeReceiptStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge-receipt-store",
		Short: "Copy every receipt in one pebble receipt store into another",
		Long: "Copies every receipt version in --source into --target, re-encoding versions when the two stores " +
			"use different MVCC encodings, and widens the target's earliest and latest versions to cover both, " +
			"so a node opening the target serves the receipts of both stores. The source is opened read-only.\n\n" +
			"Stop the nodes owning both stores first: a store open elsewhere is refused, as are stores whose " +
			"version ranges overlap unless --allow-overlap is set, and stores whose version ranges leave heights " +
			"between them that neither covers unless --allow-gap is set. A node serves the heights in such a gap " +
			"as blocks without receipts instead of refusing them as pruned. These checks read the version " +
			"markers only; check-receipt-store finds runs of blocks without receipts inside a store. An " +
			"interrupted merge leaves the target's versions as they were and can be run again. The run time " +
			"follows the size of the source, so copy the smaller store into the larger one.",
		Args:         cobra.NoArgs,
		RunE:         executeMergeReceiptStore,
		SilenceUsage: true,
		// main prints the returned error.
		SilenceErrors: true,
	}
	cmd.Flags().String("source", "",
		"Receipt store directory to copy from: <home>/data/receipt.db, or <home>/data/ledger/receipt/pebbledb")
	cmd.Flags().String("target", "", "Receipt store directory to copy into")
	cmd.Flags().Bool("allow-overlap", false,
		"Accept stores whose version ranges overlap, keeping the target's copy of any receipt both hold")
	cmd.Flags().Bool("allow-gap", false,
		"Accept stores whose version ranges leave heights between them that neither covers")
	cmd.Flags().Bool("dry-run", false, "Make every check and read the whole source without writing to the target")
	cmd.Flags().Int64("verify-every", 10_000,
		"Read back every Nth written receipt through the store's read path once merged; 0 reads none back")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}

func executeMergeReceiptStore(cmd *cobra.Command, _ []string) error {
	source, _ := cmd.Flags().GetString("source")
	target, _ := cmd.Flags().GetString("target")
	allowOverlap, _ := cmd.Flags().GetBool("allow-overlap")
	allowGap, _ := cmd.Flags().GetBool("allow-gap")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	verifyEvery, _ := cmd.Flags().GetInt64("verify-every")
	if verifyEvery < 0 {
		return errors.New("--verify-every must be at least 0")
	}

	report, err := mvcc.Merge(target, source, mvcc.MergeOptions{
		StoreKeys:    []string{receiptStoreKey},
		AllowOverlap: allowOverlap,
		AllowGap:     allowGap,
		DryRun:       dryRun,
		VerifyEvery:  verifyEvery,
		// Kept off stdout, which carries the report.
		Progress: func(r mvcc.MergeReport) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "entries read: %d\n", r.Entries)
		},
	})
	out := cmd.OutOrStdout()
	printMergeSide(out, "source", report.Source)
	printMergeSide(out, "target", report.Target)
	if err != nil {
		return fmt.Errorf("merge receipt store: %w", err)
	}
	printMergeOutcome(out, report, dryRun)
	return nil
}

func printMergeSide(w io.Writer, name string, side mvcc.DBInfo) {
	if side.Dir == "" {
		return
	}
	_, _ = fmt.Fprintf(w, "%s %s: %s\n", name, side.Dir, describeDB(side))
}

// describeDB renders a DB's encoding, version markers and stores on one line.
func describeDB(db mvcc.DBInfo) string {
	encoding := "ascending"
	if db.Descending {
		encoding = "descending"
	}
	return fmt.Sprintf("%s encoding, versions %d-%d, stores %q", encoding, db.Earliest, db.Latest, db.StoreKeys)
}

func printMergeOutcome(w io.Writer, r mvcc.MergeReport, dryRun bool) {
	written, versions := "entries written", "target versions now"
	gap := "versions neither store held, now in the target's range"
	if dryRun {
		written, versions = "entries to write", "target versions after merging"
		gap = "versions neither store holds, in the target's range after merging"
	}
	_, _ = fmt.Fprintf(w, "entries read: %d\n", r.Entries)
	_, _ = fmt.Fprintf(w, "%s: %d (%.0f MiB)\n", written, r.Written, float64(r.Bytes)/bytesPerMiB)
	if r.Held > 0 {
		_, _ = fmt.Fprintf(w, "entries the target already held, left as they were: %d (%d with a different value)\n",
			r.Held, r.Conflicting)
	}
	_, _ = fmt.Fprintf(w, "%s: %d-%d\n", versions, r.Earliest, r.Latest)
	if r.Gap != (mvcc.VersionRange{}) {
		_, _ = fmt.Fprintf(w, "%s: %d-%d\n", gap, r.Gap.From, r.Gap.To)
	}
	if !dryRun {
		_, _ = fmt.Fprintf(w, "entries read back through the store's read path: %d\n", r.Verified)
	}
}
