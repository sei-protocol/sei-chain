package configmanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/spf13/cobra"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// TestAnUnreadableSeiTomlIsDistinguishedFromAnAbsentOne holds the difference an operator depends on.
//
// A node with no sei.toml is every node today, so that case is quiet. A node whose sei.toml cannot be read
// is a node where somebody wrote the file and it is doing nothing, and the two cannot share an answer: an
// error collapsed into "no file" is a mistake reported as the normal case, and the only signal the operator
// has for it is the one that got collapsed.
func TestAnUnreadableSeiTomlIsDistinguishedFromAnAbsentOne(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		write   bool
		wantErr func(error) bool
	}{
		{
			name:    "absent",
			wantErr: func(err error) bool { return errors.Is(err, fs.ErrNotExist) },
		},
		{
			name:    "unparseable",
			body:    "[evm\n",
			write:   true,
			wantErr: func(err error) bool { return err != nil && !errors.Is(err, fs.ErrNotExist) },
		},
		{
			name:    "no node mode",
			body:    "schema_version = 1\n",
			write:   true,
			wantErr: func(err error) bool { return err != nil && !errors.Is(err, fs.ErrNotExist) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
				t.Fatalf("make a home: %v", err)
			}
			if tc.write {
				if err := os.WriteFile(filepath.Join(home, "config", seiTomlName),
					[]byte(tc.body), 0o600); err != nil {
					t.Fatalf("write the file: %v", err)
				}
			}

			_, err := readSeiTomlAt(home)
			if !tc.wantErr(err) {
				t.Errorf("reading a %s sei.toml returned %v, which does not tell an operator which of "+
					"the two happened", tc.name, err)
			}
		})
	}
}

// TestAPanicWhileInstallingDoesNotRefuseTheBoot holds the claim the install path makes about itself.
//
// Selecting this manager is meant to be a switch rather than a configuration change: a node with nothing
// written, or something wrong written, starts exactly as before. Reflection over the node's own types and
// two decoding libraries sit under this, so a panic is a shape nobody predicted, and letting one escape
// would refuse a boot for the single reason this path promises never to refuse one.
func TestAPanicWhileInstallingDoesNotRefuseTheBoot(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a panic escaped the install path and would have refused the boot: %v", r)
		}
	}()

	var reported bytes.Buffer
	// No command at all. Reading a server context out of one is the first thing the install does, and the
	// path under test is whatever happens when that cannot be done.
	installResolved(nil, map[string]string{},
		slog.New(slog.NewTextHandler(&reported, &slog.HandlerOptions{Level: slog.LevelDebug})))

	// The recover has to be shown to have run. Without this the test passes whether or not anything
	// panicked, so the day the library this reaches grows a guard of its own, the recover stops being
	// exercised and nothing here says so.
	if !strings.Contains(reported.String(), "panicked") {
		t.Fatalf("nothing panicked, so the recover this test exists for was never entered. What the "+
			"install reported instead: %q", reported.String())
	}
}

// installWithSeiToml runs the install against a home holding the given sei.toml, and returns what it
// reported.
func installWithSeiToml(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", seiTomlName), []byte(body), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}

	// The boot carries the server context on the command, which is where the install reads it from.
	cmd := &cobra.Command{Use: "probe"}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey,
		server.NewDefaultContext()))
	cmd.Flags().String(flags.FlagHome, home, "")

	var out bytes.Buffer
	installResolved(cmd, map[string]string{},
		slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return out.String()
}

// TestAFileHoldingOnlyDecodedKeysIsNotReportedAsSupplyingNothing covers the one report an operator has.
//
// The sections whose reader decodes its file whole are deliberately left out of this install, because
// putting a value into the source reaches nothing for them. Those keys are then invisible: absent from
// what was installed, and declared, so absent from the undeclared keys too.
//
// An operator whose file holds only such keys therefore supplied plenty and would be told it supplied
// nothing, while their node ran the old values. That is the failure the whole surface exists to remove,
// arriving through the report meant to prevent it.
func TestAFileHoldingOnlyDecodedKeysIsNotReportedAsSupplyingNothing(t *testing.T) {
	// mempool is read by a decode, so its keys are held back by this install.
	out := installWithSeiToml(t, "schema_version = 1\nnode_mode = \"full\"\n\n[mempool]\nsize = 4321\n")

	if !strings.Contains(out, "mempool.size") {
		t.Errorf("the held-back key is not named anywhere, so an operator has no way to learn their "+
			"value did not arrive:\n%s", out)
	}
}

