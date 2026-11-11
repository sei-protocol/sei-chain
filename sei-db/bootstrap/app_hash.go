package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// completeAppHashBuilderSetup connects the state DB's hashes to the app hash builder, and completes its setup at the
// height recovery left the state on, unless deferSetup leaves the setup to the caller.
//
// It must run after every other store has recovered, and before any block is executed.
func completeAppHashBuilderSetup(
	ctx context.Context,
	// Leaves SetupComplete() to the caller.
	deferSetup bool,
	stateDB *giga.StateDB,
	appHashBuilder builder.AppHashBuilder,
) error {
	start := time.Now()
	logger.Info("Completing the app hash builder setup")
	stateHeight, err := stateDB.SC().GetLatestVersion()
	if err != nil {
		return fmt.Errorf("read state height: %w", err)
	}
	//nolint:gosec // state heights are non-negative
	height := uint64(stateHeight)
	if err := reportStateHashesToAppHashBuilder(ctx, stateDB, appHashBuilder, height); err != nil {
		return fmt.Errorf("connect the app hash builder to the state DB: %w", err)
	}
	if deferSetup {
		logger.Info("Leaving the app hash builder setup to the caller",
			"height", height, "elapsed", time.Since(start))
		return nil
	}
	if err := appHashBuilder.SetupComplete(ctx, height); err != nil {
		return fmt.Errorf("set up at block %d: %w", height, err)
	}
	logger.Info("Completed the app hash builder setup", "height", height, "elapsed", time.Since(start))
	return nil
}

// reportStateHashesToAppHashBuilder reports the state hash of block height to the app hash builder, and registers the
// builder for the state hash of every block committed from here on. Errors unless the state stands at block height.
//
// The builder checks the reported hash against the app hash it already stores for block height, and halts the node
// on a mismatch before its setup completes.
func reportStateHashesToAppHashBuilder(
	ctx context.Context,
	stateDB *giga.StateDB,
	appHashBuilder builder.AppHashBuilder,
	// The block recovery left the state on.
	height uint64,
) error {
	// The blocks replayed during recovery are hashed asynchronously. Their hashes have to reach the listeners
	// already registered before the builder registers, so that the builder's first delivery follows height.
	if err := stateDB.SC().FlushHashes(); err != nil {
		return fmt.Errorf("flush state hashes: %w", err)
	}
	reportStateHash := stateHashReporter(appHashBuilder)
	stateHash, err := stateDB.RegisterHashListener(reportStateHash)
	if err != nil {
		return fmt.Errorf("register app hash builder for state hashes: %w", err)
	}
	//nolint:gosec // state heights are non-negative
	if stateHashHeight := uint64(stateHash.BlockNumber); stateHashHeight != height {
		return fmt.Errorf("the live state DB delivers state hashes after block %d, but the state is at "+
			"block %d", stateHashHeight, height)
	}
	// Reported after registering, since only the registration hands over this hash. No block commits before
	// the storage manager is returned, so nothing the listener delivers can precede it.
	if err := reportStateHash(ctx, stateHash.BlockNumber, &stateHash); err != nil {
		return fmt.Errorf("report the recovered state hash: %w", err)
	}
	return nil
}

// stateHashReporter returns a listener that reports a committed block's state hash to appHashBuilder.
func stateHashReporter(appHashBuilder builder.AppHashBuilder) gigatypes.HashListener {
	return func(ctx context.Context, blockNumber int64, hash *lthash.BlockHash) error {
		//nolint:gosec // committed block numbers are non-negative
		height := uint64(blockNumber)
		stateHash := hash.Global.Checksum()
		componentHashes := hashlog.FlatKVComponentHashes(hash)
		if err := appHashBuilder.ReportStateHash(ctx, height, stateHash, componentHashes); err != nil {
			return fmt.Errorf("report the state hash of block %d: %w", height, err)
		}
		return nil
	}
}
