package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// Keys these tests measure through, and why each one.
//
// The first two are declared keys that nothing else in a booting node answers: no start flag carries them
// and the generated app.toml does not name them, so a value read back for one came from this install and
// from nowhere else. Of a hundred and fifty declared keys only eleven are like that, and the rest are
// reachable by a flag of the same name, whose registration default answers before the lookup comes back
// empty. Measuring through one of those would be reading the flag's default and calling it an install.
//
// The third is the opposite case on purpose: a key a start flag does carry, so it is the one that can show
// the flag channel reaching a declared key at all.
const (
	bootProbeKey     = "evm.max_tx_pool_txs"
	bootUntouchedKey = "state-commit.sc-snapshot-writer-limit"
	bootFlagKey      = "state-sync.snapshot-keep-recent"
)

// bootWith runs a real boot against a sei.toml and returns the source a node would read.
//
// Flags are set through the command rather than handed to the install, because it is the flag being marked
// changed that the snapshot reads. A value poked in directly would hold even if the boot never looked at
// the command line.
func bootWith(t *testing.T, body string, typed map[string]string) *server.Context {
	t.Helper()
	home := configtest.NewHome(t)
	if err := os.MkdirAll(filepath.Join(home.Root, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if body != "" {
		path := filepath.Join(home.Root, "config", "sei.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write sei.toml: %v", err)
		}
	}

	cmd := server.StartCmd(nil, home.Root, []trace.TracerProviderOption{})
	if err := cmd.Flags().Set("home", home.Root); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	for name, value := range typed {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set --%s=%s: %v", name, value, err)
		}
	}
	ctx, err := runManager(t, configmanager.SeiConfigManager{}, cmd)
	if err != nil {
		t.Fatalf("the boot was refused: %v", err)
	}
	return ctx
}

