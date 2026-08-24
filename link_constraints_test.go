package sei_test

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// assertSelects asserts that dir selects exactly wantFile for the given build tags on the
// platform this test binary was built for, and that the archive it names is aarch64 only
// when wantAarch64 is set.
func assertSelects(t *testing.T, dir string, tags []string, wantFile string, wantAarch64 bool) {
	t.Helper()

	selected := selectedLinkFiles(t, dir, tags)
	require.Lenf(t, selected, 1,
		"%s tags=%v: expected exactly one link file, got %v\n"+
			"  0 means nothing links (undefined references at link time)\n"+
			"  2 means two -l directives on one link line (incompatible archives)",
		dir, tags, selected)
	require.Equalf(t, wantFile, selected[0], "%s tags=%v: wrong link directive", dir, tags)

	lib := ldflagLibrary(t, filepath.Join(dir, selected[0]))
	require.Equalf(t, wantAarch64, strings.Contains(lib, "aarch64"),
		"%s links -l%s", selected[0], lib)
}

// selectedLinkFiles returns the link_*.go files in dir that the Go toolchain compiles for
// the platform this test binary was built for.
func selectedLinkFiles(t *testing.T, dir string, tags []string) []string {
	t.Helper()

	ctx := build.Default
	ctx.BuildTags = tags

	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "read %s", dir)

	var selected []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "link_") || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		ok, err := ctx.MatchFile(dir, name)
		require.NoErrorf(t, err, "match %s", filepath.Join(dir, name))
		if ok {
			selected = append(selected, name)
		}
	}
	return selected
}

// ldflagLibrary returns the -l<name> argument from the cgo LDFLAGS directive in the file
// at path, for example "wasmvm155_muslc.aarch64".
func ldflagLibrary(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	m := reLDFlag.FindStringSubmatch(string(data))
	require.NotEmptyf(t, m, "no `#cgo LDFLAGS: ... -l<name>` directive in %s", path)
	return m[1]
}
