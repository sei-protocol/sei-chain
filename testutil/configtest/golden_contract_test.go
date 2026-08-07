package configtest

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGoldenFilePathConfinesBothHalvesOfTheFileName pins what the //nolint:gosec suppressions on
// the two golden reads rest on.
//
// Each suppression says the path it reads is testdata/<section><suffix> and nothing else, and
// what makes that true is this function refusing everything else. Both halves are checked
// because both are joined: a validated section name with an unvalidated suffix appended to it is
// not a validated path, and `goldenFilePath(t, "app", "/../../../../../../etc/crontab")` used to
// return a path outside the repository. No call site can reach that today — the suffix is a
// constant at both of them — which is the reason to hold it here rather than in an argument about
// reachability that the next call site invalidates.
func TestGoldenFilePathConfinesBothHalvesOfTheFileName(t *testing.T) {
	for _, accepted := range []struct{ name, suffix string }{
		{"app", ".golden"},
		{"state-commit", keyNameRecordSuffix},
		{"light_invariance", keyNameRecordSuffix},
	} {
		want := filepath.Join("testdata", accepted.name+accepted.suffix)
		if got := goldenFilePath(t, accepted.name, accepted.suffix); got != want {
			t.Errorf("goldenFilePath(%q, %q) = %q, want %q", accepted.name, accepted.suffix, got, want)
		}
	}

	for _, rejected := range []struct{ what, name, suffix string }{
		{"a traversal in the suffix", "app", "/../../../../../../etc/crontab"},
		{"a traversal in the name", "../../etc/crontab", ".golden"},
		{"a separator in the name", "app/nested", ".golden"},
		{"a relative prefix", "./app", ".golden"},
		{"an absolute name", "/etc/crontab", ".golden"},
		{"the parent directory", "..", ""},
		{"no name at all", "", ".golden"},
	} {
		reported := capture(t, func(tb testing.TB) { goldenFilePath(tb, rejected.name, rejected.suffix) })
		if !reported.fatal {
			t.Errorf("%s: goldenFilePath(%q, %q) returned instead of giving up, so a golden read or "+
				"write could leave testdata", rejected.what, rejected.name, rejected.suffix)
			continue
		}
		if msg := reported.only(t); !strings.Contains(msg, "testdata") {
			t.Errorf("%s: the rejection does not say where a golden file may live:\n%s",
				rejected.what, msg)
		}
	}
}
