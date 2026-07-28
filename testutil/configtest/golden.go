package configtest

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -update rewrites the recorded defaults instead of comparing against them, so one
// invocation regenerates every section: `go test ./... -update`.
//
// Registered defensively. sei-tendermint/internal/p2p/conn already defines a flag of the
// same name for its own golden files, and a second registration in one binary panics at
// init, which would break any test binary that ever linked both. Reusing the existing flag
// is safe because the two mean the same thing, and reading it through Lookup at call time
// avoids caring which package won the registration.
func init() {
	if flag.Lookup("update") == nil {
		flag.Bool("update", false, "rewrite golden files with current values")
	}
}

// goldenUpdateRequested reports whether -update was passed, whoever registered it.
func goldenUpdateRequested() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

// CheckDefaults asserts that a section's in-code defaults still match the values recorded
// in testdata/<name>.golden.
//
// This is the anchor CheckAbsent cannot provide. CheckAbsent compares a reader's
// empty-input result against the package's own default struct, so a change to the reader's
// logic moves one side and is caught, while a change to a default's value moves both sides
// together and is not: that assertion holds for any default, which is exactly why it says
// nothing about which default is correct. The golden file is a second, independent
// recording, so moving a default fails here until someone regenerates it.
//
// Failing is the point, not a prohibition. A default should be able to change; what should
// not happen is it changing without the new value appearing in a diff. That is the same
// pinned-rather-than-fixed posture the rest of this suite takes, and it is the class of
// drift the config-manager work exists to end: a default moving on one side of the
// binary/renderer boundary while the other side keeps the old one.
//
// Regenerate with `go test ./<pkg>/ -update` once the change is deliberate.
func CheckDefaults(t testing.TB, name string, defaults any) {
	t.Helper()

	got := Dump(defaults)
	path := filepath.Join("testdata", name+".golden")

	if goldenUpdateRequested() {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatalf("%s: create testdata: %v", name, err)
		}
		// Written with a trailing newline: these files exist to be read in a review diff, and
		// without one every diff carries a "\ No newline at end of file" marker.
		if err := os.WriteFile(path, []byte(got+"\n"), 0o600); err != nil {
			t.Fatalf("%s: write %s: %v", name, path, err)
		}
		t.Logf("%s: rewrote %s", name, path)
		return
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: cannot read %s (%v).\nIf this section is new, create it with "+
			"`go test ./... -update` and review the recorded values as part of the change.",
			name, path, err)
	}
	// Tolerant of the trailing newline either way, so an editor that strips or adds one
	// does not read as a default having changed.
	want := strings.TrimRight(string(raw), "\n")
	got = strings.TrimRight(got, "\n")
	if got == want {
		return
	}
	t.Fatalf("%s: the in-code defaults no longer match %s.\n%s\n"+
		"A default changing is a change every node inherits without editing its app.toml, so it "+
		"is recorded rather than followed. If the new values are intended, regenerate with "+
		"`go test ./... -update` and keep the diff in the review.",
		name, path, goldenDiff(want, got))
}

// goldenDiff reports only the lines that differ, because a section can carry well over a
// hundred leaves and printing both dumps in full buries the one value that moved.
func goldenDiff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	index := func(lines []string) map[string]string {
		m := make(map[string]string, len(lines))
		for _, l := range lines {
			if k, v, ok := strings.Cut(l, " = "); ok {
				m[k] = v
			}
		}
		return m
	}
	wi, gi := index(wl), index(gl)

	var b strings.Builder
	for _, l := range wl {
		k, wv, ok := strings.Cut(l, " = ")
		if !ok {
			continue
		}
		if gv, present := gi[k]; !present {
			b.WriteString("  removed: " + k + " = " + wv + "\n")
		} else if gv != wv {
			b.WriteString("  changed: " + k + "\n      was: " + wv + "\n      now: " + gv + "\n")
		}
	}
	for _, l := range gl {
		if k, gv, ok := strings.Cut(l, " = "); ok {
			if _, present := wi[k]; !present {
				b.WriteString("    added: " + k + " = " + gv + "\n")
			}
		}
	}
	if b.Len() == 0 {
		return "  (the leaf values agree; the difference is in ordering or formatting)"
	}
	return b.String()
}
