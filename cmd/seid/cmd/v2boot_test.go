package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configcli"
	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// What selecting the v2 manager does to a booting node, measured through the real handler.

// bootUnder runs a manager against a home and returns the source a node would read.
func bootUnder(t *testing.T, mgr configmanager.ConfigManager, home string) *server.Context {
	t.Helper()
	cmd := server.StartCmd(nil, home, []trace.TracerProviderOption{})
	if err := cmd.Flags().Set(flags.FlagHome, home); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	ctx, err := runManager(t, mgr, cmd)
	if err != nil {
		t.Fatalf("Apply refused the boot: %v", err)
	}
	if ctx.Viper == nil {
		t.Fatal("Apply left the source nil, so nothing below measures anything")
	}
	return ctx
}

// settingsOf reads every enumerable key.
func settingsOf(ctx *server.Context) map[string]any {
	out := map[string]any{}
	for _, key := range ctx.Viper.AllKeys() {
		out[strings.ToLower(key)] = ctx.Viper.Get(key)
	}
	return out
}

// TestSelectingV2WithNoSeiTomlChangesNothing is the property that makes the flag safe to turn on.
//
// A node that has not generated a file is the expected state for the whole migration. If selecting the
// manager changed anything then, enabling it would be a configuration change rather than a switch, and
// every node would have to be re-verified before the first section had moved.
func TestSelectingV2WithNoSeiTomlChangesNothing(t *testing.T) {
	configtest.Isolate(t)
	// One home, and a boot before the two under comparison. The handler writes app.toml and config.toml
	// when they are absent, so a first boot enumerates fewer keys than every boot after it. Comparing
	// a first boot against a second measures that difference and not the managers, which is what this
	// test did until it caught itself.
	home := configtest.NewHome(t)
	bootUnder(t, configmanager.LegacyConfigManager{}, home.Root)

	legacy := settingsOf(bootUnder(t, configmanager.LegacyConfigManager{}, home.Root))
	v2 := settingsOf(bootUnder(t, configmanager.SeiConfigManager{}, home.Root))

	if len(legacy) == 0 {
		t.Fatal("the legacy boot resolved no keys, so this comparison holds for any pair of managers")
	}
	for key, want := range legacy {
		got, present := v2[key]
		if !present {
			t.Errorf("%q is absent under the v2 manager and present under the legacy one", key)
			continue
		}
		if !sameSetting(got, want) {
			t.Errorf("%q reads %#v under v2 and %#v under legacy. With no sei.toml the two must resolve "+
				"identically, or turning the flag on is itself a configuration change", key, got, want)
		}
	}
	for key := range v2 {
		if _, present := legacy[key]; !present {
			t.Errorf("%q appears only under the v2 manager, and no sei.toml asked for it", key)
		}
	}
}

// TestAMalformedSeiTomlDoesNotStopTheNode holds the refusal that must not happen.
//
// The file is hand-editable by design, so a typo in it is a thing that will happen. Refusing to start
// would turn that typo into an outage on the next restart, which is a worse failure than reading the
// values the node was already reading.
func TestAMalformedSeiTomlDoesNotStopTheNode(t *testing.T) {
	configtest.Isolate(t)

	// Each fixture carries a written value for a declared key that differs from every baseline. Without
	// one, a mode guessed in place of the unusable one would install baselines that match what the node
	// already reads, and this test would hold for exactly the fallback it exists to forbid.
	const differing = "\n[giga_executor]\nocc_enabled = false\n"
	for _, tc := range []struct{ name, body string }{
		{"not toml at all", "this is not = = toml"},
		{"no node mode", "schema_version = 1\n" + differing},
		{"a mode no release produced", "schema_version = 1\nnode_mode = \"archival\"\n" + differing},
		{"an empty mode", "schema_version = 1\nnode_mode = \"\"\n" + differing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := configtest.NewHome(t)
			seed(t, home.Root, tc.body)

			// A boot before the two under comparison, so both read files that already exist.
			bootUnder(t, configmanager.LegacyConfigManager{}, home.Root)
			legacy := settingsOf(bootUnder(t, configmanager.LegacyConfigManager{}, home.Root))
			v2 := settingsOf(bootUnder(t, configmanager.SeiConfigManager{}, home.Root))

			if len(legacy) == 0 {
				t.Fatal("the legacy boot resolved no keys, so this comparison holds for anything")
			}
			// Nothing installed, so the node reads exactly what it always read. Asserted against the
			// legacy boot rather than merely checking that something resolved, because a mode guessed
			// in place of the unusable one would also resolve plenty.
			for key, want := range legacy {
				if got, present := v2[key]; !present || !sameSetting(got, want) {
					t.Errorf("with %s in sei.toml, %q reads %#v under v2 and %#v under legacy. An "+
						"unusable file has to leave the node reading what it always read, not a mode "+
						"picked in place of the one it could not use", tc.name, key, got, want)
				}
			}
		})
	}
}

// TestASeiTomlWithAUsableModeIsInstalled is the other direction.
//
// The tests above would all hold for a manager that ignored sei.toml entirely, so this one requires a
// declared key to actually take its resolved value.
func TestASeiTomlWithAUsableModeIsInstalled(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	// Generated for archive, which is the mode config.toml cannot express, then given a value that
	// differs from every baseline. Without that difference the install is unobservable: the one
	// registered section runs the same defaults a node already runs, which is deliberate and makes it
	// useless for telling an install from a no-op.
	file, err := configcli.Generate("archive")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := file.Set("giga_executor.occ_enabled", false); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := file.Save(configcli.Path(home.Root)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx := bootUnder(t, configmanager.SeiConfigManager{}, home.Root)

	written, err := file.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if len(written) == 0 {
		t.Fatal("the generated file holds no keys, so nothing could have been installed")
	}
	for key, want := range written {
		if got := ctx.Viper.Get(key); !sameSetting(got, want) {
			t.Errorf("%q reads %#v from the booted source and sei.toml writes %#v. A declared key that "+
				"does not take the value the operator wrote makes the whole file decorative", key, got, want)
		}
	}
	// The value the operator wrote differs from the baseline, so this cannot pass for an install that
	// only ever writes baselines.
	if got := ctx.Viper.Get("giga_executor.occ_enabled"); got != false {
		t.Errorf("giga_executor.occ_enabled reads %#v and sei.toml writes false. Resolving without the "+
			"file's own values installs baselines over what the operator chose", got)
	}
}

// TestTheFileNameTheBootReadsIsTheOneTheVerbsWrite keeps the two spellings together.
//
// The boot names the file itself rather than importing the command package, so that the node does not
// depend on the verbs that write it. That duplication is only safe while a test compares them.
func TestTheFileNameTheBootReadsIsTheOneTheVerbsWrite(t *testing.T) {
	home := t.TempDir()
	written := configcli.Path(home)

	// The boot resolves <home>/config/<name>, so the verbs' path has to end the same way.
	if got := filepath.Base(written); got != configcli.FileName {
		t.Fatalf("the verbs write %q", got)
	}
	if !strings.HasSuffix(written, filepath.Join("config", "sei.toml")) {
		t.Errorf("the verbs write %q and the boot reads <home>/config/sei.toml. A node would generate "+
			"a file nothing reads", written)
	}
}

// seed writes a sei.toml into a home.
func seed(t *testing.T, home, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "sei.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("seed sei.toml: %v", err)
	}
}
