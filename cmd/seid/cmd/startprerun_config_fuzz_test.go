package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// start layers two more config behaviors on top of Apply, in its own PreRunE and at
// the head of its RunE. Both are reachable without launching a node; everything after
// them is not.
//
// PreRunE re-binds the command's flags into the viper Apply already populated, then
// resolves pruning purely to fail fast — the returned options are discarded. So a bad
// pruning strategy is refused before the node touches disk, and refused a second time
// later by newApp, where the same helper panics instead of returning.
//
// RunE then re-reads client.toml, compares its chain-id against --chain-id, and
// panics on a mismatch before any app is constructed.
//
// What is NOT reachable here, and is stated rather than silently skipped: cpu-profile,
// trace-store, the second GetConfig, grpc-only forcing GRPC.Enable, and the
// api/grpc-web gating all live inside unexported startInProcess, which opens listeners
// and starts a node. Pinning those needs an integration harness.

// nodeEscapedMarker is a fixed token so CI triage can grep one string for this failure, and
// nodeEscaped carries it from the row that detected it to TestMain.
const nodeEscapedMarker = "CONFIGTEST_NODE_ESCAPED"

var nodeEscaped atomic.Bool

// TestMain fails the binary when a node outlived the row that started it.
//
// The escape is detected inside runEBounded, which on its own can only fail one test. This
// turns it into a non-zero exit for the whole package, so a run that left a node holding
// listeners cannot be read as a pass, while every other test still reports its result. The
// goroutine dump goes with it, since the first question is what the surviving node is doing.
func TestMain(m *testing.M) {
	code := m.Run()
	if nodeEscaped.Load() {
		fmt.Fprintf(os.Stderr, "\n%s: a node outlived the test that started it and did not stop "+
			"when its context was cancelled. Failing this binary deliberately; results above are "+
			"valid up to the point of the escape.\n", nodeEscapedMarker)
		_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// newStartCmd resolves the real start command out of a real root and runs the root's
// PersistentPreRunE against it, so the command arrives in the state seid puts it in:
// Apply has populated the viper, and the client context carries the codec and keyring
// that RunE's client.toml re-read needs.
//
// Building the client context by hand does not work here — RunE reaches
// ReadFromClientConfig, which constructs a keyring from the context's codec, so a bare
// client.Context nil-derefs before the chain-id comparison this file is about.
//
// The returned cancel belongs to the command's own context. runEBounded uses it to ask a
// node to stop on the path where RunE got further than the row expects; other callers
// never reach that path and just let cleanup fire it.
func newStartCmd(t *testing.T, home *configtest.Home, flagValues map[string]string) (*cobra.Command, *server.Context, context.CancelFunc) {
	t.Helper()

	root, _ := NewRootCmd()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	cmd, _, err := root.Find([]string{"start"})
	if err != nil {
		t.Fatalf("find start: %v", err)
	}
	if err := cmd.Flags().Set("home", home.Root); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	setFlags(t, cmd, flagValues)

	serverCtx := &server.Context{}
	base, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ctx := context.WithValue(base, server.ServerContextKey, serverCtx)
	ctx = context.WithValue(ctx, client.ClientContextKey, &client.Context{})
	cmd.SetContext(ctx)

	if err := root.PersistentPreRunE(cmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}
	return cmd, serverCtx, cancel
}

// FuzzStartPreRunPruningFailsFast pins the fail-fast: an unresolvable pruning
// configuration stops start in PreRunE, before anything is opened.
//
// The same helper runs again inside newApp, where it panics rather than returning an
// error — so this check is the difference between a legible refusal and a stack trace.
// It also means pruning is validated twice per boot from the same viper, and the two
// call sites must agree or a node fails in the uglier of the two places.
func FuzzStartPreRunPruningFailsFast(f *testing.F) {
	f.Add("default", uint64(0), uint64(0))
	f.Add("nothing", uint64(0), uint64(0))
	f.Add("everything", uint64(0), uint64(0))
	f.Add("custom", uint64(100), uint64(10))
	f.Add("custom", uint64(0), uint64(0))
	f.Add("bogus", uint64(0), uint64(0))
	f.Add("", uint64(0), uint64(0))

	f.Fuzz(func(t *testing.T, strategy string, keepRecent, interval uint64) {
		configtest.Isolate(t)
		home := configtest.NewHome(t)

		flagValues := map[string]string{}
		if strategy != "" {
			flagValues[server.FlagPruning] = strategy
		}
		if keepRecent != 0 {
			flagValues[server.FlagPruningKeepRecent] = strconv.FormatUint(keepRecent, 10)
		}
		if interval != 0 {
			flagValues[server.FlagPruningInterval] = strconv.FormatUint(interval, 10)
		}

		cmd, serverCtx, _ := newStartCmd(t, home, flagValues)
		preRunErr := cmd.PreRunE(cmd, nil)

		// PreRunE's only job beyond re-binding is this resolution, so its verdict must
		// match the helper's on the very viper it hands over.
		_, want := server.GetPruningOptionsFromFlags(serverCtx.Viper)
		if (preRunErr == nil) != (want == nil) {
			t.Fatalf("pruning=%q keep-recent=%d interval=%d: PreRunE said %v, the helper says %v; "+
				"the fail-fast must agree with the resolution newApp will later panic on",
				strategy, keepRecent, interval, preRunErr, want)
		}
	})
}

// TestStartPreRunRebindsFlagsIntoTheApplyViper pins the re-bind, which is why start
// sees flags Apply did not.
//
// Apply binds the flag set it is given; PreRunE binds it again into the same viper.
// The second bind is what makes start-only flags (grpc-only, profile, the archival
// group) visible to appOpts.Get, and it is a second place where the flag set and the
// viper have to stay in step.
func TestStartPreRunRebindsFlagsIntoTheApplyViper(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	cmd, serverCtx, _ := newStartCmd(t, home, map[string]string{
		server.FlagPruning: "nothing",
		"grpc-only":        "true",
	})
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
	}

	if !serverCtx.Viper.GetBool("grpc-only") {
		t.Fatal("PreRunE must re-bind start's own flags into the viper Apply populated")
	}
	if got := serverCtx.Viper.GetString(server.FlagPruning); got != "nothing" {
		t.Fatalf("pruning resolved to %q after the re-bind, want nothing", got)
	}
}

// TestStartChainIDMismatchPanics pins the comparison at the head of RunE.
//
// start treats client.toml as the authority and --chain-id as an assertion about it.
// Disagreement is a panic, not an error, and the message names ~/.sei/config/client.toml
// whatever --home says — so an operator with a non-default home is pointed at a file
// the node is not reading.
//
// The panic fires before any app is constructed, which is the only reason this is
// reachable at all.
func TestStartChainIDMismatchPanics(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)
	home.WriteClientTOML(t, []byte("chain-id = \"from-client-toml\"\nkeyring-backend = \"test\"\n"))
	home.WriteAppTOML(t, []byte(fixtureAppTOML))

	cmd, _, stop := newStartCmd(t, home, map[string]string{
		server.FlagPruning: "nothing",
		server.FlagChainID: "a-different-chain",
	})

	r, runErr := runEBounded(t, cmd, stop)
	if r == nil {
		t.Fatalf("a --chain-id that disagrees with client.toml must panic before the app is "+
			"built; RunE returned %v instead", runErr)
	}
	msg, ok := r.(string)
	if !ok {
		t.Fatalf("expected a string panic, got %T: %v", r, r)
	}
	if !strings.Contains(msg, "chain-id mismatch") {
		t.Fatalf("the panic must name the mismatch, got %q", msg)
	}
	if !strings.Contains(msg, "from-client-toml") || !strings.Contains(msg, "a-different-chain") {
		t.Fatalf("the panic must quote both values so an operator can see which is which, got %q", msg)
	}
	if !strings.Contains(msg, "~/.sei/config/client.toml") {
		t.Fatalf("the message no longer hardcodes the default home (%q). Deriving it from --home "+
			"is a fix, and this row is where that gets recorded rather than skipped past", msg)
	}
}

