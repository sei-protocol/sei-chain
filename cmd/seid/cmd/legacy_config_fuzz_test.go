package cmd

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

// This file pins the legacy boot seam itself: LegacyConfigManager.Apply, which
// forwards verbatim to server.InterceptConfigsPreRunHandler.
//
// Apply is the whole legacy configuration path in one call. It resolves --home,
// creates or reads config/config.toml, unmarshals it into a tmcfg.Config, creates
// or merges config/app.toml into the same viper, binds every cobra flag, and
// leaves two channels behind for the rest of the boot:
//
//	serverCtx.Config — the Tendermint config struct, populated by viper.Unmarshal
//	serverCtx.Viper  — the flat key/value map every appOpts.Get() call site reads
//
// Those two channels are the entire interface between configuration and a running
// node, which is what makes them the right thing to pin. A replacement manager is
// correct exactly insofar as it leaves the same two channels in the same state,
// and every target here states one property of that state precisely enough for a
// second implementation to be measured against it.
//
// Everything runs in a pinned environment (configtest.Isolate) against a fixture
// home. That is not tidiness: the path reads bare environment variables and $HOME,
// so an un-pinned environment makes the assertions mean different things on
// different machines.

// applyResult is what one boot through the legacy manager leaves behind.
type applyResult struct {
	ctx *server.Context
	err error
}

// applyLegacy boots one fixture home through LegacyConfigManager.Apply with the
// given explicit flags, and returns the resulting channels.
//
// The command is built fresh for every call, from the real server.StartCmd flag
// set and the real initAppConfig template, so the flag universe and the app.toml
// template are the node's own rather than a test's approximation. Setting a flag
// through cmd.Flags().Set marks it Changed, which is exactly how cobra represents
// "the operator passed this on the command line" — so the flag layer here is the
// real flag layer, not a viper override standing in for one.
//
// StartCmd's own PreRunE is deliberately not run. It layers more behavior on top
// (re-binding flags, fail-fast pruning validation, pinning chain-id from
// client.toml at override precedence) which belongs to separate manifest rows;
// this harness isolates Apply.
func applyLegacy(t *testing.T, home *configtest.Home, flagValues map[string]string) applyResult {
	t.Helper()
	cmd, serverCtx := newApplyCommand(t, home)
	setFlags(t, cmd, flagValues)
	return applyResult{ctx: serverCtx, err: applyThrough(cmd)}
}

// newApplyCommand builds the command and server context Apply runs against, with
// --home already pointed at the fixture. It is separate from applyLegacy so a test
// that needs to inspect the flag set *after* Apply — the write-back in bindFlags
// mutates it — can hold onto the command.
func newApplyCommand(t *testing.T, home *configtest.Home) (*cobra.Command, *server.Context) {
	t.Helper()

	cmd := server.StartCmd(nil, home.Root, []trace.TracerProviderOption{})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set(flags.FlagHome, home.Root); err != nil {
		t.Fatalf("set --home: %v", err)
	}

	serverCtx := &server.Context{}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverCtx))
	return cmd, serverCtx
}

// setFlags applies flag values in sorted key order.
//
// Ranging a map directly would apply them in a different order per run. Every caller here
// sets flags that do not interact, so nothing depends on the order today, but a fuzz
// corpus is only useful if a failing entry reproduces: the first row whose flags interact,
// through cobra validation or one flag's Set reading another, would otherwise fail
// intermittently against the seed that found it. Sorting costs nothing and removes the
// class.
func setFlags(t *testing.T, cmd *cobra.Command, flagValues map[string]string) {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(flagValues)) {
		if err := cmd.Flags().Set(name, flagValues[name]); err != nil {
			t.Fatalf("set --%s=%q: %v", name, flagValues[name], err)
		}
	}
}

// applyThrough runs the legacy manager against a command from newApplyCommand, using
// the node's real template and config struct.
//
// It takes no server.Context: Apply reaches the one it populates through the command's own
// context, set in newApplyCommand, so a parameter here would only imply the context is
// threaded through this call.
func applyThrough(cmd *cobra.Command) error {
	template, appConfig := initAppConfig()
	return configmanager.LegacyConfigManager{}.Apply(cmd, template, appConfig)
}

// setServerEnv sets the environment variable the server viper reads for a config
// key, deriving the name the way viper does from the running binary's basename.
func setServerEnv(t *testing.T, key, value string) {
	t.Helper()
	prefix, err := configtest.ServerEnvPrefix()
	if err != nil {
		t.Fatalf("resolve env prefix: %v", err)
	}
	name := configtest.ServerEnvKey(prefix, key)
	if err := os.Setenv(name, value); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(name) })
}

// tmKey is a Tendermint config key reachable from all four layers: it has a
// cobra flag, it lives in config.toml, it has an env spelling, and it has an
// in-code default.
type tmKey struct {
	// Key is the dotted config.toml key, which for these rows is also the flag
	// name and the basis of the env var.
	Key string
	// Path is the Dump path of the tmcfg.Config field it resolves into.
	Path string
	// Values are three distinct, individually-valid values for the key, used one
	// per layer so the winning layer is identifiable from the result alone.
	Values [3]string
}

