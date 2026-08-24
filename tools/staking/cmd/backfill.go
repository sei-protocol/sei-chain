package cmd

import (
	"fmt"
	"os"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/app"
	stakingtool "github.com/sei-protocol/sei-chain/tools/staking"
)

// BackfillDelegationIndexCmd backfills validator-indexed delegation keys for benchmarking.
func BackfillDelegationIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backfill-delegation-index",
		Short: "Backfill validator-indexed delegation keys (dev/benchmark)",
		Long: `Iterate all delegations and write validator-indexed secondary index keys.

By default this runs in dry-run mode and does not modify state. Pass --write to
persist index keys. When using --write, stop the node and operate on a copy of
the data directory. --write cannot be combined with --height.`,
		RunE: runBackfillDelegationIndex,
	}

	stakingtool.BindServerFlags(cmd, app.DefaultNodeHome)
	cmd.Flags().Int64("height", 0, "Load state at this height (0 = latest committed)")
	cmd.Flags().Bool("write", false, "Write index keys and commit state (requires stopped node)")
	return cmd
}

func runBackfillDelegationIndex(cmd *cobra.Command, _ []string) error {
	height, err := cmd.Flags().GetInt64("height")
	if err != nil {
		return err
	}
	write, err := cmd.Flags().GetBool("write")
	if err != nil {
		return err
	}
	dryRun := !write

	if write && height > 0 {
		return fmt.Errorf("--write cannot be used with --height")
	}

	if write {
		fmt.Fprintln(os.Stderr, "WARNING: --write mutates application state. Stop the node and use a data copy.")
	}

	seiApp, err := stakingtool.LoadApp(cmd, height)
	if err != nil {
		return err
	}
	defer seiApp.Close()

	blockHeight := seiApp.LastBlockHeight()
	if blockHeight == 0 {
		blockHeight = 1
	}

	ctx := seiApp.NewUncachedContext(false, tmproto.Header{Height: blockHeight})
	result := seiApp.StakingKeeper.BackfillDelegationByValIndex(ctx, dryRun)

	fmt.Printf("dry_run=%t height=%d\n", result.DryRun, blockHeight)
	fmt.Printf("total_delegations=%d index_written=%d already_indexed=%d elapsed=%s\n",
		result.TotalDelegations,
		result.IndexWritten,
		result.AlreadyIndexed,
		result.Elapsed,
	)
	if result.TotalDelegations > 0 && result.Elapsed > 0 {
		perSec := float64(result.TotalDelegations) / result.Elapsed.Seconds()
		fmt.Printf("delegations_per_second=%.2f\n", perSec)
	}

	if !write {
		return nil
	}

	commitID := seiApp.CommitMultiStore().Commit(true)
	fmt.Printf("committed_version=%d\n", commitID.Version)
	return nil
}