// TestStartAfterChainIDAgreementHitsTheGenesisNilDeref pins what happens immediately
// past the chain-id comparison, and is the reason the agreement case cannot be asserted
// the obvious way.
//
// With matching values RunE continues to the genesis cross-check, which discards
// GenesisDocFromFile's error and then dereferences the returned document. On a home with
// no genesis.json that is a nil-pointer dereference, so a missing or corrupt genesis file
// reports itself as a runtime fault with nothing naming the file.
//
// Pinning that keeps this test bounded, which matters more than it looks. Asserting
// "agreement does not panic" would mean letting RunE continue into startInProcess, which
// opens the state databases and binds the RPC, P2P and gRPC listeners: the call would
// block instead of returning, and a node would keep running while later tests clear the
// environment underneath it.
func TestStartAfterChainIDAgreementHitsTheGenesisNilDeref(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)
	home.WriteClientTOML(t, []byte("chain-id = \"agreed-chain\"\nkeyring-backend = \"test\"\n"))
	home.WriteAppTOML(t, []byte(fixtureAppTOML))

	cmd, _, stop := newStartCmd(t, home, map[string]string{
		server.FlagPruning: "nothing",
		server.FlagChainID: "agreed-chain",
	})
	if home.Exists("genesis.json") {
		t.Fatal("the fixture must carry no genesis.json for this row to reach the discarded error")
	}

	r, runErr := runEBounded(t, cmd, stop)
	if r == nil {
		t.Fatalf("with no genesis.json the discarded GenesisDocFromFile error must surface as a "+
			"nil-pointer dereference. RunE returned %v instead: a nil error means the read is now "+
			"handled and this row becomes an assertion about a legible failure, while a non-nil "+
			"error means RunE failed before reaching the genesis cross-check at all", runErr)
	}
	if msg, ok := r.(string); ok {
		if strings.Contains(msg, "chain-id mismatch") {
			t.Fatalf("matching chain-ids must not trip the mismatch panic, got %q", msg)
		}
		t.Fatalf("expected a nil-pointer dereference past the chain-id comparison, got %q", msg)
	}
	if err, ok := r.(error); ok &&
		!strings.Contains(err.Error(), "nil pointer") &&
		!strings.Contains(err.Error(), "invalid memory") {
		t.Fatalf("expected a nil-pointer dereference past the chain-id comparison, got %v", err)
	}
}