// tmKeys are the Tendermint rows the precedence target drives. Each carries three
// distinct legal values so that "which layer won" is readable directly off the
// resolved config.
var tmKeys = []tmKey{
	{
		Key: "rpc.laddr", Path: "RPC.ListenAddress",
		Values: [3]string{"tcp://127.0.0.1:26610", "tcp://127.0.0.1:26620", "tcp://127.0.0.1:26630"},
	},
	{
		Key: "rpc.pprof-laddr", Path: "RPC.PprofListenAddress",
		Values: [3]string{"localhost:6010", "localhost:6020", "localhost:6030"},
	},
	{
		Key: "p2p.laddr", Path: "P2P.ListenAddress",
		Values: [3]string{"tcp://0.0.0.0:26610", "tcp://0.0.0.0:26620", "tcp://0.0.0.0:26630"},
	},
	{
		Key: "p2p.persistent-peers", Path: "P2P.PersistentPeers",
		Values: [3]string{"a@1.1.1.1:26656", "b@2.2.2.2:26656", "c@3.3.3.3:26656"},
	},
	{
		Key: "moniker", Path: "Moniker",
		Values: [3]string{"from-file", "from-env", "from-flag"},
	},
}

// FuzzHashVaultDisabledUnsafeResolution pins the root-scope kill switch for the
// app-hash equivocation guard.
//
// Two things make it worth its own target. It is a bool whose safe value is the
// default, so an absent key must resolve false — setting it true removes
// equivocation protection with only a log banner. And it lives at TOML root scope,
// before any [section] header: nested under a section it parses as a different key
// and is silently ignored, which reads as "I disabled the guard" while the guard
// stays on, and would read the other way round if the scope were ever mishandled.
// The document is built from the fuzzer's choices rather than taken as free text,
// so the expected outcome follows from construction instead of being a second
// input the fuzzer can mutate out of agreement with the first.
func FuzzHashVaultDisabledUnsafeResolution(f *testing.F) {
	f.Add(false, false, false)
	f.Add(true, true, false)  // root scope, true: the guard is off
	f.Add(true, false, false) // root scope, false
	f.Add(true, true, true)   // nested under a section: silently ignored
	f.Add(true, false, true)

	f.Fuzz(func(t *testing.T, present, value, underSection bool) {
		configtest.Isolate(t)
		home := configtest.NewHome(t)

		var doc strings.Builder
		if present {
			if underSection {
				doc.WriteString("[p2p]\n")
			}
			fmt.Fprintf(&doc, "hash-vault-disabled-unsafe = %t\n", value)
		}
		if doc.Len() > 0 {
			home.WriteConfigTOML(t, []byte(doc.String()))
		}

		// Root scope is the only placement that resolves. Nested under a section the
		// key becomes p2p.hash-vault-disabled-unsafe, which nothing reads.
		wantDisabled := present && value && !underSection

		got := applyLegacy(t, home, nil)
		if got.err != nil {
			t.Fatalf("Apply must succeed on a well-formed config.toml, got %v", got.err)
		}
		if got.ctx.Config.HashVaultDisabledUnsafe != wantDisabled {
			t.Fatalf("hash-vault-disabled-unsafe resolved to %v, want %v, from:\n%s",
				got.ctx.Config.HashVaultDisabledUnsafe, wantDisabled, doc.String())
		}
	})
}

// TestHashVaultDisabledUnsafeDefaultsToEnabledGuard states the default on its own,
// so the guard's safe value is pinned even if every seed above were removed.
func TestHashVaultDisabledUnsafeDefaultsToEnabledGuard(t *testing.T) {
	configtest.Isolate(t)
	got := applyLegacy(t, configtest.NewHome(t), nil)
	if got.err != nil {
		t.Fatalf("Apply: %v", got.err)
	}
	if got.ctx.Config.HashVaultDisabledUnsafe {
		t.Fatal("an empty home must leave the app-hash equivocation guard enabled")
	}
}

