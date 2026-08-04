package configtest

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// -update rewrites the recorded defaults instead of comparing against them.
//
// Invoked per package, not tree-wide: the flag is only registered in binaries that link this
// helper, so `go test ./... -update` makes every other package exit with "flag provided but
// not defined". Regenerating several sections means naming them, or passing the list of
// packages that carry goldens.
//
// Registered defensively. sei-tendermint/internal/p2p/conn already defines a flag of the
// same name for its own golden files, and a second registration in one binary panics at
// init, which would break any test binary that ever linked both. Reusing the existing flag
// is safe because the two mean the same thing, and reading it through Lookup at call time
// avoids caring which package won the registration.
//
// Registering from a library file is the wrong shape in general, since importing a package
// should not install a process-global flag. It stays here because it cannot move: the only
// consumer is CheckDefaults, which is exported for other packages' tests and therefore has to
// live in a non-test file, and a non-test file cannot reference a symbol declared in a
// _test.go one. Only test binaries import this package, so the exposure is bounded.
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

// DerivedDefault names a default that is computed from the machine rather than written
// down, together with the value it must equal.
//
// Such a field cannot sit in a golden file as a literal: runtime.NumCPU() makes the recorded
// value true only of the machine that generated it, so a ten-core laptop would record 20
// where a four-core runner produces 8. Recording it would turn every CI run into a false
// report of a changed default, which is the opposite of what these files are for.
//
// It is masked rather than dropped, and the formula is asserted in its place. So the field
// stays covered, just against its derivation instead of against a constant: the golden holds
// a stable marker, and Want carries the expression the value has to match. A change to the
// formula fails, a change to the machine does not, and the field disappearing still shows up
// as a removed line.
type DerivedDefault struct {
	// Path is the Dump path of the field, e.g. "WorkerPoolSize".
	Path string
	// Want is the value the derivation must currently produce, computed the same way the
	// production code computes it.
	Want any
	// Why records the derivation in the golden file itself, so a reader sees what the field
	// depends on without going to the source.
	Why string
}

