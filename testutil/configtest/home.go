package configtest

import (
	"os"
	"path/filepath"
	"testing"
)

// Home is a fixture node directory: the --home a test points seid at, with its
// config/ contents under the test's control byte for byte.
//
// The legacy path treats a node directory as read-write during a read: an absent
// config.toml is created (with hardcoded overrides that DefaultConfig does not
// carry), and an absent app.toml is materialized from whatever the viper holds at
// that moment. A fixture home is therefore both the input and part of the
// observed output, which is why every test gets a fresh one.
type Home struct {
	// Root is the value to pass as --home.
	Root string
}

// NewHome returns an empty fixture home with its config/ directory created,
// rooted in the test's temp directory.
func NewHome(t testing.TB) *Home {
	t.Helper()
	root := filepath.Join(t.TempDir(), "node")
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o750); err != nil {
		t.Fatalf("create fixture home: %v", err)
	}
	return &Home{Root: root}
}

// ConfigDir returns the home's config directory.
func (h *Home) ConfigDir() string { return filepath.Join(h.Root, "config") }

// ConfigTOMLPath returns the path of config.toml, whether or not it exists.
func (h *Home) ConfigTOMLPath() string { return filepath.Join(h.ConfigDir(), "config.toml") }

// AppTOMLPath returns the path of app.toml, whether or not it exists.
func (h *Home) AppTOMLPath() string { return filepath.Join(h.ConfigDir(), "app.toml") }

// ClientTOMLPath returns the path of client.toml, whether or not it exists.
func (h *Home) ClientTOMLPath() string { return filepath.Join(h.ConfigDir(), "client.toml") }

// WriteConfigTOML writes config.toml with the given contents. Passing arbitrary
// (including invalid) bytes is the point: the read path's behavior on a malformed
// file is part of the contract being pinned.
func (h *Home) WriteConfigTOML(t testing.TB, contents []byte) {
	t.Helper()
	h.write(t, h.ConfigTOMLPath(), contents)
}

// WriteAppTOML writes app.toml with the given contents.
func (h *Home) WriteAppTOML(t testing.TB, contents []byte) {
	t.Helper()
	h.write(t, h.AppTOMLPath(), contents)
}

// WriteClientTOML writes client.toml with the given contents.
func (h *Home) WriteClientTOML(t testing.TB, contents []byte) {
	t.Helper()
	h.write(t, h.ClientTOMLPath(), contents)
}

func (h *Home) write(t testing.TB, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// Read returns the contents of a file under config/, and whether it exists. It
// is how a test observes materialization: which files a read path created, and
// what it put in them.
func (h *Home) Read(t testing.TB, name string) ([]byte, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.ConfigDir(), name)) //nolint:gosec // fixture path under the test's temp dir
	switch {
	case os.IsNotExist(err):
		return nil, false
	case err != nil:
		t.Fatalf("read %s: %v", name, err)
	}
	return b, true
}

// Exists reports whether a file exists under config/.
func (h *Home) Exists(name string) bool {
	_, err := os.Stat(filepath.Join(h.ConfigDir(), name))
	return err == nil
}