// TestOnlyTheBootReportsTheRoutineLine keeps a configuration line off every other command.
//
// Every subcommand passes through the same pre-run, so `seid keys list` and `seid q` install too. The
// routine line is held at a level above the operator's own log_level, so reporting it there puts a
// configuration line on every invocation of a CLI that was asked something else.
//
// A problem still reports on any command. Those are not routine.
//
// Both deliveries are held to it. The file below writes one key each side, so a routine line from the
// install and one from the decode both have to follow the command rather than only the install's.
func TestOnlyTheBootReportsTheRoutineLine(t *testing.T) {
	const file = "schema_version = 1\nnode_mode = \"full\"\n" +
		"\n[evm]\nmax_tx_pool_txs = 111\n" +
		"\n[mempool]\nsize = 4321\n"

	for _, tc := range []struct {
		command string
		routine bool
	}{
		{"start", true},
		{"keys", false},
		{"q", false},
	} {
		t.Run(tc.command, func(t *testing.T) {
			out := installOnCommand(t, tc.command, file, slog.LevelInfo)
			for _, line := range []struct{ what, says string }{
				{"install", "configuration installed"},
				{"decode", "this section's settings now differ"},
			} {
				if said := strings.Contains(out, line.says); said != tc.routine {
					t.Errorf("`seid %s` reports the %s delivery's routine line at info: %v, want %v.\n%s",
						tc.command, line.what, said, tc.routine, out)
				}
			}
		})
	}
}

// installOnCommand runs the install as the named command, capturing what it reported at or above level.
func installOnCommand(t *testing.T, name, body string, level slog.Level) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", seiTomlName), []byte(body), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}

	cmd := &cobra.Command{Use: name}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey,
		server.NewDefaultContext()))
	cmd.Flags().String(flags.FlagHome, home, "")

	var out bytes.Buffer
	installResolved(cmd, map[string]string{},
		slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: level})))
	return out.String()
}

// TestAFlagIsNotReportedAsSomethingTheFileWrote covers the report an operator reads on an ordinary boot.
//
// A key a flag or a variable answered lands in the same supplied set the file's keys do. For a section read
// by a decode, reporting that set names the file for a key it does not contain, and says the value reads as
// it always has when the flag does in fact apply. `seid start --log_level=info` on any node with a sei.toml
// hits it, because log-level is a declared root key of a decoded section.
func TestAFlagIsNotReportedAsSomethingTheFileWrote(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A file that supplies nothing at all.
	if err := os.WriteFile(filepath.Join(home, "config", seiTomlName),
		[]byte("schema_version = 1\nnode_mode = \"full\"\n"), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}

	cmd := &cobra.Command{Use: "start"}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey,
		server.NewDefaultContext()))
	cmd.Flags().String(flags.FlagHome, home, "")

	var out bytes.Buffer
	// The flag answers a declared key of a section a decode delivers.
	installResolved(cmd, map[string]string{"log_level": "info"},
		slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))

	if strings.Contains(out.String(), "sei.toml writes keys whose reader decodes") {
		t.Errorf("a key only a flag supplied is reported as something sei.toml writes, and as reading the "+
			"way it always has when the flag applies:\n%s", out.String())
	}
}

// TestNoHomeMeansNoReadRatherThanAForeignFile holds the one case where reading is worse than not reading.
//
// Every path here joins the home directory with config/sei.toml. An empty home leaves the relative path,
// so the read lands wherever the process happens to have started, and whatever sei.toml is there is
// installed into this node. A node configured from a directory nobody chose is the outcome, and it reports
// as a successful install.
func TestNoHomeMeansNoReadRatherThanAForeignFile(t *testing.T) {
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(elsewhere, "config"), 0o750); err != nil {
		t.Fatalf("make a directory holding another node's file: %v", err)
	}
	// A complete, valid file for some other node. Nothing about it is wrong; it is simply not this node's.
	if err := os.WriteFile(filepath.Join(elsewhere, "config", seiTomlName),
		[]byte("schema_version = 1\nnode_mode = \"full\"\n"), 0o600); err != nil {
		t.Fatalf("write another node's sei.toml: %v", err)
	}
	t.Chdir(elsewhere)

	// No --home flag and no home in the environment, which is what a subcommand invoked without one has.
	cmd := &cobra.Command{Use: "start"}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey,
		server.NewDefaultContext()))

	var out bytes.Buffer
	file, ok := readSeiToml(cmd, slog.New(slog.NewTextHandler(&out,
		&slog.HandlerOptions{Level: slog.LevelDebug})))
	if ok || file != nil {
		t.Fatalf("with no home set, the read returned another node's file from the working directory, so "+
			"its values would install into this one:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "no home directory is set") {
		t.Errorf("declining to read is right, but the reason an operator gets does not name it:\n%s",
			out.String())
	}
}

