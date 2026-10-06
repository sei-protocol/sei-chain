package chain

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// bringUpTimeout bounds a single-validator bring-up. The budget is seconds; this
// is generous enough that a breach means something is wrong rather than slow.
const bringUpTimeout = 60 * time.Second

// TestStartSingleValidator is the stage-1 done-when: Start, WaitReady, Close for
// N=1 with no listener bound and no store left open.
func TestStartSingleValidator(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), bringUpTimeout)
	defer cancel()

	started := time.Now()
	c, err := Start(ctx, Config{Validators: 1})
	require.NoError(t, err, "Start")
	defer func() { require.NoError(t, c.Close()) }()
	t.Logf("Start(N=1) took %s", time.Since(started))

	require.Equal(t, 1, c.Validators())
	v := c.Validator(0)

	// no-listener bring-up: nothing is bound, and the in-process client works
	// anyway. This is the falsification of the RPC.ListenAddress = "" risk.
	require.Empty(t, v.tmCfg.RPC.ListenAddress, "a CometBFT RPC listener was bound")
	require.NotNil(t, v.LocalClient(), "rpclocal.New did not produce a client with no HTTP bound")

	require.NoError(t, c.WaitReady(ctx), "WaitReady")
	t.Logf("WaitReady(N=1) at %s", time.Since(started))

	// The chain is producing blocks and answering through the local client.
	res, err := v.LocalClient().Block(ctx, nil)
	require.NoError(t, err)
	require.Greater(t, res.Block.Height, int64(1), "chain did not commit past genesis")

	// The validator is in the live valset, so consensus is really running on it.
	vals, err := v.LocalClient().Validators(ctx, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, vals.Validators, 1)

	require.NotEmpty(t, v.Moniker())
	require.NotEmpty(t, v.OperatorAddr())
	require.NotNil(t, v.Keyring())
	require.NotNil(t, v.App())
	require.NotNil(t, v.ClientContext().Client)
}

// TestCloseIsIdempotent falsifies the app.Close half of the teardown risk: the
// second Close must not deadlock on the commit lock, double-close a store, or
// panic. BaseApp.Close is not itself idempotent, so this is load-bearing.
func TestCloseIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), bringUpTimeout)
	defer cancel()

	c, err := Start(ctx, Config{Validators: 1})
	require.NoError(t, err)
	require.NoError(t, c.WaitReady(ctx))

	home := c.HomeDir()
	require.NoError(t, closeWithin(t, c, 30*time.Second), "first Close")
	require.NoError(t, closeWithin(t, c, 5*time.Second), "second Close")

	_, statErr := os.Stat(home)
	require.True(t, errors.Is(statErr, os.ErrNotExist), "engine-owned home dir survived Close: %v", statErr)
}

// TestStartCancelledMidBringUpClosesCleanly falsifies the partial-start half of
// the teardown risk: a bring-up abandoned part-way must tear down what it built
// without panicking or hanging, and BaseApp.Close is the part that could — it
// takes the commit lock, and it is reached here for an app whose node never ran.
//
// The deadlines sweep rather than pinning one value. A single guess stops
// testing anything the moment bring-up gets faster or slower than it: every
// deadline must return cleanly, and the run is only meaningful if at least one
// of them actually landed mid-bring-up, which the test asserts.
func TestStartCancelledMidBringUpClosesCleanly(t *testing.T) {
	deadlines := []time.Duration{
		time.Nanosecond, // before anything is provisioned
		10 * time.Millisecond,
		50 * time.Millisecond,
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond, // around a healthy N=1 bring-up
	}

	var cancelled int
	for _, deadline := range deadlines {
		t.Run(deadline.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()

			c, err := startWithin(t, ctx, Config{Validators: 1}, bringUpTimeout)
			if err != nil {
				// Start tore down its own partial bring-up before returning; a nil
				// handle is the contract, and nothing is left for the caller to close.
				require.Nil(t, c, "Start returned both an error and a handle")
				cancelled++
				t.Logf("cancelled mid-bring-up as intended: %v", err)
				return
			}
			t.Log("bring-up completed inside the deadline")
			require.NoError(t, closeWithin(t, c, 30*time.Second))
		})
	}
	require.NotZero(t, cancelled, "no deadline landed mid-bring-up, so the partial-start teardown path went untested")
}

// startWithin runs Start on its own goroutine and fails the test if it does not
// return in time, so a bring-up that hangs on its own teardown reports as a
// failure here rather than as the whole package timing out.
func startWithin(t *testing.T, ctx context.Context, cfg Config, timeout time.Duration) (*Chain, error) {
	t.Helper()
	type result struct {
		chain *Chain
		err   error
	}
	done := make(chan result, 1)
	go func() {
		c, err := Start(ctx, cfg)
		done <- result{c, err}
	}()
	select {
	case r := <-done:
		return r.chain, r.err
	case <-time.After(timeout):
		t.Fatalf("Start did not return within %s of its context being cancelled", timeout)
		return nil, nil
	}
}

// closeWithin runs Close on its own goroutine and fails the test if it does not
// return in time, so a deadlock reports as a failure rather than as the whole
// package timing out with no attribution.
func closeWithin(t *testing.T, c *Chain, timeout time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.Close() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		t.Fatalf("Close did not return within %s", timeout)
		return nil
	}
}