// runEBounded runs a command's RunE on a goroutine and returns what it panicked with, or
// nil if it returned cleanly.
//
// Both RunE rows need this. Each is stopped today by a panic firing before control
// reaches startInProcess, and neither may depend on that: if either panic stops firing,
// RunE continues on to open the state databases and bind the RPC, P2P and gRPC
// listeners, so the call never returns. The bound converts that into a failure naming
// what happened.
//
// The result is delivered only by the deferred send. recover() is nil on a clean return,
// which is exactly the value callers check for, so an explicit second send would fill the
// one-slot buffer and leave the deferred send blocked for the life of the test binary.
//
// The returned error is reported alongside the panic rather than discarded. Without it a
// caller cannot tell "RunE reached the row under test and returned" from "RunE failed
// somewhere earlier", and those two want very different failure messages.
//
// On the timeout path the node is asked to stop before the failure is reported. t.Fatal
// runs runtime.Goexit on this goroutine only, so without the cancel a live node would keep
// its state databases open and its listeners bound for the rest of the test binary, reading
// the environment while configtest.Isolate's cleanup clears and restores it. Cancelling is
// a request rather than a guarantee, which is why the second wait is bounded too and the
// message distinguishes a node that stopped from one that ignored the cancel.
//
// The bounds are deliberately generous, because the terminal branch inverted the cost of
// being wrong. A bound that is too short no longer just files an early report: it aborts the
// whole package on a loaded -race shard, destroying results for every other test on nothing
// more than wall-clock evidence. A bound that is too long costs only a delayed report on a
// path where something is already broken, since the happy path returns as soon as the panic
// fires, in well under a second, and never waits at all. So the timeout is sized to outlast
// any plausible shard rather than to fail fast, and the terminal branch is reached only after
// a cancel, a grace period, and a final non-blocking check that RunE did not just finish.
// runEBound is how long RunE gets before it is treated as having escaped its guard, and
// cancelGrace is how long the cancel gets to take effect after that. Both are generous by
// design; see the note above runEBounded.
const (
	runEBound   = 30 * time.Second
	cancelGrace = 10 * time.Second
)

