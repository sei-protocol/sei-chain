package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios"
)

// stallingWorkload holds the build of one height until released, standing in for a slow builder.
type stallingWorkload struct {
	stalledHeight uint64
	release       chan struct{}
	started       atomic.Int64
}

func (w *stallingWorkload) BuildBlock(ctx context.Context, height uint64) (evmonly.BlockRequest, error) {
	w.started.Add(1)
	if height == w.stalledHeight {
		select {
		case <-w.release:
		case <-ctx.Done():
			return evmonly.BlockRequest{}, ctx.Err()
		}
	}
	return evmonly.BlockRequest{Context: evmonly.BlockContext{Number: height}}, nil
}

// A stalled height must not let the other builders run ahead without limit, and once it lands the
// blocks still leave in order.
func TestStreamBlocksBoundsRunAheadBehindAStalledBuilder(t *testing.T) {
	const builders = 4
	cfg := config{builders: builders}
	// The first claimed block is numbered 1 and built at height 2, above genesis.
	workload := &stallingWorkload{stalledHeight: 2, release: make(chan struct{})}
	out := make(chan blockEnvelope, 1024)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	metrics := newLoadMetrics(prometheus.NewRegistry())
	done := make(chan error, 1)
	go func() {
		done <- streamBlocks(ctx, cfg, workload, out, metrics)
	}()

	limit := int64(2 * builders)
	require.Eventually(t, func() bool { return workload.started.Load() == limit }, time.Second, time.Millisecond)
	require.Never(t, func() bool { return workload.started.Load() > limit }, 50*time.Millisecond, 5*time.Millisecond,
		"builders ran past the slot limit while the first height was stalled")
	require.Empty(t, out, "no block may leave before the stalled height")
	require.Eventually(t, func() bool {
		var reported dto.Metric
		require.NoError(t, metrics.reorderPending.Write(&reported))
		return reported.GetGauge().GetValue() == float64(limit-1)
	}, time.Second, time.Millisecond, "every built block but the stalled one should be reported as pending")

	close(workload.release)
	for want := uint64(1); want <= uint64(limit); want++ {
		select {
		case block := <-out:
			require.Equal(t, want, block.number)
		case <-time.After(time.Second):
			t.Fatalf("block %d never left the reorder buffer", want)
		}
	}
	cancel()
	require.NoError(t, <-done)
}

// A prebuilt run with --accounts pays pool members, as a streaming run does, rather than minting a
// fresh recipient per transaction.
func TestPrebuiltPooledRunPaysPoolMembers(t *testing.T) {
	const accounts = 8
	cfg, err := parseConfig([]string{"--metrics-addr=", "--blocks=3", "--accounts=8", "--txs-per-block=2"})
	require.NoError(t, err)
	workload, err := newWorkload(cfg, newGeneratedState())
	require.NoError(t, err)

	require.NoError(t, seedAccountPool(t.Context(), cfg, workload))
	prebuilt, err := prebuildBlockRequests(t.Context(), cfg, workload)
	require.NoError(t, err)

	pool := make(map[common.Address]struct{}, accounts)
	for i := uint64(0); i < accounts; i++ {
		key, err := scenarios.DeterministicPrivateKey(i)
		require.NoError(t, err)
		pool[crypto.PubkeyToAddress(key.PublicKey)] = struct{}{}
	}
	signer := ethtypes.LatestSignerForChainID(cfg.chainID)
	for _, block := range prebuilt {
		for _, raw := range block.request.Txs {
			var tx ethtypes.Transaction
			require.NoError(t, tx.UnmarshalBinary(raw))
			sender, err := ethtypes.Sender(signer, &tx)
			require.NoError(t, err)
			require.Contains(t, pool, sender)
			require.Contains(t, pool, *tx.To(), "a pooled run paid a recipient outside the pool")
		}
	}
}

func TestRunPrebuiltBlocksWithAnAccountPool(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--report-interval=0",
		"--blocks=3",
		"--accounts=8",
		"--txs-per-block=2",
	})
	require.NoError(t, err)
	require.NoError(t, run(cfg))
}