// FuzzApplyPrecedenceTendermint pins the resolution order for Tendermint config:
// flag beats environment beats config.toml beats the in-code default.
//
// The three layers carry three different legal values, so the assertion reads the
// winner straight off serverCtx.Config rather than inferring it. The fuzzer's job
// is to enumerate the presence combinations across every row, including the ones
// nobody writes a hand test for — env set but file absent, flag set with neither,
// all three set at once.
//
// This is the ordering the whole four-layer model in the ConfigManager design
// rests on, and it is currently an emergent property of viper's precedence
// interacting with bindFlags' write-back rather than anything stated in one place.
// Pinning it is what makes it a contract.
func FuzzApplyPrecedenceTendermint(f *testing.F) {
	f.Add(uint(0), false, false, false)
	f.Add(uint(0), true, false, false)
	f.Add(uint(0), false, true, false)
	f.Add(uint(0), false, false, true)
	f.Add(uint(0), true, true, false)
	f.Add(uint(0), true, false, true)
	f.Add(uint(0), false, true, true)
	f.Add(uint(0), true, true, true)
	f.Add(uint(4), true, true, true)
	f.Add(uint(3), false, true, true)

	f.Fuzz(func(t *testing.T, keyIdx uint, inFile, inEnv, inFlag bool) {
		configtest.Isolate(t)
		row := tmKeys[keyIdx%uint(len(tmKeys))]
		home := configtest.NewHome(t)

		// A dotted TOML key is a table path, so one line per key is enough to
		// place a value in any section without rendering the section header.
		if inFile {
			home.WriteConfigTOML(t, []byte(fmt.Sprintf("%s = %q\n", row.Key, row.Values[0])))
		}
		if inEnv {
			setServerEnv(t, row.Key, row.Values[1])
		}
		flagValues := map[string]string{}
		if inFlag {
			flagValues[row.Key] = row.Values[2]
		}

		got := applyLegacy(t, home, flagValues)
		if got.err != nil {
			t.Fatalf("%s: Apply must succeed with legal values in every layer, got %v", row.Key, got.err)
		}

		want := ""
		switch {
		case inFlag:
			want = row.Values[2]
		case inEnv:
			want = row.Values[1]
		case inFile:
			want = row.Values[0]
		}

		leaf, ok := configtest.LeafAt(configtest.Dump(*got.ctx.Config), row.Path)
		if !ok {
			t.Fatalf("%s claims to resolve into %q, which is not in the resolved Tendermint config", row.Key, row.Path)
		}
		if want == "" {
			// No layer supplied a value, so the in-code default stands. The default
			// itself is not asserted — moniker's is the hostname, which is
			// machine-dependent — only that no absent layer leaked a test value in.
			for _, v := range row.Values {
				if leaf == configtest.DumpAt(row.Path, v) {
					t.Fatalf("%s resolved to %s with no layer setting it", row.Key, leaf)
				}
			}
			return
		}
		if wantLeaf := configtest.DumpAt(row.Path, want); leaf != wantLeaf {
			t.Fatalf("%s did not resolve to the highest present layer\n got: %s\nwant: %s\n"+
				"layers: file=%v env=%v flag=%v", row.Key, leaf, wantLeaf, inFile, inEnv, inFlag)
		}
	})
}

// appKey is an app.toml key that also has a cobra flag, resolved through
// serverCtx.Viper rather than through the Tendermint struct.
type appKey struct {
	Key    string
	Values [3]string
	// Numeric marks a key whose app.toml spelling is an unquoted TOML integer, so
	// the file layer carries a typed scalar rather than a quoted string.
	Numeric bool
	// WantGoType is the Go type the value has once it reaches appOpts.Get,
	// whichever layer supplied it. See FuzzApplyPrecedenceApp for why it is a
	// property of the flag's declared type and not of the winning layer.
	WantGoType string
}

var appKeys = []appKey{
	{Key: "pruning", Values: [3]string{"nothing", "everything", "default"}, WantGoType: "string"},
	{Key: "minimum-gas-prices", Values: [3]string{"0.01usei", "0.02usei", "0.03usei"}, WantGoType: "string"},
	{Key: "halt-height", Values: [3]string{"100", "200", "300"}, Numeric: true, WantGoType: "string"},
	{Key: "min-retain-blocks", Values: [3]string{"1000", "2000", "3000"}, Numeric: true, WantGoType: "string"},
	{Key: "grpc.address", Values: [3]string{"127.0.0.1:9010", "127.0.0.1:9020", "127.0.0.1:9030"}, WantGoType: "string"},
	{Key: "state-sync.snapshot-interval", Values: [3]string{"100", "200", "300"}, Numeric: true, WantGoType: "string"},
	// concurrency-workers is registered as an Int flag, which is one of the few
	// types viper converts rather than passing through as text.
	{Key: "concurrency-workers", Values: [3]string{"4", "8", "16"}, Numeric: true, WantGoType: "int"},
}

// FuzzApplyPrecedenceApp pins the same ordering on the other channel. App
// configuration never becomes a struct during Apply — it stays a flat viper map
// that app.New reads key by key through appOpts.Get — so the assertion is on
// serverCtx.Viper.Get and the comparison is on the rendered value, which keeps the
// resolved *type* in frame. That matters here more than for the Tendermint struct:
// a value that arrives from a flag or an environment variable is a string, while
// the same value from app.toml is a typed TOML scalar, and every downstream
// cast.To* sees the difference.
func FuzzApplyPrecedenceApp(f *testing.F) {
	f.Add(uint(0), false, false, false)
	f.Add(uint(0), true, false, false)
	f.Add(uint(0), false, true, false)
	f.Add(uint(0), false, false, true)
	f.Add(uint(0), true, true, true)
	f.Add(uint(2), true, true, false)
	f.Add(uint(2), true, false, true)
	f.Add(uint(4), false, true, true)
	f.Add(uint(5), true, true, true)

	f.Fuzz(func(t *testing.T, keyIdx uint, inFile, inEnv, inFlag bool) {
		configtest.Isolate(t)
		row := appKeys[keyIdx%uint(len(appKeys))]
		home := configtest.NewHome(t)

		// app.toml has to exist for this key to come from the file layer, and a
		// file that exists is never rewritten, so writing just the one key is
		// enough — and is also the shape of an app.toml from an older release.
		if inFile {
			literal := fmt.Sprintf("%q", row.Values[0])
			if row.Numeric {
				literal = row.Values[0] // an unquoted TOML integer, so viper returns int64
			}
			home.WriteAppTOML(t, []byte(fmt.Sprintf("%s = %s\n", row.Key, literal)))
		}
		if inEnv {
			setServerEnv(t, row.Key, row.Values[1])
		}
		flagValues := map[string]string{}
		if inFlag {
			flagValues[row.Key] = row.Values[2]
		}

		got := applyLegacy(t, home, flagValues)
		if got.err != nil {
			t.Fatalf("%s: Apply must succeed with legal values in every layer, got %v", row.Key, got.err)
		}

		want := ""
		switch {
		case inFlag:
			want = row.Values[2]
		case inEnv:
			want = row.Values[1]
		case inFile:
			want = row.Values[0]
		}
		if want == "" {
			return // nothing set; the default is the template's business, not this row's
		}

		raw := got.ctx.Viper.Get(row.Key)
		if resolved := fmt.Sprintf("%v", raw); resolved != want {
			t.Fatalf("%s resolved to %q, want the value from the highest present layer (%q)\n"+
				"layers: file=%v env=%v flag=%v", row.Key, resolved, want, inFile, inEnv, inFlag)
		}

		// The resolved Go type is decided by the flag's declared type, not by which
		// layer supplied the value, and not by the TOML scalar's own type.
		//
		// bindFlags copies whatever viper resolved back into the cobra flag for every
		// bound flag, which marks it Changed. viper then answers Get from the flag,
		// converting only the types its switch names — int and its widths, bool, and
		// the slice/map kinds. A uint64 flag falls through to the default branch and
		// comes back as the flag's text. So halt-height written as an unquoted TOML
		// integer still reaches appOpts.Get as a string, while concurrency-workers
		// comes back as an int.
		//
		// Every downstream reader is a cast.To*, which absorbs the difference — which
		// is exactly why this stays invisible until a second manager puts typed values
		// in the viper and the differential compares types.
		if row.WantGoType != "" {
			if got := fmt.Sprintf("%T", raw); got != row.WantGoType {
				t.Fatalf("%s resolved to Go type %s (%#v), want %s\n"+
					"layers: file=%v env=%v flag=%v", row.Key, got, raw, row.WantGoType, inFile, inEnv, inFlag)
			}
		}
	})
}

