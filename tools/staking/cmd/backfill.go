package cmd

import (
	"bufio"
	"fmt"
	"os"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/input"
	stakingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	stakingtool "github.com/sei-protocol/sei-chain/tools/staking"
)

const backfillDelegationIndexNotice = `This command is for benchmarking and analysis only. It measures how long it
takes to populate the validator-indexed delegation secondary index. Do NOT use
it to migrate live chain state.

How to use:
  1. Stop the node whose state you want to measure.
  2. Copy the node home directory to a path outside your real Sei data directory
     (for example: cp -a ~/.sei /tmp/sei-backfill-bench).
  3. Run a dry-run against the copy (default; does not modify state):
       seid tools staking backfill-delegation-index --home /path/to/copy
  4. To measure write and commit cost on the copy only, add --write:
       seid tools staking backfill-delegation-index --home /path/to/copy --write
  5. Review elapsed time and delegations_per_second in the output.

Never point --home at a running node's directory. Never use --write on production
data. --write cannot be combined with --height.`

// BackfillDelegationIndexCmd backfills validator-indexed delegation keys for benchmarking.
func BackfillDelegationIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backfill-delegation-index",
		Short: "Benchmark validator-indexed delegation index population (not for live migration)",
		Long:  backfillDelegationIndexNotice,
		RunE:  runBackfillDelegationIndex,
	}

	stakingtool.BindServerFlags(cmd, app.DefaultNodeHome)
	cmd.Flags().Int64("height", 0, "Load state at this height on the copy (0 = latest committed)")
	cmd.Flags().Bool("write", false, "Write index keys and commit state on the copy (benchmark only; never on live data)")
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

	fmt.Fprintln(os.Stderr, backfillDelegationIndexNotice)
	if write {
		fmt.Fprintln(os.Stderr, "WARNING: --write mutates state in the directory passed to --home. Use a copy, not live data.")
	}

	confirmed, err := input.GetConfirmation(
		"Proceed with this benchmark run on the directory passed to --home?",
		bufio.NewReader(cmd.InOrStdin()),
		cmd.ErrOrStderr(),
	)
	if err != nil {
		return err
	}
	if !confirmed {
		return fmt.Errorf("aborted")
	}

	fmt.Fprintln(os.Stderr, "loading application state...")
	seiApp, err := stakingtool.LoadApp(cmd, height)
	if err != nil {
		return err
	}
	defer func() {
		if err := seiApp.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing sei app: %v", err)
		}
	}()

	blockHeight := seiApp.LastBlockHeight()
	if blockHeight == 0 {
		blockHeight = 1
	}

	ctx := seiApp.NewUncachedContext(false, tmproto.Header{Height: blockHeight})
	fmt.Fprintln(os.Stderr, "backfill started")
	result := seiApp.StakingKeeper.BackfillDelegationByValIndex(ctx, dryRun, reportBackfillProgress)

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

func reportBackfillProgress(p stakingkeeper.BackfillDelegationByValIndexProgress) {
	perSec := 0.0
	if p.Elapsed > 0 {
		perSec = float64(p.TotalDelegations) / p.Elapsed.Seconds()
	}
	fmt.Fprintf(os.Stderr,
		"progress total_delegations=%d index_written=%d already_indexed=%d elapsed=%s delegations_per_second=%.2f\n",
		p.TotalDelegations,
		p.IndexWritten,
		p.AlreadyIndexed,
		p.Elapsed,
		perSec,
	)
}