// TestARefusedInstallPublishesNothing holds the two deliveries to one outcome.
//
// One of them refuses its whole set on a single bad pair; the other refuses one section at a time. Run in
// the wrong order the per-section delivery commits first, so a node reads sei.toml for the settings its own
// file decodes and its own files for everything else, under a line saying nothing was applied. That state
// is described nowhere.
//
// The refusing state is built here rather than reached through a boot. A source enumerating a path a
// declared key occupies is what the install refuses, and the readers of the node's own files reject that
// shape before it reaches the source, so a boot cannot currently produce it. The refusal is still what the
// install does when handed it, and the order it happens in is what this holds.
func TestARefusedInstallPublishesNothing(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", seiTomlName),
		[]byte("schema_version = 1\nnode_mode = \""+tmcfg.DefaultConfig().Mode+
			"\"\n\n[mempool]\nsize = 7777\n"),
		0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}

	sctx := server.NewDefaultContext()
	// "pruning" is declared, so a value at pruning.foo names a path that key already occupies.
	sctx.Viper.Set("pruning.foo", 1)
	before := sctx.Config.Mempool.Size

	cmd := &cobra.Command{Use: "start"}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, sctx))
	cmd.Flags().String(flags.FlagHome, home, "")

	var out bytes.Buffer
	installResolved(cmd, nil, slog.New(slog.NewTextHandler(&out,
		&slog.HandlerOptions{Level: slog.LevelDebug})))

	if !strings.Contains(out.String(), "cannot install this node's configuration") {
		t.Fatalf("the install was expected to refuse and did not, so this measures nothing:\n%s",
			out.String())
	}
	if got := sctx.Config.Mempool.Size; got != before {
		t.Errorf("the install refused every one of its keys and mempool.size moved from %d to %d anyway. "+
			"Half the configuration came from sei.toml and half from the node's own files, under a line "+
			"reporting that nothing was applied", before, got)
	}
}

// TestNothingIsDeliveredWhenTheNodeRunsWithNoKind covers the kind a node holds when its own file states the
// key and leaves it empty.
//
// A boot unmarshals that empty string over its default, so the node really does run with no kind at all.
// Treating it as agreement would deliver whatever kind sei.toml names to a node that is none of them, and
// every value delivered is the answer for one kind.
func TestNothingIsDeliveredWhenTheNodeRunsWithNoKind(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", seiTomlName),
		[]byte("schema_version = 1\nnode_mode = \""+tmcfg.DefaultConfig().Mode+
			"\"\n\n[api]\nmax-open-connections = 4321\n"), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}

	sctx := server.NewDefaultContext()
	// The kind emptied the way the node's own file can state it.
	sctx.Config.Mode = ""

	cmd := &cobra.Command{Use: "start"}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, sctx))
	cmd.Flags().String(flags.FlagHome, home, "")

	var out bytes.Buffer
	installResolved(cmd, nil, slog.New(slog.NewTextHandler(&out,
		&slog.HandlerOptions{Level: slog.LevelDebug})))

	if got := fmt.Sprint(sctx.Viper.Get("api.max-open-connections")); got == "4321" {
		t.Errorf("the node runs with no kind at all and a value was delivered anyway, so it is "+
			"configured as a kind it is not:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "says another") {
		t.Errorf("nothing was delivered, and the reason an operator gets does not name the "+
			"disagreement:\n%s", out.String())
	}
}

// TestTheLevelFollowsTheStructEvenWhenNobodyWroteIt keeps three answers about the log level together.
//
// The decode publishes whatever the resolution answered into the struct, so a level nobody wrote still
// moves the field. Applying only what a source supplied left the struct saying one level, the process
// running another, and the report naming a move that never reached the logger.
func TestTheLevelFollowsTheStructEvenWhenNobodyWroteIt(t *testing.T) {
	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, wrote := resolved.Values[logLevelKey]; !wrote {
		t.Fatalf("%s is not answered by an empty resolution, so this measures nothing", logLevelKey)
	}
	for _, key := range resolved.Overrides {
		if key == logLevelKey {
			t.Fatalf("a source supplied %s, and this case is about the level nobody wrote", logLevelKey)
		}
	}

	var out bytes.Buffer
	log := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	applyResolvedLogLevel(resolved, map[string]string{}, log)

	if !strings.Contains(out.String(), "resolved log level applied") {
		t.Errorf("no source wrote the level and it was not applied, so the struct the decode publishes "+
			"and the level the process runs at disagree:\n%s", out.String())
	}
}

// TestAPathThatAppliedNothingChangedNoLevelEither holds the reports that say nothing was applied.
//
// The level is the one setting this manager changes outside the two deliveries, so applying it before the
// gates would make every one of those reports false for it.
func TestAPathThatAppliedNothingChangedNoLevelEither(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A kind the node does not run as, which is the gate that reports nothing was delivered.
	other := "validator"
	if tmcfg.DefaultConfig().Mode == other {
		t.Fatalf("the default kind is %q, so this cannot tell agreement from disagreement", other)
	}
	if err := os.WriteFile(filepath.Join(home, "config", seiTomlName),
		[]byte("schema_version = 1\nnode_mode = \""+other+"\"\n"), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}

	cmd := &cobra.Command{Use: "start"}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey,
		server.NewDefaultContext()))
	cmd.Flags().String(flags.FlagHome, home, "")

	var out bytes.Buffer
	installResolved(cmd, nil, slog.New(slog.NewTextHandler(&out,
		&slog.HandlerOptions{Level: slog.LevelDebug})))

	if !strings.Contains(out.String(), "says another") {
		t.Fatalf("the kind gate did not fire, so this measures nothing:\n%s", out.String())
	}
	if strings.Contains(out.String(), "resolved log level applied") {
		t.Errorf("nothing was delivered and the log level was applied anyway, so the report saying every "+
			"key reads as it always has is false for this one:\n%s", out.String())
	}
}