// FuzzApplyEnvReachesTheStructOnlyForStructurallyKnownKeys pins the sharpest edge
// in the legacy environment story: one environment variable reaches one boot
// channel and not the other, and which one depends on whether the node has booted
// before.
//
// The server viper runs AutomaticEnv, which resolves at Get time — viper.Get(key)
// consults the environment for any key at all. serverCtx.Config, by contrast, is
// produced by viper.Unmarshal, which only walks keys viper knows structurally:
// those bound to a cobra flag, and those present in a config.toml it actually
// read. Those two sets are not the same, so the channels disagree.
//
// The part no operator could guess is that the set changes on the second boot.
// Creating config.toml and reading config.toml are separate branches: the branch
// that writes a fresh file never reads it back, while the branch that finds an
// existing file calls ReadInConfig. So a key that lives in the rendered template
// but has no flag — p2p.queue-type is one — is invisible to Unmarshal on a fresh
// home and visible on every boot afterwards. The same SEID_P2P_QUEUE_TYPE is
// therefore inert on a node's first start and effective on its restart, while
// being visible to every appOpts.Get() both times.
//
// A replacement manager that resolves every key uniformly diverges here, which is
// a decision to ratify rather than a difference to find in production.
func FuzzApplyEnvReachesTheStructOnlyForStructurallyKnownKeys(f *testing.F) {
	f.Add("priority")
	f.Add("simple-priority") // the key's own default: the assertion must not rely on inequality
	f.Add("fifo")
	// An environment variable is bytes, not text, and viper hands whatever it holds
	// straight through. Found by the fuzzer; kept as a seed.
	f.Add("\xeb")

	f.Fuzz(func(t *testing.T, envValue string) {
		if envValue == "" || !configtest.EnvValueIsSettable(envValue) {
			return // an empty variable reads as unset, and a NUL cannot be exported
		}
		configtest.Isolate(t)

		// p2p.queue-type has no cobra flag (neither AddNodeFlags nor
		// addStartNodeFlags registers one) and is rendered in the config.toml
		// template. That combination is what makes it structurally unknown on a
		// fresh home and known on a re-boot.
		const key = "p2p.queue-type"
		const path = "P2P.QueueType"

		home := configtest.NewHome(t)
		setServerEnv(t, key, envValue)
		fromEnv := configtest.DumpAt(path, envValue)

		// First boot: config.toml is created and not read back, so the key is
		// structurally unknown and Unmarshal cannot see the environment.
		first := applyLegacy(t, home, nil)
		if first.err != nil {
			t.Fatalf("first Apply must succeed, got %v", first.err)
		}
		firstLeaf, ok := configtest.LeafAt(configtest.Dump(*first.ctx.Config), path)
		if !ok {
			t.Fatalf("%q is not present in the resolved Tendermint config", path)
		}
		// A fuzzer that happens to generate the key's own default makes firstLeaf equal
		// fromEnv without the environment having reached anything, so that case is exempt.
		// The default is read from tmcfg rather than written as a literal: hardcoding it
		// couples this row to one spelling, and the row would start failing for real the
		// first time the default moved and the fuzzer reached the new value.
		defaultLeaf, ok := configtest.LeafAt(configtest.Dump(*tmcfg.DefaultConfig()), path)
		if !ok {
			t.Fatalf("%q is not present in the default Tendermint config", path)
		}
		if firstLeaf == fromEnv && firstLeaf != defaultLeaf {
			t.Fatalf("on a fresh home the environment reached serverCtx.Config for a flag-less key (%s). "+
				"If the creation branch now reads back the config.toml it wrote, that changes which "+
				"SEID_* variables take effect on a node's first start", firstLeaf)
		}

		// Both boots: viper resolves the environment regardless, because
		// AutomaticEnv answers at Get time.
		if resolved := fmt.Sprintf("%v", first.ctx.Viper.Get(key)); resolved != envValue {
			t.Fatalf("serverCtx.Viper.Get(%q) = %q, want the environment value %q", key, resolved, envValue)
		}

		// Second boot: config.toml now exists, ReadInConfig runs, the key becomes
		// structurally known, and the same variable now moves the struct.
		second := applyLegacy(t, home, nil)
		if second.err != nil {
			t.Fatalf("second Apply must succeed, got %v", second.err)
		}
		secondLeaf, ok := configtest.LeafAt(configtest.Dump(*second.ctx.Config), path)
		if !ok {
			t.Fatalf("%q is not present in the resolved Tendermint config", path)
		}
		if secondLeaf != fromEnv {
			t.Fatalf("on a materialized home the environment must reach serverCtx.Config\n got: %s\nwant: %s",
				secondLeaf, fromEnv)
		}
		if resolved := fmt.Sprintf("%v", second.ctx.Viper.Get(key)); resolved != envValue {
			t.Fatalf("serverCtx.Viper.Get(%q) = %q, want the environment value %q", key, resolved, envValue)
		}
	})
}

