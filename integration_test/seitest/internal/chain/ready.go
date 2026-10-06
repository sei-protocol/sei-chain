package chain

import (
	"context"
	"fmt"
	"time"

	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// readyPollInterval is the readiness poll cadence. Each poll is an in-process
// function call rather than an HTTP round trip, so it can be far tighter than
// the half-second an out-of-process probe settles for.
const readyPollInterval = 20 * time.Millisecond

// WaitReady blocks until every validator is producing blocks, or ctx fires. It
// is deliberately separate from Start, which only builds and starts the
// validators: a caller that wants to observe a partial bring-up, or bound
// readiness on a different deadline than construction, can.
func (c *Chain) WaitReady(ctx context.Context) error {
	for _, v := range c.validators {
		height, err := waitHeightAdvances(ctx, v, 1)
		if err != nil {
			return err
		}
		if err := assertLiveTimeouts(ctx, v, height, c.consensusParams.Timeout); err != nil {
			return err
		}
	}
	return nil
}

// waitHeightAdvances blocks until v's committed height rises by at least delta
// from the first height it reads, and returns the height it settled at — proof
// the validator is producing blocks, not merely answering. A validator stalled
// at a frozen height still reports that height, so a single successful read
// proves nothing.
func waitHeightAdvances(ctx context.Context, v *Validator, delta int64) (int64, error) {
	tick := time.NewTicker(readyPollInterval)
	defer tick.Stop()
	var start, last int64 = -1, -1
	for {
		if h, ok := latestHeight(ctx, v); ok {
			if start < 0 {
				start = h
			}
			last = h
			if h >= start+delta {
				return h, nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("%s height did not advance +%d (start=%d last=%d): %w", v.moniker, delta, start, last, ctx.Err())
		case <-tick.C:
		}
	}
}

// latestHeight reads v's committed height through its in-process client. ok is
// false before the first block, when the client has nothing to report.
func latestHeight(ctx context.Context, v *Validator) (int64, bool) {
	if v.rpc == nil {
		return 0, false
	}
	res, err := v.rpc.Block(ctx, nil)
	if err != nil || res == nil || res.Block == nil {
		return 0, false
	}
	return res.Block.Height, true
}

// assertLiveTimeouts checks the consensus timeouts the chain is actually running
// against what Config asked for. assertGenesisConsensusParams checks the
// genesis file; this checks the live chain, and so is the standing guard against
// a genesis-assembly path that silently resets consensus params.
//
// height must be one the validator has committed. A nil height asks for the
// latest, which resolves to the height being built rather than the last one
// finished — and params for a height that has not been stored yet are simply
// absent.
func assertLiveTimeouts(ctx context.Context, v *Validator, height int64, want tmtypes.TimeoutParams) error {
	res, err := v.rpc.ConsensusParams(ctx, &height)
	if err != nil {
		return fmt.Errorf("read live consensus params from %s at height %d: %w", v.moniker, height, err)
	}
	if got := res.ConsensusParams.Timeout; got != want {
		return fmt.Errorf("%s runs consensus timeouts %+v at height %d, but Config asked for %+v", v.moniker, got, height, want)
	}
	return nil
}