// runEWaits returns the two bounds, shortened to fit the test binary's remaining deadline
// when it has one.
//
// Waiting past the deadline would hand the run to go test's own timeout, whose message says
// only that the binary ran too long. That is strictly worse than this row's diagnosis, and
// it also means the terminal branch never executes to report whether the node stopped. So
// when the remaining budget is smaller than the generous bounds, they scale down to fit and
// keep a quarter of it back for the failure to be written.
func runEWaits(t *testing.T) (bound, grace time.Duration) {
	t.Helper()
	bound, grace = runEBound, cancelGrace

	deadline, ok := t.Deadline()
	if !ok {
		return bound, grace
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		// Already past the deadline. Return the smallest useful waits so the diagnostic still
		// runs, rather than a floor that would guarantee the timeout wins.
		return time.Millisecond, time.Millisecond
	}
	// A quarter of what is left is held back for the failure to be written.
	budget := remaining * 3 / 4
	if budget >= bound+grace {
		return bound, grace
	}
	// Split the budget, never exceeding it. There is deliberately no floor: raising the waits
	// above the time actually left would let go test's timeout kill the binary before the
	// terminal branch reports anything, which is the failure this whole function exists to
	// avoid. A deadline too short to wait out is a deadline too short, and saying so quickly
	// beats saying nothing.
	return budget * 3 / 4, budget / 4
}

func runEBounded(t *testing.T, cmd *cobra.Command, stop context.CancelFunc) (recovered any, err error) {
	t.Helper()

	type result struct {
		recovered any
		err       error
	}
	bound, grace := runEWaits(t)
	outcome := make(chan result, 1)
	go func() {
		var res result
		defer func() {
			res.recovered = recover()
			outcome <- res
		}()
		res.err = cmd.RunE(cmd, nil)
	}()

	select {
	case r := <-outcome:
		return r.recovered, r.err
	case <-time.After(bound):
		stop()
		diagnosis := fmt.Sprintf("RunE neither returned nor panicked within %s, so it got past the "+
			"guard that normally stops it and into startInProcess. That guard was presumably fixed "+
			"and this row needs rewriting", bound)
		select {
		case <-outcome:
			// The node stopped, so the binary is clean and an ordinary failure is right.
			t.Fatalf("%s. It stopped when the command context was cancelled", diagnosis)
			return nil, nil
		case <-time.After(grace):
		}
		// A last non-blocking receive before the terminal branch. RunE may have completed in the
		// window between the grace period expiring and this line, and in that case the goroutine
		// is gone and there is no node to strand, so the ordinary failure is still correct.
		select {
		case <-outcome:
			t.Fatalf("%s. It finished while the cancel was being waited on", diagnosis)
			return nil, nil
		default:
		}
		// Nothing came back, so a node is still holding its listeners and state databases, and
		// t.Fatal only unwinds this goroutine. Every later test in the binary then runs alongside
		// a live node, including through configtest.Isolate's cleanup, which clears the
		// environment and restores it while that node reads it. The binary has to fail as a
		// whole, not just this row.
		//
		// It fails at exit rather than here. Panicking would take the package down at this
		// instant and destroy every other test's result, and the trigger is a wall-clock bound
		// rather than proof that a node is running: a wedged RunE that never started one, or a
		// shard loaded enough to burn the budget, reach this line too. Recording the escape and
		// letting TestMain force the exit keeps the failure deliberate while still reporting
		// everything else the run learned.
		nodeEscaped.Store(true)
		t.Fatalf("%s: %s, and it did not stop within %s of the command context being cancelled. "+
			"The binary will be failed at exit: a live node with bound listeners must not outlive "+
			"this test and be inherited by the ones after it", nodeEscapedMarker, diagnosis, grace)
		return nil, nil
	}
}

// fixtureAppTOML is the app.toml both RunE rows write before booting.
//
// The listener sections are disabled and pinned to port 0 as defense in depth. Both rows are
// stopped by a panic before startInProcess opens anything, and TestMain fails the binary if a
// node ever outlives its row, but neither guarantee is worth resting a bound port on: if a row
// did get through, a default address would collide with a real node or another shard on the
// same host rather than failing locally.
//
// telemetry is disabled deliberately. start's RunE registers prometheus collectors on a
// process-global registry, so a second invocation in the same binary fails with a
// duplicate-registration error before reaching either row's subject, and the rows would
// only work once per process. global-labels is present because GetConfig reads it with a
// bare type assertion and fails outright when it is absent.
const fixtureAppTOML = `[telemetry]
enabled = false
global-labels = []

[api]
enable = false
address = "tcp://127.0.0.1:0"

[grpc]
enable = false
address = "127.0.0.1:0"

[grpc-web]
enable = false
address = "127.0.0.1:0"
`