// seiTomlWriting returns a file body that writes one key, wherever that key belongs.
//
// A key with no section goes above every table. Once a table heading is open every bare key after it
// belongs to that table, so a node-wide setting written after one would be read under the wrong name.
func seiTomlWriting(key, value string) string {
	const header = "schema_version = 1\nnode_mode = \"validator\"\n"
	if i := indexOf(key, '.'); i >= 0 {
		return header + "\n[" + key[:i] + "]\n" + key[i+1:] + " = " + value + "\n"
	}
	return header + key + " = " + value + "\n"
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// TestEachChannelWinsOverTheOneBelowIt drives the declared order through a real boot.
//
// Every channel that can carry a value has to reach the resolution. A channel that is not wired does not
// fail, it stops applying: a value an operator supplied through it loses to a lower layer and nothing
// reports it. So each one is supplied a value and the declared order has to hold.
func TestEachChannelWinsOverTheOneBelowIt(t *testing.T) {
	t.Run("nothing written leaves the key at what this binary declares", func(t *testing.T) {
		// Not at whatever app.toml said. This manager is where a node's configuration comes from, so a key
		// sei.toml does not mention is answered by a declaration rather than by what happened to be on
		// disk.
		configtest.Isolate(t)
		ctx := bootWith(t, "schema_version = 1\nnode_mode = \"validator\"\n", nil)
		declared, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got, want := ctx.Viper.Get(bootProbeKey), declared.Values[bootProbeKey]; !sameSetting(got, want) {
			t.Errorf("%s reads %#v with nothing written, want the declared %#v. A key the file leaves out "+
				"is answered by this binary's declaration for the kind of node it is", bootProbeKey, got, want)
		}
	})

	t.Run("the file beats the default", func(t *testing.T) {
		configtest.Isolate(t)
		ctx := bootWith(t, seiTomlWriting(bootProbeKey, "111"), nil)
		if got := ctx.Viper.Get(bootProbeKey); !sameSetting(got, int64(111)) {
			t.Errorf("%s reads %#v with 111 written, want 111. A file channel that is not passed to the "+
				"resolution leaves the operator's value losing to the default", bootProbeKey, got)
		}
	})

	t.Run("the environment beats the file", func(t *testing.T) {
		configtest.Isolate(t)
		t.Setenv(registry.EnvName(bootProbeKey), "222")
		ctx := bootWith(t, seiTomlWriting(bootProbeKey, "111"), nil)
		if got := ctx.Viper.Get(bootProbeKey); !sameSetting(got, "222") {
			t.Errorf("%s reads %#v with 111 in the file and 222 in the environment, want 222", bootProbeKey, got)
		}
	})

	t.Run("a typed flag beats both", func(t *testing.T) {
		configtest.Isolate(t)
		t.Setenv(registry.EnvName(bootFlagKey), "222")
		ctx := bootWith(t, seiTomlWriting(bootFlagKey, "111"), map[string]string{bootFlagKey: "333"})
		if got := ctx.Viper.Get(bootFlagKey); !sameSetting(got, "333") {
			t.Errorf("%s reads %#v with 111 in the file, 222 in the environment and --%s=333 typed, "+
				"want 333. An operator who types a flag to override a file has to win, and a flag "+
				"whose name never reaches the resolution loses to both", bootFlagKey, got, bootFlagKey)
		}
	})
}

// TestEveryDeclaredKeyIsInstalled is what makes sei.toml the node's configuration rather than a patch on it.
//
// A resolution answers for every declared key, and every one of those answers is installed. So a key an
// operator did not write is answered by this binary's declaration for the kind of node it is, and app.toml
// is not consulted for a declared key at all.
//
// The cost is deliberate and it is large: on a node whose app.toml was hand-tuned, every declared key
// absent from sei.toml moves to its declared value. That is why a path rendering sei.toml from a node's
// existing files has to land before this is switched on anywhere.
//
// The key measured is one the file never mentions, so it can only be there because the install put it
// there. Its declared value is read out first, because a key whose declaration answers nothing would pass
// this whichever way the install behaved.
func TestEveryDeclaredKeyIsInstalled(t *testing.T) {
	configtest.Isolate(t)

	const untouched = bootUntouchedKey
	resolved, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Values[untouched] == nil {
		t.Fatalf("%s declares no value, so this would measure nothing", untouched)
	}

	ctx := bootWith(t, seiTomlWriting(bootProbeKey, "111"), nil)

	if got := ctx.Viper.Get(untouched); !sameSetting(got, resolved.Values[untouched]) {
		t.Errorf("%s reads %#v after a file that never mentions it, want the declared %#v. A key the "+
			"file leaves out is answered by the declaration, not by whatever app.toml held",
			untouched, got, resolved.Values[untouched])
	}
	if got := ctx.Viper.Get(bootProbeKey); !sameSetting(got, int64(111)) {
		t.Errorf("%s reads %#v, so the written value did not arrive and the check above would hold for "+
			"an install that wrote defaults over everything and nothing else", bootProbeKey, got)
	}
}

// TestAppTomlDoesNotReachTheFlagChannel is the guard on where the flag snapshot is taken.
//
// The handler this manager re-enters copies configuration values into flags, so that a file can supply a
// flag's default: for every flag whose name its source knows a value for, it calls Set, and Set marks the
// flag changed. After that has run, a flag an operator typed and a key their app.toml holds cannot be told
// apart.
//
// A flag channel built from that state puts app.toml at the top of the order, above sei.toml, which is a
// worse inversion than the one the channel exists to prevent. Taking the snapshot at the entry to Apply is
// what keeps the two apart, and there is no later point where the truth survives.
func TestAppTomlDoesNotReachTheFlagChannel(t *testing.T) {
	const key = "state-sync.snapshot-keep-recent"
	if _, declared := declaredKey(key); !declared {
		t.Fatalf("%s is not declared, so this test cannot reach the inversion it exists for. Skipping "+
			"instead would leave the only guard on it passing while measuring nothing", key)
	}
	configtest.Isolate(t)

	home := configtest.NewHome(t)
	// app.toml holds one value and sei.toml another, and the operator typed no flag at all.
	home.WriteAppTOML(t, []byte("[state-sync]\nsnapshot-keep-recent = 77\n"))
	if err := os.MkdirAll(filepath.Join(home.Root, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "schema_version = 1\nnode_mode = \"validator\"\n\n[state-sync]\nsnapshot-keep-recent = 111\n"
	if err := os.WriteFile(filepath.Join(home.Root, "config", "sei.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}

	cmd := server.StartCmd(nil, home.Root, []trace.TracerProviderOption{})
	if err := cmd.Flags().Set("home", home.Root); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	ctx, err := runManager(t, configmanager.SeiConfigManager{}, cmd)
	if err != nil {
		t.Fatalf("the boot was refused: %v", err)
	}

	if got := ctx.Viper.Get(key); !sameSetting(got, int64(111)) {
		t.Errorf("%s reads %#v with 77 in app.toml, 111 in sei.toml and no flag typed, want 111.\n\n"+
			"A value of 77 means app.toml arrived through the flag channel, because the handler marked "+
			"the flag changed on its behalf. The snapshot has to be taken before the handler runs", key, got)
	}
}

// TestAFileThisBinaryCannotUseLeavesTheNodeAsItWas is the promise that makes the switch safe.
//
// Selecting this manager is a switch rather than a configuration change, so a file it cannot use installs
// nothing and the node reads what it always read. Refusing instead would turn a mistyped line in a
// hand-editable file into an outage on the next restart.
//
// Every case writes a value for a declared key, so a file that was wrongly accepted would install one and
// the assertion would see it. A case supplying nothing would read as unusable whether it was refused or
// accepted, which measures the absence of a value rather than the refusal.
func TestAFileThisBinaryCannotUseLeavesTheNodeAsItWas(t *testing.T) {
	supplies := "\n[evm]\nmax_tx_pool_txs = 111\n"
	for _, tc := range []struct {
		name string
		body string
	}{
		{"no file at all", ""},
		{"a mode nothing knows", "schema_version = 1\nnode_mode = \"sentry\"\n" + supplies},
		{"no mode at all", "schema_version = 1\n" + supplies},
		{"not parseable", "schema_version = 1\nnode_mode = \"validator\"\n[evm\n" + supplies},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configtest.Isolate(t)
			ctx := bootWith(t, tc.body, nil)
			if got := ctx.Viper.Get(bootProbeKey); got != nil {
				t.Errorf("%s reads %#v, so a value was installed from a file this binary cannot use. "+
					"A node whose file names a mode this binary does not know would run one mode's "+
					"answers while being configured as another", bootProbeKey, got)
			}
		})
	}
}

// sameSetting compares two resolved values without caring which shape carried them.
//
// A value reaches a source as its own Go type from a default, as whatever the file format decoded to from
// a file, and as one string from a variable. A comparison that insisted on the type would be asserting
// which channel answered rather than what the node reads.
func sameSetting(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// declaredKey reports whether the registry declares a key.
func declaredKey(key string) (string, bool) {
	for _, section := range registry.Sections() {
		for _, k := range section.Keys {
			if k == key {
				return section.Name, true
			}
		}
	}
	return "", false
}
