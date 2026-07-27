package utils_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// Every directory an operator names in app.toml — the state-commit directory, the
// state-store directory, the receipt-store directory — reaches the filesystem through
// this expansion. Two properties of it make a config value mean something other than
// what it says.
//
// A leading ~ is expanded against $HOME at open time, not at config-write time, so
// the same app.toml resolves to different directories under different service users.
// And the resolver creates the directory as a side effect of resolving it, so a typo
// in a path does not fail — it silently provisions an empty database somewhere
// nobody is looking, and the node comes up with no state.

// FuzzResolveAndCreateDirTildeExpansion pins the expansion rules: bare ~ becomes
// $HOME, a ~/ prefix is joined onto it, and a tilde anywhere else is an ordinary
// character rather than a home reference.
//
// The last rule is the one that surprises: "~alice/data" is not expanded (Go has no
// user-lookup here), so it resolves to a relative directory literally named "~alice"
// under the process working directory.
func FuzzResolveAndCreateDirTildeExpansion(f *testing.F) {
	f.Add("~")
	f.Add("~/sei")
	f.Add("~/sei/data")
	f.Add("~alice/data") // not a home reference
	f.Add("sei/data")    // plain relative
	f.Add("")            // empty: resolved, not created
	f.Add("./sei")
	f.Add("~~")

	f.Fuzz(func(t *testing.T, dir string) {
		// The resolver creates directories as a side effect, so the input has to be one
		// that cannot escape the fixture. An absolute path would be MkdirAll'd on the
		// real filesystem wherever it points, so absolute inputs are out of scope here
		// and TestResolveAndCreateDirCreatesATypo covers that case against a path built
		// under t.TempDir(). A tilde reference is safe because Isolate has repointed
		// $HOME at a scratch directory.
		if strings.ContainsRune(dir, 0) || len(dir) > 128 {
			return
		}
		if filepath.IsAbs(dir) {
			return
		}
		home := configtest.Isolate(t)
		// t.Chdir rather than os.Chdir: it restores the working directory on cleanup.
		// A bare os.Chdir into a t.TempDir leaves later tests in this package running
		// with a deleted CWD once the fixture is removed, which turns relative-path
		// resolution into an order-dependent failure.
		t.Chdir(t.TempDir())

		got, err := utils.ResolveAndCreateDir(dir)
		if err != nil {
			// A path the filesystem rejects is a legitimate failure; what matters is
			// that it is an error rather than a silent substitution.
			return
		}

		want := dir
		switch {
		case dir == "~":
			want = home
		case strings.HasPrefix(dir, "~/"):
			want = filepath.Join(home, dir[2:])
		}
		abs, absErr := filepath.Abs(want)
		if absErr != nil {
			t.Skipf("abs(%q): %v", want, absErr)
		}
		if got != abs {
			t.Fatalf("ResolveAndCreateDir(%q) = %q, want %q; only a bare ~ or a ~/ prefix is a "+
				"home reference", dir, got, abs)
		}

		// A non-empty path is created as a side effect of resolving it.
		if dir != "" {
			if info, statErr := os.Stat(got); statErr != nil || !info.IsDir() {
				t.Fatalf("ResolveAndCreateDir(%q) did not create %q: %v", dir, got, statErr)
			}
		}
	})
}

// TestResolveAndCreateDirFollowsHomeAtOpenTime pins the property that makes a
// tilde-bearing config value non-portable: the same string resolves differently when
// $HOME differs, because expansion happens when the database is opened rather than
// when the value was written.
//
// So a config file that is byte-identical across two nodes can put their state in
// different places, which is why generated config bytes are not a comparable artifact
// between managers.
func TestResolveAndCreateDirFollowsHomeAtOpenTime(t *testing.T) {
	first := configtest.Isolate(t)
	t.Chdir(t.TempDir())

	fromFirst, err := utils.ResolveAndCreateDir("~/sei-state")
	if err != nil {
		t.Fatalf("ResolveAndCreateDir: %v", err)
	}
	if !strings.HasPrefix(fromFirst, first) {
		t.Fatalf("~/sei-state resolved to %q, want it under %q", fromFirst, first)
	}

	// Same config value, different $HOME, different directory.
	second := t.TempDir()
	if err := os.Setenv("HOME", second); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	fromSecond, err := utils.ResolveAndCreateDir("~/sei-state")
	if err != nil {
		t.Fatalf("ResolveAndCreateDir: %v", err)
	}
	if fromSecond == fromFirst {
		t.Fatal("the tilde is no longer expanded against $HOME at open time; if the expansion " +
			"moved to config-write time, that changes where existing nodes look for state")
	}
	if !strings.HasPrefix(fromSecond, second) {
		t.Fatalf("~/sei-state resolved to %q, want it under %q", fromSecond, second)
	}
}

// TestResolveAndCreateDirCreatesATypo records the side effect that turns a
// mistyped directory into a silent empty database. Resolving a path nobody has ever
// used succeeds and provisions it, so the node opens an empty store rather than
// reporting that the configured directory does not exist.
func TestResolveAndCreateDirCreatesATypo(t *testing.T) {
	configtest.Isolate(t)
	work := t.TempDir()

	typo := filepath.Join(work, "sei-statee")
	if _, err := os.Stat(typo); !os.IsNotExist(err) {
		t.Fatalf("fixture path must not exist yet: %v", err)
	}

	got, err := utils.ResolveAndCreateDir(typo)
	if err != nil {
		t.Fatalf("ResolveAndCreateDir: %v", err)
	}
	info, err := os.Stat(got)
	if err != nil || !info.IsDir() {
		t.Fatalf("a path that did not exist must be created, got %v", err)
	}
	entries, err := os.ReadDir(got)
	if err != nil {
		t.Fatalf("read created dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the created directory must be empty, got %d entries", len(entries))
	}
}
