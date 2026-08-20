package sei_test

import (
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLinkConstraintsSelectOneFile asserts that every build configuration selects exactly
// one cgo link directive in each of linkDirs, and that the archive it names matches the
// target architecture.
func TestLinkConstraintsSelectOneFile(t *testing.T) {
	// wantCount is how many link_*.go files the toolchain selects: 0 leaves the linker
	// with no archive and undefined references, 2 puts two -l directives on one line and
	// the archives collide. wantFile pins which file, catching a swap that keeps the
	// count at 1.
	for _, p := range []struct {
		goos, goarch string
		tags         []string
		wantCount    int
		wantFile     string
		why          string
	}{
		{"linux", "amd64", []string{"muslc"}, 1, "link_muslc.go", "static musl release build, amd64"},
		{"linux", "arm64", []string{"muslc"}, 1, "link_muslc_aarch64.go", "static musl release build, arm64"},
		{"linux", "amd64", nil, 1, "link_glibclinux_x86_64.go", "ordinary dynamic build, amd64"},
		{"linux", "arm64", nil, 1, "link_glibclinux_aarch64.go", "ordinary dynamic build, arm64 (the Docker image)"},
		{"linux", "amd64", []string{"muslc", "sys_wasmvm"}, 1, "link_system.go", "system-libs escape hatch"},
		{"linux", "arm64", []string{"muslc", "sys_wasmvm"}, 1, "link_system.go", "system-libs escape hatch, arm64"},
		{"darwin", "arm64", nil, 1, "link_mac.go", "local development on Apple Silicon"},
		{"darwin", "amd64", nil, 1, "link_mac.go", "local development on Intel Mac"},
		{"windows", "amd64", nil, 1, "link_windows.go", "windows"},

		// No archive of either flavour is vendored for the remaining linux
		// architectures, so selecting nothing is correct and matches the glibc path.
		// seid cannot build for them regardless: giga/executor/lib rejects them at
		// compile time.
		{"linux", "riscv64", []string{"muslc"}, 0, "", "unsupported arch, must select nothing"},
	} {
		name := p.goos + "_" + p.goarch
		for _, tag := range p.tags {
			name += "_" + tag
		}
		t.Run(name, func(t *testing.T) {
			for _, dir := range linkDirs {
				t.Run(dir, func(t *testing.T) {
					selected := selectedLinkFiles(t, dir, p.goos, p.goarch, p.tags)
					require.Lenf(t, selected, p.wantCount,
						"%s/%s tags=%v (%s): expected %d link file(s), got %d: %v\n"+
							"  0 means nothing links (undefined references at link time)\n"+
							"  2 means two -l directives on one link line (incompatible archives)",
						p.goos, p.goarch, p.tags, p.why, p.wantCount, len(selected), selected)

					if p.wantFile != "" {
						require.Equalf(t, p.wantFile, selected[0],
							"%s/%s tags=%v (%s): wrong link directive selected",
							p.goos, p.goarch, p.tags, p.why)
					}

					// Naming an archive that exists is already covered by
					// TestLinkDirectivesResolve, and that holds even if two directives
					// are swapped. Pin the architecture of the archive as well.
					if p.goos == "linux" && !slices.Contains(p.tags, "sys_wasmvm") && len(selected) == 1 {
						lib := ldflagLibrary(t, filepath.Join(dir, selected[0]))
						require.Equalf(t, p.goarch == "arm64", strings.Contains(lib, "aarch64"),
							"%s/%s tags=%v: %s links -l%s; an aarch64 archive must be used "+
								"exactly when building for arm64",
							p.goos, p.goarch, p.tags, selected[0], lib)
					}
				})
			}
		})
	}
}

// selectedLinkFiles returns the link_*.go files in dir that the Go toolchain compiles for
// the given platform.
func selectedLinkFiles(t *testing.T, dir, goos, goarch string, tags []string) []string {
	t.Helper()

	ctx := build.Default
	ctx.GOOS = goos
	ctx.GOARCH = goarch
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