// goldenFilePath returns the path of a section's recorded file, rejecting anything that could
// steer it out of testdata.
//
// The whole file name is checked, not just the section name. A section name is written at the
// call site and names a TOML section, and the suffix selects which of a section's two records
// is wanted; both are joined before validation, because a check on one half says nothing about
// a string the other half appends to it. The gosec suppressions on the reads rest on this, so
// what it has to establish is that the only reachable path is testdata/<name><suffix> with no
// separator in either half — which is also what the rejection message claims, where a bare
// filepath.IsLocal would accept a/b and ./app.
func goldenFilePath(t testing.TB, name, suffix string) string {
	t.Helper()
	file := name + suffix
	if name == "" || strings.ContainsAny(file, `/\`) || !filepath.IsLocal(file) {
		t.Fatalf("section name %q and suffix %q must compose a bare file name with no path "+
			"separator, so that a golden file is read and written inside testdata and nowhere else",
			name, suffix)
	}
	return filepath.Join("testdata", file)
}

// recordText normalizes a golden file's bytes for comparison.
//
// Two edits reach these files without anyone intending a change to what they record: an editor
// adding or stripping the trailing newline, and a checkout rewriting every line ending to CRLF.
// Trimming the trailing run alone covers the first and not the second, because a CRLF file
// keeps a \r at the end of every interior line — and a key-name record quotes the manifest's
// key rather than the record's line, so a stray \r falls outside the quotes and renders
// identically to the correct spelling. A CRLF checkout of one real record reported three keys
// removed and the same three added, visually indistinguishable.
func recordText(raw []byte) string {
	return strings.TrimRight(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r\n")
}

// writeGolden records content at path, for the -update branch of a golden check.
//
// path is goldenFilePath's result, which is where the confinement to testdata is established.
// The directory is created from the path rather than from a literal, so the two cannot disagree
// about where the file goes.
func writeGolden(t testing.TB, name, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("%s: create %s: %v", name, dir, err)
	}
	// Written with a trailing newline: these files exist to be read in a review diff, and
	// without one every diff carries a "\ No newline at end of file" marker.
	if err := os.WriteFile(path, []byte(content+"\n"), 0o600); err != nil {
		t.Fatalf("%s: write %s: %v", name, path, err)
	}
	t.Logf("%s: rewrote %s", name, path)
}

// CheckDefaults asserts that a section's in-code defaults still match the values recorded
// in testdata/<section>.golden, with any machine-derived fields checked against their formula
// instead.
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
// Regenerate with `go test ./<pkg>/ -run TestDefaultsMatchTheRecordedValues -update` once the
// change is deliberate. The filter is part of the instruction, not tidiness: -update is one
// process-global flag and every golden check in the package obeys it, so an unfiltered run also
// rewrites the section's key-name record and absorbs a pending rename into a defaults commit.
func CheckDefaults(t testing.TB, name string, defaults any, derived ...DerivedDefault) {
	t.Helper()

	path := goldenFilePath(t, name, ".golden")
	got := Dump(defaults)

	// Machine-derived fields are verified against their derivation, then replaced by a stable
	// marker so the golden means the same thing on every machine.
	for _, d := range derived {
		leaf, ok := leafAt(got, d.Path)
		if !ok {
			t.Fatalf("%s: %q is declared machine-derived but is not present in the resolved "+
				"defaults. If the field was removed, drop it from the derived list; if it was "+
				"renamed, update the path", name, d.Path)
		}
		if want := DumpAt(d.Path, d.Want); leaf != want {
			t.Fatalf("%s: %s no longer matches its derivation (%s)\n got: %s\nwant: %s\n"+
				"This field is computed rather than written down, so the assertion is on the "+
				"formula. Either the formula changed, in which case update it here and say so, "+
				"or the field stopped being derived and belongs in the golden as a literal",
				name, d.Path, d.Why, leaf, want)
		}
		marker := d.Path + " = <derived: " + d.Why + ">"
		spliced, ok := spliceLeaf(got, d.Path, marker)
		if !ok {
			t.Fatalf("%s: could not mask %q in the resolved defaults", name, d.Path)
		}
		got = spliced
	}

	if goldenUpdateRequested() {
		writeGolden(t, name, path, got)
		return
	}

	raw, err := os.ReadFile(path) //nolint:gosec // testdata/<section>.golden; the whole file name is validated by goldenFilePath
	if err != nil {
		t.Fatalf("%s: cannot read %s (%v).\nIf this section is new, create it with "+
			"`go test ./<pkg>/ -run TestDefaultsMatchTheRecordedValues -update` and review the "+
			"recorded values as part of the change. The filter keeps the same run from also "+
			"rewriting the section's key-name record.", name, path, err)
	}
	// Compared as text rather than as bytes, so a stripped trailing newline or a CRLF checkout
	// does not read as a default having changed. recordText says why the trailing run is not
	// enough on its own.
	want := recordText(raw)
	got = strings.TrimRight(got, "\n")
	if got == want {
		return
	}
	t.Fatalf("%s: the in-code defaults no longer match %s.\n%s\n"+
		"A default changing is a change every node inherits without editing its app.toml, so it "+
		"is recorded rather than followed. If the new values are intended, regenerate with "+
		"`go test ./<pkg>/ -run TestDefaultsMatchTheRecordedValues -update` and keep the diff in "+
		"the review. The filter keeps the same run from also rewriting the key-name record.",
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

// CheckManifestCoversEveryField asserts that every field in a section's resolved view is
// named by a manifest row, or is explicitly listed as not read from configuration.
//
// A section's manifest carries the claim that it lists every key the reader looks up, and
// that claim is what a replacement implementation will treat as the contract. Left as prose
// it is unenforced: a key can be added to the reader, rendered into app.toml, and consumed by
// a node while the table that is supposed to enumerate it stays silent. The reader and the
// table then disagree, and the table is the one being trusted.
//
// The comparison is on fields rather than on key strings, because the fields are what Dump
// can enumerate without parsing the reader. That makes it a completeness check on the
// section's struct: a field with no row and no exemption fails, which catches both a key the
// manifest forgot and a field added later.
//
// coveredElsewhere lists Dump paths the table deliberately does not name: a field whose key is
// driven by a dedicated target in the same file, or one that carries no configuration key at
// all. Naming it here makes the exclusion a statement someone wrote, which is the difference
// between a decision and an omission. Each entry should carry a comment saying which it is.
func CheckManifestCoversEveryField(t testing.TB, name string, defaults any, specs []KeySpec, coveredElsewhere ...string) {
	t.Helper()

	covered := make(map[string]bool, len(specs))
	for _, s := range specs {
		covered[s.Path] = true
		for _, p := range s.AlsoWrites {
			covered[p] = true
		}
	}
	exempt := make(map[string]bool, len(coveredElsewhere))
	for _, p := range coveredElsewhere {
		exempt[p] = true
	}

	var missing []string
	for _, line := range strings.Split(Dump(defaults), "\n") {
		path, _, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		// An indexed leaf belongs to its parent field, which is what a row names.
		if i := strings.IndexByte(path, '['); i >= 0 {
			path = path[:i]
		}
		if covered[path] || exempt[path] {
			continue
		}
		missing = append(missing, path)
	}
	missing = dedupeSorted(missing)
	if len(missing) == 0 {
		return
	}
	t.Fatalf("%s: the manifest claims to name every key the reader looks up, and these fields "+
		"have no row and no exemption:\n  %s\n"+
		"Add a KeySpec for each key the reader resolves into them, or pass the path as "+
		"coveredElsewhere with a comment saying where. A field that is read but unlisted is the "+
		"case this check exists for: the manifest is what a replacement implementation treats "+
		"as the contract, so a silent omission tells its authors the key does not exist.",
		name, strings.Join(missing, "\n  "))
}

// dedupeSorted returns the distinct entries of s in sorted order.
func dedupeSorted(s []string) []string {
	seen := make(map[string]bool, len(s))
	var out []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
