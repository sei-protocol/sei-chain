package configmanager

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/spf13/cobra"
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
	out := installWithSeiToml(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[mempool]\nsize = 4321\n")

	if strings.Contains(out, "supplies no declared value") {
		t.Errorf("a file supplying mempool.size is reported as supplying no declared value:\n%s", out)
	}
	if !strings.Contains(out, "mempool.size") {
		t.Errorf("the held-back key is not named anywhere, so an operator has no way to learn their "+
			"value did not arrive:\n%s", out)
	}
}

// TestAFileSupplyingNothingStillSaysSo keeps the fix above from silencing the case it was scoped around.
func TestAFileSupplyingNothingStillSaysSo(t *testing.T) {
	out := installWithSeiToml(t, "schema_version = 1\nnode_mode = \"validator\"\n")

	if !strings.Contains(out, "supplies no declared value") {
		t.Errorf("a file that really supplies nothing no longer says so:\n%s", out)
	}
}

// TestOnlyTheBootReportsTheRoutineLine keeps a configuration line off every other command.
//
// Every subcommand passes through the same pre-run, so `seid keys list` and `seid q` install too. The
// routine line is held at a level above the operator's own log_level, so reporting it there puts a
// configuration line on every invocation of a CLI that was asked something else.
//
// A problem still reports on any command. Those are not routine.
func TestOnlyTheBootReportsTheRoutineLine(t *testing.T) {
	const file = "schema_version = 1\nnode_mode = \"validator\"\n\n[evm]\nmax_tx_pool_txs = 111\n"

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
			said := strings.Contains(out, "configuration installed")
			if said != tc.routine {
				t.Errorf("`seid %s` reports the routine line at info: %v, want %v.\n%s",
					tc.command, said, tc.routine, out)
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