// FuzzApplyMalformedConfigTOML feeds arbitrary bytes to config.toml. The property
// is total behavior: Apply either succeeds or returns an error, and never panics,
// leaves the process wedged, or reports success on a file it could not read.
//
// It matters because the file is operator-authored and edited under pressure. A
// truncated write, a stray shell heredoc, a half-applied sed — each produces bytes
// like these, and the difference between "the node refuses to start and says why"
// and "the node starts on partially-parsed config" is the difference between an
// outage and a silent misconfiguration.
func FuzzApplyMalformedConfigTOML(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("moniker = \"ok\"\n"))
	f.Add([]byte("moniker = \n"))
	f.Add([]byte("[rpc\nladdr = \"x\"\n"))
	f.Add([]byte("[[[[["))
	f.Add([]byte("log-level = 42\n"))
	f.Add([]byte("log-level = \"not-a-level\"\n"))
	f.Add([]byte("mode = \"\"\n"))
	f.Add([]byte("rpc.laddr = 1\n"))
	f.Add([]byte("\x00\x01\x02"))
	f.Add([]byte("moniker = \"a\"\nmoniker = \"b\"\n"))

	f.Fuzz(func(t *testing.T, contents []byte) {
		configtest.Isolate(t)
		home := configtest.NewHome(t)
		home.WriteConfigTOML(t, contents)

		got := applyLegacy(t, home, nil)

		if got.err != nil {
			return
		}
		if got.ctx.Config == nil {
			t.Fatal("Apply reported success but left serverCtx.Config nil")
		}
		if got.ctx.Viper == nil {
			t.Fatal("Apply reported success but left serverCtx.Viper nil")
		}
		// Note what is deliberately *not* asserted: that a successful Apply leaves a
		// valid config. It does not, and cannot be made to here — ValidateBasic runs
		// only on the file-creation path. See
		// TestApplyDoesNotValidateAPreExistingConfigFile.
		if got.ctx.Config.RootDir != home.Root {
			t.Fatalf("serverCtx.Config.RootDir = %q, want the resolved home %q", got.ctx.Config.RootDir, home.Root)
		}
	})
}

// TestApplyDoesNotValidateAPreExistingConfigFile records the validation gap the
// legacy path leaves, and it is the single most important row in this file for the
// ConfigManager work, because closing it is one of the new manager's stated goals.
//
// interceptConfigs calls conf.ValidateBasic() only on the branch that *creates*
// config.toml. When the file already exists it is read, unmarshalled, and handed
// back unvalidated. So a config.toml with `mode = ""` — the shape a half-finished
// edit or a templating bug produces — passes Apply cleanly and takes the node down
// later, from node.New, with an error that points at consensus setup rather than at
// the file.
//
// This is exactly the "silent misconfiguration" class the design proposes to make
// structurally extinct by halting at boot validation with the key named. Pinning
// the current behavior is what lets the new manager's halt be recognized as an
// intentional, ratified divergence rather than a regression.
func TestApplyDoesNotValidateAPreExistingConfigFile(t *testing.T) {
	configtest.Isolate(t)

	home := configtest.NewHome(t)
	home.WriteConfigTOML(t, []byte("mode = \"\"\n"))

	got := applyLegacy(t, home, nil)
	if got.err != nil {
		t.Fatalf("legacy Apply must not reject an invalid pre-existing config.toml, got %v", got.err)
	}
	if err := got.ctx.Config.ValidateBasic(); err == nil {
		t.Fatal("mode = \"\" now passes ValidateBasic; the fixture no longer exercises the gap")
	}
	// Restated as the property, so the test fails if Apply starts validating:
	// success from Apply does not imply a bootable config.
}

