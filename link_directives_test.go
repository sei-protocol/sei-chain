package sei_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// linkDirs are the directories whose link_*.go cgo directives must
// point at real on-disk library artifacts vendored next to them.
var linkDirs = []string{
	"sei-wasmd/x/wasm/artifacts/v152/api",
	"sei-wasmd/x/wasm/artifacts/v155/api",
	"sei-wasmvm/internal/api",
}

// reLDFlag captures the -l<name> argument from a cgo LDFLAGS line, e.g.
//
//	// #cgo LDFLAGS: -Wl,-rpath,${SRCDIR} -L${SRCDIR} -lwasmvm155_muslc
//
// yields "wasmvm155_muslc".
var reLDFlag = regexp.MustCompile(`#cgo LDFLAGS:.*?-l(\S+)`)

// linkableExts is the set of extensions cgo's linker will accept when
// resolving -l<name> — it searches for lib<name>.{so,a,dylib} (plus a few
// platform variants we don't need to enumerate here).
var linkableExts = []string{".a", ".so", ".dylib"}

// TestLinkDirectivesResolve asserts that every cgo LDFLAGS -l<name>
// directive across the vendored libwasmvm api packages points at a real
// library file checked into the same directory.
//
// This catches two symmetric failure modes:
//
//  1. A link_*.go references -l<name> but no lib<name>.{a,so,dylib}
//     exists in $SRCDIR. This is what surfaced in PLT-41 once the
//     static-build path was first exercised: link_muslc.go declared
//     -lwasmvm155_muslc but no libwasmvm155_muslc.a was checked in,
//     producing `cannot find -lwasmvm155_muslc` at link time.
//
//  2. An artifact is checked in under a name no link directive resolves
//     against — orphaned files that look authoritative but aren't wired
//     to anything. The previous libwasmvm155static.a (Mach-O arm64) was
//     a textbook example: present in the tree, consumed by nothing.
//
// The 1 KiB floor on file size is a sanity gate against the failure
// mode where a download produced an HTML error page or an LFS pointer
// (~100–200 bytes) instead of a real archive.
func TestLinkDirectivesResolve(t *testing.T) {
	for _, dir := range linkDirs {
		dir := dir
		gofiles, err := filepath.Glob(filepath.Join(dir, "link_*.go"))
		require.NoError(t, err, "glob link_*.go in %s", dir)
		require.NotEmpty(t, gofiles, "expected link_*.go in %s", dir)

		for _, gofile := range gofiles {
			gofile := gofile
			t.Run(gofile, func(t *testing.T) {
				data, err := os.ReadFile(gofile)
				require.NoError(t, err)

				m := reLDFlag.FindStringSubmatch(string(data))
				require.NotEmptyf(t, m,
					"no `#cgo LDFLAGS: ... -l<name>` directive in %s", gofile)
				libName := m[1]

				var found string
				for _, ext := range linkableExts {
					path := filepath.Join(dir, "lib"+libName+ext)
					if info, err := os.Stat(path); err == nil && info.Size() > 1024 {
						found = path
						break
					}
				}
				require.NotEmptyf(t, found,
					"%s declares -l%s but no lib%s.{a,so,dylib} (>1KiB) "+
						"exists in %s — checked-in artifact is missing, "+
						"empty, or named inconsistently with the linker directive",
					gofile, libName, libName, dir)
				t.Logf("ok: %s -> %s", gofile, found)
			})
		}
	}
}

// TestArtifactsHaveNoOrphans asserts the inverse direction: every
// library file in a linkDir corresponds to *something* a link_*.go
// might consume. Catches the "checked in but never used" class — files
// like the original libwasmvm155static.a that were never resolved by any
// directive and sat dormant until someone tried to static-link.
//
// Allowed: lib<name>.{a,so,dylib} where some link_*.go in the same dir
// references -l<name> OR -l<name-without-arch-suffix>. Arch-suffixed
// siblings (lib<base>_muslc.aarch64.a alongside lib<base>_muslc.a) are
// permitted because they're swapped in by build-time tooling, not by a
// distinct cgo directive.
func TestArtifactsHaveNoOrphans(t *testing.T) {
	for _, dir := range linkDirs {
		dir := dir
		t.Run(dir, func(t *testing.T) {
			// Collect referenced library base names from every link_*.go.
			referenced := map[string]bool{}
			gofiles, _ := filepath.Glob(filepath.Join(dir, "link_*.go"))
			for _, gofile := range gofiles {
				data, _ := os.ReadFile(gofile)
				if m := reLDFlag.FindStringSubmatch(string(data)); len(m) > 1 {
					referenced[m[1]] = true
				}
			}

			// For every lib*.{a,so,dylib} on disk, the basename (with the
			// arch suffix stripped) must match a referenced -l<name>.
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			for _, e := range entries {
				name := e.Name()
				ext := filepath.Ext(name)
				isLib := false
				for _, allowed := range linkableExts {
					if ext == allowed {
						isLib = true
						break
					}
				}
				if !isLib {
					continue
				}
				base := name[len("lib") : len(name)-len(ext)]
				// Strip any .x86_64 / .aarch64 arch suffix.
				stripped := base
				for _, s := range []string{".x86_64", ".aarch64"} {
					stripped = stripSuffix(stripped, s)
				}
				if !referenced[base] && !referenced[stripped] {
					t.Errorf("orphan artifact %s/%s — no link_*.go references "+
						"-l%s (or -l%s); either delete the file or wire it up",
						dir, name, base, stripped)
				}
			}
		})
	}
}

func stripSuffix(s, suf string) string {
	if len(s) >= len(suf) && s[len(s)-len(suf):] == suf {
		return s[:len(s)-len(suf)]
	}
	return s
}