// TestValidateTopology pins the rejected validator counts. N=2 is the one that
// would otherwise hang instead of failing.
func TestValidateTopology(t *testing.T) {
	require.Error(t, validateTopology(0))
	require.ErrorContains(t, validateTopology(2), "block-sync")
	require.NoError(t, validateTopology(1))
	require.NoError(t, validateTopology(3))
	require.NoError(t, validateTopology(4))
}

// TestNewValidatorConfigInvariants checks that the sole config constructor
// satisfies the guard, and that the guard rejects each invariant being undone.
// Pure config check — no bring-up.
func TestNewValidatorConfigInvariants(t *testing.T) {
	build := func(t *testing.T, serveRPC bool) *tmcfgWithPort {
		t.Helper()
		cfg, port, err := newValidatorConfig(t.TempDir(), "node0", serveRPC)
		require.NoError(t, err)
		return &tmcfgWithPort{cfg: cfg, port: port}
	}

	t.Run("default binds no RPC listener", func(t *testing.T) {
		got := build(t, false)
		require.Empty(t, got.cfg.RPC.ListenAddress)
		require.NotEmpty(t, got.port)
		require.NoError(t, assertConfigInvariants(got.cfg, "node0", 1))
	})

	t.Run("ServeTendermintRPC binds loopback", func(t *testing.T) {
		got := build(t, true)
		require.Contains(t, got.cfg.RPC.ListenAddress, "127.0.0.1:")
		require.NoError(t, assertConfigInvariants(got.cfg, "node0", 1))
	})

	t.Run("guard rejects metrics on", func(t *testing.T) {
		got := build(t, false)
		got.cfg.Instrumentation.Prometheus = true
		require.ErrorContains(t, assertConfigInvariants(got.cfg, "node0", 1), "Prometheus")
	})

	t.Run("guard rejects a non-loopback bind", func(t *testing.T) {
		got := build(t, false)
		got.cfg.P2P.ListenAddress = "tcp://0.0.0.0:26656"
		require.ErrorContains(t, assertConfigInvariants(got.cfg, "node0", 1), "loopback")
	})

	t.Run("guard rejects a missing peer mesh for N>=2", func(t *testing.T) {
		got := build(t, false)
		require.ErrorContains(t, assertConfigInvariants(got.cfg, "node0", 3), "PersistentPeers")
		got.cfg.P2P.PersistentPeers = "abc@127.0.0.1:1"
		require.NoError(t, assertConfigInvariants(got.cfg, "node0", 3))
	})

	t.Run("guard rejects a conn-tracker ceiling below the fleet size", func(t *testing.T) {
		got := build(t, false)
		got.cfg.P2P.PersistentPeers = "abc@127.0.0.1:1"
		got.cfg.P2P.MaxIncomingConnectionAttempts = 10
		require.ErrorContains(t, assertConfigInvariants(got.cfg, "node0", 3), "MaxIncomingConnectionAttempts")
	})
}

// tmcfgWithPort pairs a built config with the P2P port its constructor
// allocated, so the subtests can read both.
type tmcfgWithPort struct {
	cfg  *config.Config
	port string
}

// TestAppOptionsServeNothing pins the EVM-serving-off invariant against the
// resolved config, which is what assertEVMServingOff reads. Enabling a listener
// must fail the guard rather than quietly limit the process to one network.
func TestAppOptionsServeNothing(t *testing.T) {
	require.NoError(t, assertEVMServingOff(appOptions{chainID: "seitest-1"}))
	require.ErrorContains(t, assertEVMServingOff(evmOnOpts{}), "EVM")
	require.ErrorContains(t, assertEVMServingOff(adminOnOpts{}), "admin")
}

// evmOnOpts is appOptions with the EVM HTTP listener turned back on.
type evmOnOpts struct{}

func (evmOnOpts) Get(key string) interface{} {
	if key == "evm.http_enabled" {
		return true
	}
	return appOptions{}.Get(key)
}

// adminOnOpts is appOptions with the admin gRPC server turned back on.
type adminOnOpts struct{}

func (adminOnOpts) Get(key string) interface{} {
	if key == adminEnabledKey {
		return true
	}
	return appOptions{}.Get(key)
}

// TestConfigDefaults pins the zero-value resolution Config documents.
func TestConfigDefaults(t *testing.T) {
	got := Config{}.withDefaults()
	require.Equal(t, 1, got.Validators)
	require.NotEmpty(t, got.ChainID)
	require.NotNil(t, got.LogWriter)
	require.NotEqual(t, got.ChainID, Config{}.withDefaults().ChainID, "chain id is not fresh per Start")
	require.Equal(t, "pinned", Config{ChainID: "pinned"}.withDefaults().ChainID)
}

// TestConsensusParamsForUsesFastDefaults pins that an unset Config.Timeouts
// inherits sei-tendermint's defaults rather than a slower engine-local value,
// which is where the block cadence comes from.
func TestConsensusParamsForUsesFastDefaults(t *testing.T) {
	require.Equal(t, tmtypes.DefaultTimeoutParams(), consensusParamsFor(nil).Timeout)

	want := tmtypes.DefaultTimeoutParams()
	want.Commit = 250 * time.Millisecond
	require.Equal(t, want, consensusParamsFor(&want).Timeout)
}