// TestApplyMergesAppTOMLAfterUnmarshallingTheTendermintConfig pins the ordering
// inside interceptConfigs, which produces a divergence between the two channels
// that no operator could guess.
//
// config.toml is read and unmarshalled into the Tendermint struct *before* app.toml
// is merged, and both files land in one flat viper namespace where app.toml wins
// collisions. So a key that exists in both files resolves one way in
// serverCtx.Config (config.toml's value, because the struct was already built) and
// the other way in serverCtx.Viper (app.toml's value, because it merged last).
//
// A single app.toml key can therefore shadow a Tendermint setting for every
// appOpts.Get() reader while leaving the Tendermint node itself on the config.toml
// value.
func TestApplyMergesAppTOMLAfterUnmarshallingTheTendermintConfig(t *testing.T) {
	configtest.Isolate(t)

	home := configtest.NewHome(t)
	home.WriteConfigTOML(t, []byte("moniker = \"from-config-toml\"\n"))
	home.WriteAppTOML(t, []byte("moniker = \"from-app-toml\"\n"))

	got := applyLegacy(t, home, nil)
	if got.err != nil {
		t.Fatalf("Apply: %v", got.err)
	}

	if got.ctx.Config.Moniker != "from-config-toml" {
		t.Errorf("serverCtx.Config.Moniker = %q, want config.toml's value: the struct is "+
			"unmarshalled before app.toml is merged", got.ctx.Config.Moniker)
	}
	if resolved := fmt.Sprintf("%v", got.ctx.Viper.Get("moniker")); resolved != "from-app-toml" {
		t.Errorf("serverCtx.Viper.Get(\"moniker\") = %q, want app.toml's value: both files "+
			"share one flat namespace and app.toml is merged last", resolved)
	}
}

// FuzzApplyMalformedAppTOML feeds arbitrary bytes to app.toml. The extra property
// on this side is that app.toml and config.toml share one flat viper namespace —
// app.toml is merged into the same instance that already holds config.toml, and
// wins on collisions — so a malformed app.toml can only fail the merge, never
// silently reshape the Tendermint config that was already unmarshalled.
func FuzzApplyMalformedAppTOML(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("halt-height = 1\n"))
	f.Add([]byte("halt-height = \n"))
	f.Add([]byte("[telemetry\nenabled = true\n"))
	f.Add([]byte("moniker = \"app-toml-wins\"\n"))
	f.Add([]byte("telemetry.global-labels = \"not-a-list\"\n"))
	f.Add([]byte("\xff\xfe\x00"))
	f.Add([]byte("[[[[["))

	f.Fuzz(func(t *testing.T, contents []byte) {
		configtest.Isolate(t)
		home := configtest.NewHome(t)
		home.WriteAppTOML(t, contents)

		got := applyLegacy(t, home, nil)

		if got.err != nil {
			return
		}
		if got.ctx.Config == nil || got.ctx.Viper == nil {
			t.Fatal("Apply reported success but left a boot channel nil")
		}
		// app.toml is merged after the Tendermint struct is built, so no app.toml
		// content can make the struct invalid — whatever these bytes contain, the
		// struct came from config.toml (here: the freshly created defaults).
		if err := got.ctx.Config.ValidateBasic(); err != nil {
			t.Fatalf("app.toml content must not be able to invalidate the Tendermint config, got %v", err)
		}
	})
}

// FuzzApplyIsIdempotent pins the property that makes every other assertion in
// this file meaningful: booting the same home twice resolves to the same thing.
//
// It is not trivially true. The first Apply on a fresh home *writes* both files —
// config.toml with hardcoded overrides that DefaultConfig does not carry, and
// app.toml rendered from whatever the viper held at that moment — and the second
// Apply reads what the first one wrote. So this target is really asking whether
// materialization is a fixed point, which is what lets a node restart without its
// configuration drifting, and what lets the differential harness compare two
// managers on a fixture home at all.
//
// The freshly-generated app.toml carries a randomized pruning-interval and a
// hostname-derived moniker, so the comparison is deliberately between run two and
// run three (both of which read files rather than writing them) with run one
// serving only to materialize.
func FuzzApplyIsIdempotent(f *testing.F) {
	f.Add(false, false, "")
	f.Add(true, false, "")
	f.Add(false, true, "")
	f.Add(true, true, "")
	f.Add(true, true, "tcp://127.0.0.1:26656")

	f.Fuzz(func(t *testing.T, seedConfig, seedApp bool, p2pLaddr string) {
		configtest.Isolate(t)
		home := configtest.NewHome(t)
		if seedConfig {
			home.WriteConfigTOML(t, []byte("moniker = \"fixture\"\n"))
		}
		if seedApp {
			home.WriteAppTOML(t, []byte("halt-height = 7\n"))
		}
		flagValues := map[string]string{}
		if p2pLaddr != "" {
			flagValues["p2p.laddr"] = p2pLaddr
		}

		if first := applyLegacy(t, home, flagValues); first.err != nil {
			t.Skipf("materializing boot failed (%v); malformed-input behavior is covered elsewhere", first.err)
		}
		if !home.Exists("config.toml") || !home.Exists("app.toml") {
			t.Fatal("the first Apply must leave both config.toml and app.toml on disk")
		}

		second := applyLegacy(t, home, flagValues)
		third := applyLegacy(t, home, flagValues)
		if second.err != nil || third.err != nil {
			t.Fatalf("re-booting a materialized home must succeed: %v / %v", second.err, third.err)
		}

		if a, b := configtest.Dump(*second.ctx.Config), configtest.Dump(*third.ctx.Config); a != b {
			t.Fatalf("serverCtx.Config differs between two boots of the same home\n--- second\n%s\n--- third\n%s", a, b)
		}
		if a, b := configtest.DumpViper(second.ctx.Viper), configtest.DumpViper(third.ctx.Viper); a != b {
			t.Fatalf("serverCtx.Viper differs between two boots of the same home\n--- second\n%s\n--- third\n%s", a, b)
		}
	})
}

