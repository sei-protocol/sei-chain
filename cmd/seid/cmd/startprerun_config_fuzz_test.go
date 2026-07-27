package cmd

import (
	"context"
	"io"
	"strconv"
	"strings"
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
	func() {
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
	}()
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
// Both bounds are 5s. Each row is stopped by a panic that fires in well under a second, so
// the bound only has to outlast a loaded -race shard, not a node doing real work. Waiting
// longer just delays the report of a guard that has already stopped guarding, and holds the
// listeners open while it waits.
func runEBounded(t *testing.T, cmd *cobra.Command, stop context.CancelFunc) (recovered any, err error) {
	t.Helper()

	type result struct {
		recovered any
		err       error
	}
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
	case <-time.After(5 * time.Second):
		stop()
		const diagnosis = "RunE neither returned nor panicked within 5s, so it got past the guard " +
			"that normally stops it and into startInProcess. That guard was presumably fixed and " +
			"this row needs rewriting"
		select {
		case <-outcome:
			// The node stopped, so the binary is clean and an ordinary failure is right.
			t.Fatalf("%s. It stopped when the command context was cancelled", diagnosis)
			return nil, nil
		case <-time.After(5 * time.Second):
		}
		// The node ignored the cancel, so it still holds its listeners and state databases and
		// t.Fatal would only unwind this goroutine. Every later test in the binary would then run
		// alongside a live node, including through configtest.Isolate's cleanup, which clears the
		// environment and restores it while that node reads it. Panicking is the terminal action:
		// it fails the binary here rather than leaving a corrupted one to produce results nobody
		// should trust, and the goroutine dump shows what the node is doing.
		panic(diagnosis + ", and it did not stop when the command context was cancelled. Failing " +
			"the whole binary deliberately: a live node with bound listeners must not outlive this " +
			"test and be inherited by the ones after it")
	}
}

// fixtureAppTOML is the app.toml both RunE rows write before booting.
//
// telemetry is disabled deliberately. start's RunE registers prometheus collectors on a
// process-global registry, so a second invocation in the same binary fails with a
// duplicate-registration error before reaching either row's subject, and the rows would
// only work once per process. global-labels is present because GetConfig reads it with a
// bare type assertion and fails outright when it is absent.
const fixtureAppTOML = `[telemetry]
enabled = false
global-labels = []
`
