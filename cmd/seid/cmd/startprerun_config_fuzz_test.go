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
func newStartCmd(t *testing.T, home *configtest.Home, flagValues map[string]string) (*cobra.Command, *server.Context) {
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
	for name, value := range flagValues {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set --%s=%q: %v", name, value, err)
		}
	}

	serverCtx := &server.Context{}
	ctx := context.WithValue(context.Background(), server.ServerContextKey, serverCtx)
	ctx = context.WithValue(ctx, client.ClientContextKey, &client.Context{})
	cmd.SetContext(ctx)

	if err := root.PersistentPreRunE(cmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}
	return cmd, serverCtx
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

		cmd, serverCtx := newStartCmd(t, home, flagValues)
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

	cmd, serverCtx := newStartCmd(t, home, map[string]string{
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

	cmd, _ := newStartCmd(t, home, map[string]string{
		server.FlagPruning: "nothing",
		server.FlagChainID: "a-different-chain",
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("a --chain-id that disagrees with client.toml must panic before the app is built")
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
	_ = cmd.RunE(cmd, nil)
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

	cmd, _ := newStartCmd(t, home, map[string]string{
		server.FlagPruning: "nothing",
		server.FlagChainID: "agreed-chain",
	})
	if home.Exists("genesis.json") {
		t.Fatal("the fixture must carry no genesis.json for this row to reach the discarded error")
	}

	// RunE is called on a goroutine with a bounded wait rather than inline. Reaching the
	// nil-deref is what stops it today, and this test must not depend on that: if the
	// discarded error is ever handled, RunE continues into startInProcess, opens the state
	// databases, binds the listeners and never returns. The timeout turns that into a
	// legible failure instead of a hang to the 10-minute panic.
	outcome := make(chan any, 1)
	go func() {
		defer func() { outcome <- recover() }()
		_ = cmd.RunE(cmd, nil)
		outcome <- nil
	}()

	select {
	case r := <-outcome:
		if r == nil {
			t.Fatal("with no genesis.json the discarded GenesisDocFromFile error must surface as a " +
				"nil-pointer dereference; if the error is returned now, that is a fix and this row " +
				"becomes an assertion about a legible failure instead")
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
	case <-time.After(20 * time.Second):
		t.Fatal("RunE neither returned nor panicked within 20s, which means it got past the " +
			"genesis cross-check and into startInProcess. The discarded GenesisDocFromFile error " +
			"was presumably fixed; this row needs rewriting, and a node is now running in the " +
			"background of this test binary")
	}
}