// TestApplyMaterializationOverridesOnlyApplyToACreatedConfigFile pins one of the
// legacy path's least obvious behaviors, and one with a real operational
// consequence.
//
// When config.toml is absent, interceptConfigs writes it after stamping four
// values that neither tmcfg.DefaultConfig nor the template carries: a pprof
// listener on localhost:6060, P2P receive and send rates of 5120000, and a 5s
// unsafe commit-timeout override. When config.toml is present, those stampings do
// not happen — the file is simply read.
//
// So two nodes on the same binary run different consensus timeouts and different
// P2P rate limits depending only on whether their config.toml was generated by
// this code path or by an earlier one. Deleting and regenerating a config.toml
// changes node behavior, which is the opposite of what "regenerate the defaults"
// implies.
func TestApplyMaterializationOverridesOnlyApplyToACreatedConfigFile(t *testing.T) {
	configtest.Isolate(t)

	created := configtest.NewHome(t)
	if got := applyLegacy(t, created, nil); got.err != nil {
		t.Fatalf("Apply on an empty home: %v", got.err)
	}
	generated := applyLegacy(t, created, nil)
	if generated.err != nil {
		t.Fatalf("re-Apply on the materialized home: %v", generated.err)
	}

	// A config.toml that exists but says nothing: the same binary, the same
	// absent keys, and none of the creation-path stampings.
	preexisting := configtest.NewHome(t)
	preexisting.WriteConfigTOML(t, []byte("# authored by an earlier release\n"))
	read := applyLegacy(t, preexisting, nil)
	if read.err != nil {
		t.Fatalf("Apply on a home with a minimal config.toml: %v", read.err)
	}

	checks := []struct {
		what          string
		fromGenerated any
		fromRead      any
	}{
		{"RPC.PprofListenAddress", generated.ctx.Config.RPC.PprofListenAddress, read.ctx.Config.RPC.PprofListenAddress},
		{"P2P.RecvRate", generated.ctx.Config.P2P.RecvRate, read.ctx.Config.P2P.RecvRate},
		{"P2P.SendRate", generated.ctx.Config.P2P.SendRate, read.ctx.Config.P2P.SendRate},
		{
			"Consensus.UnsafeCommitTimeoutOverride",
			generated.ctx.Config.Consensus.UnsafeCommitTimeoutOverride,
			read.ctx.Config.Consensus.UnsafeCommitTimeoutOverride,
		},
	}
	for _, c := range checks {
		if fmt.Sprintf("%v", c.fromGenerated) == fmt.Sprintf("%v", c.fromRead) {
			t.Errorf("%s no longer distinguishes a generated config.toml from a pre-existing one "+
				"(both %v). If the creation-path override was moved into DefaultConfig or the "+
				"template on purpose, that changes behavior for every existing node and needs a "+
				"migration, not just an updated test", c.what, c.fromGenerated)
		}
	}
}

// TestServerEnvPrefixFollowsExecutableBasename pins the environment prefix to
// path.Base(os.Executable()) rather than to the literal "seid".
//
// This is the mechanism behind a genuinely surprising failure mode: renaming or
// symlinking the binary silently changes every environment variable the node
// responds to, so a deployment that invokes the node as `sei-node` ignores every
// SEID_* variable it is given, with no warning. The assertion is written as a
// relationship rather than a constant precisely so it holds inside a test binary
// — which is itself a differently-named executable, and therefore a live
// demonstration of the edge.
func TestServerEnvPrefixFollowsExecutableBasename(t *testing.T) {
	configtest.Isolate(t)

	prefix, err := configtest.ServerEnvPrefix()
	if err != nil {
		t.Fatalf("resolve env prefix: %v", err)
	}
	if prefix == "seid" {
		t.Skip("test binary is named seid; the prefix relationship is not observable here")
	}

	const key = "rpc.laddr"
	const want = "tcp://127.0.0.1:26699"
	const ignored = "tcp://127.0.0.1:26698"

	derivedName := configtest.ServerEnvKey(prefix, key)
	seidName := configtest.ServerEnvKey("seid", key)
	if seidName == derivedName {
		t.Fatalf("derived prefix %q collides with seid; cannot distinguish the two spellings", prefix)
	}

	// The baseline is resolved with neither variable set, so the negative half below can
	// assert an actual fallback rather than merely "not the value I set".
	baseline := applyLegacy(t, configtest.NewHome(t), nil)
	if baseline.err != nil {
		t.Fatalf("Apply: %v", baseline.err)
	}
	unset := baseline.ctx.Config.RPC.ListenAddress
	if unset == want || unset == ignored {
		t.Fatalf("fixture default %q collides with a probe value; pick different probes", unset)
	}

	// The derived name is honored...
	setServerEnv(t, key, want)
	got := applyLegacy(t, configtest.NewHome(t), nil)
	if got.err != nil {
		t.Fatalf("Apply: %v", got.err)
	}
	if got.ctx.Config.RPC.ListenAddress != want {
		t.Fatalf("%s did not take effect; resolved %q, want %q",
			derivedName, got.ctx.Config.RPC.ListenAddress, want)
	}

	// ...and the "seid" spelling is not, because this binary is not named seid. Asserted by
	// resolving it rather than by comparing the two names: that the spellings differ says
	// nothing about which one Apply reads, so the derived variable is cleared and the seid
	// one set alone.
	if err := os.Unsetenv(derivedName); err != nil {
		t.Fatalf("unset %s: %v", derivedName, err)
	}
	if err := os.Setenv(seidName, ignored); err != nil {
		t.Fatalf("set %s: %v", seidName, err)
	}
	fresh := applyLegacy(t, configtest.NewHome(t), nil)
	if fresh.err != nil {
		t.Fatalf("Apply: %v", fresh.err)
	}
	if fresh.ctx.Config.RPC.ListenAddress != unset {
		t.Fatalf("with only %s set the address resolved to %q, want the unset baseline %q. The "+
			"prefix is the literal seid rather than the executable basename %q, which would mean "+
			"a renamed binary keeps responding to SEID_* after all",
			seidName, fresh.ctx.Config.RPC.ListenAddress, unset, prefix)
	}
}

