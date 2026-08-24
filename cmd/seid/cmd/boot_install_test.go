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
	t.Run("nothing written leaves the key as it was", func(t *testing.T) {
		configtest.Isolate(t)
		ctx := bootWith(t, "schema_version = 1\nnode_mode = \"validator\"\n", nil)
		if got := ctx.Viper.Get(bootProbeKey); got != nil {
			t.Errorf("%s reads %#v with nothing written. A file that supplies no value installs nothing, "+
				"so this key should read as it did before the manager ran", bootProbeKey, got)
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

// TestOnlyWhatASourceSuppliedIsInstalled is the property that makes this safe to enable.
//
// A resolution answers for every declared key. Installing all of it would write a default over whatever a
// node's app.toml holds for every key its sei.toml does not mention, so moving one setting would replace a
// hundred and fifty. This installs only what a source supplied, so a key reaches a node exactly when
// somebody asked for it.
//
// Measured as an absence rather than against a value read back from a second boot. A baseline taken through
// this same install would carry whatever the install wrote, so an install that wrote a default over every
// key would write the same one twice and the two runs would agree. The assertion is that the key is not
// there at all, which no install can satisfy by being wrong the same way twice.
func TestOnlyWhatASourceSuppliedIsInstalled(t *testing.T) {
	configtest.Isolate(t)

	// The declared value is read out first, because a key whose declaration answers nothing would pass
	// this whether the install was contained or not.
	const untouched = bootUntouchedKey
	resolved, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Values[untouched] == nil {
		t.Fatalf("%s declares no value, so an install that wrote every declared default would leave it "+
			"absent too and this would measure nothing", untouched)
	}

	ctx := bootWith(t, seiTomlWriting(bootProbeKey, "111"), nil)

	if got := ctx.Viper.Get(untouched); got != nil {
		t.Errorf("%s reads %#v after a file that never mentions it, and %s declares %#v. Installing a "+
			"declared default over a key nobody wrote replaces an operator's configuration rather than "+
			"moving one setting of it", untouched, got, untouched, resolved.Values[untouched])
	}
	if got := ctx.Viper.Get(bootProbeKey); !sameSetting(got, int64(111)) {
		t.Errorf("%s reads %#v, so nothing was installed at all and the check above holds for an install "+
			"that does nothing", bootProbeKey, got)
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
		t.Skipf("%s is not declared, so this cannot happen through it", key)
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
	for name, body := range map[string]string{
		"no file at all":       "",
		"a mode nothing knows": "schema_version = 1\nnode_mode = \"sentry\"\n" + supplies,
		"no mode at all":       "schema_version = 1\n" + supplies,
		"not parseable":        "schema_version = 1\nnode_mode = \"validator\"\n[evm\n" + supplies,
	} {
		t.Run(name, func(t *testing.T) {
			configtest.Isolate(t)
			ctx := bootWith(t, body, nil)
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
