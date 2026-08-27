package configmanager

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
