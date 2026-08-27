package configmanager

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
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
	// A command with no server context is the cheapest way in: the install reads one out of the command
	// and the path under test is whatever it does with what it finds.
	installResolved(nil, map[string]string{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