// TestGeneratedAppTOMLDivergesFromTheWasmInCodeDefault pins the [wasm] gas divergence
// against the template seid actually renders.
//
// query_gas_limit is one of the few keys the template writes as a bare literal rather than
// a {{ .Field }} substitution, so the number lives in this package's template string and
// nothing derives it from wasmd's defaults. The consequence is the finding: a node whose
// app.toml seid generated runs smart queries at a tenth of the allowance of a node whose
// app.toml has no [wasm] section, and neither node looks misconfigured by its own file.
//
// The row belongs here rather than beside the reader. A test in the wasm package can only
// hand the reader a number and watch it come back, which proves the reader echoes its
// input and would stay green if the template changed. This reads what a fresh home
// materializes, so editing the literal in the template moves this assertion.
func TestGeneratedAppTOMLDivergesFromTheWasmInCodeDefault(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	// The expected value is stated here and compared against a real generated file, so it is
	// an expectation rather than the echo it would be if it were fed to the reader.
	const generatedLiteral = uint64(300000)

	got := applyLegacy(t, home, nil)
	if got.err != nil {
		t.Fatalf("Apply: %v", got.err)
	}
	if !home.Exists("app.toml") {
		t.Fatal("Apply did not materialize app.toml, so this row is not reading a generated file")
	}
	raw := got.ctx.Viper.Get("wasm.query_gas_limit")
	if raw == nil {
		t.Fatal("a generated app.toml no longer carries wasm.query_gas_limit. If the key left the " +
			"template, every generated node now runs smart queries at wasmd's in-code default " +
			"instead, which raises the allowance tenfold")
	}
	fromTemplate, castErr := cast.ToUint64E(raw)
	if castErr != nil {
		t.Fatalf("wasm.query_gas_limit = %#v does not convert to uint64: %v", raw, castErr)
	}
	if fromTemplate != generatedLiteral {
		t.Fatalf("a generated app.toml resolves wasm.query_gas_limit to %d, and this row expects "+
			"%d. The template literal moved: that changes the gas allowance on every node generated "+
			"from it, so update this row deliberately rather than to make it pass",
			fromTemplate, generatedLiteral)
	}

	inCode := wasmtypes.DefaultWasmConfig().SmartQueryGasLimit
	if fromTemplate == inCode {
		t.Fatalf("the template literal and wasmd's in-code default are both %d. Closing that "+
			"divergence changes what contract queries succeed on every node whose app.toml lacks "+
			"[wasm], so it is recorded here rather than skipped past", inCode)
	}
	if fromTemplate >= inCode {
		t.Fatalf("a generated app.toml (%d) is no longer tighter than the in-code default (%d); the "+
			"direction of the divergence changed", fromTemplate, inCode)
	}
}

// TestApplyLeavesBothChannelsPopulated states the seam's minimum contract, the one
// the ConfigManager interface documents: whichever manager runs, both channels
// come back populated. It is the assertion a new manager fails first.
func TestApplyLeavesBothChannelsPopulated(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	got := applyLegacy(t, home, nil)
	if got.err != nil {
		t.Fatalf("Apply on an empty fixture home must succeed, got %v", got.err)
	}
	if got.ctx.Config == nil {
		t.Fatal("serverCtx.Config is nil")
	}
	if got.ctx.Viper == nil {
		t.Fatal("serverCtx.Viper is nil")
	}
	if got.ctx.Config.RootDir != home.Root {
		t.Fatalf("serverCtx.Config.RootDir = %q, want the resolved home %q", got.ctx.Config.RootDir, home.Root)
	}
	// The viper must carry the app sections app.New reads, not just tendermint keys.
	for _, key := range []string{
		"state-commit.sc-enable",
		"state-store.ss-enable",
		"evm.http_enabled",
		"giga_executor.enabled",
		"admin_server.admin_enabled",
	} {
		if got.ctx.Viper.Get(key) == nil {
			t.Errorf("serverCtx.Viper is missing %q, which app.New reads through appOpts.Get", key)
		}
	}
}
