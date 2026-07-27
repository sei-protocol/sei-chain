package configtest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cast"
)

// CastKind names the spf13/cast conversion a read site applies to a raw value.
// Every section reader in the legacy path has the same shape — look the key up in
// appOpts, then cast it — so naming the cast is enough to predict the resolved
// value from the raw one, which is what lets one engine check every row.
type CastKind int

const (
	CastBool CastKind = iota
	CastString
	CastInt
	CastInt64
	CastUint
	CastUint32
	CastUint64
	CastFloat64
	CastDuration
	CastStringSlice
	CastIntSlice
)

func (k CastKind) String() string {
	switch k {
	case CastBool:
		return "bool"
	case CastString:
		return "string"
	case CastInt:
		return "int"
	case CastInt64:
		return "int64"
	case CastUint:
		return "uint"
	case CastUint32:
		return "uint32"
	case CastUint64:
		return "uint64"
	case CastFloat64:
		return "float64"
	case CastDuration:
		return "time.Duration"
	case CastStringSlice:
		return "[]string"
	case CastIntSlice:
		return "[]int"
	default:
		return fmt.Sprintf("CastKind(%d)", int(k))
	}
}

// cast applies the checked (…E) conversion.
//
// One conversion serves both checked and unchecked readers, which rests on a
// property of spf13/cast: the unchecked ToX is defined as `v, _ := ToXE(i);
// return v`, so the two always agree on the value and differ only in whether the
// error surfaces. That is what lets CheckKey predict an unchecked reader's field
// from the checked conversion. A cast release that broke the equivalence would
// invalidate the engine rather than fail one row, so it is named here.
func (k CastKind) cast(v any) (any, error) {
	switch k {
	case CastBool:
		return cast.ToBoolE(v)
	case CastString:
		return cast.ToStringE(v)
	case CastInt:
		return cast.ToIntE(v)
	case CastInt64:
		return cast.ToInt64E(v)
	case CastUint:
		return cast.ToUintE(v)
	case CastUint32:
		return cast.ToUint32E(v)
	case CastUint64:
		return cast.ToUint64E(v)
	case CastFloat64:
		return cast.ToFloat64E(v)
	case CastDuration:
		return cast.ToDurationE(v)
	case CastStringSlice:
		return cast.ToStringSliceE(v)
	case CastIntSlice:
		return cast.ToIntSliceE(v)
	default:
		return nil, fmt.Errorf("unknown cast kind %d", int(k))
	}
}

// zero is the value an unchecked cast yields when the conversion fails, and the
// value an unguarded read therefore writes over a non-zero in-code default.
func (k CastKind) zero() any {
	switch k {
	case CastBool:
		return false
	case CastString:
		return ""
	case CastInt:
		return int(0)
	case CastInt64:
		return int64(0)
	case CastUint:
		return uint(0)
	case CastUint32:
		return uint32(0)
	case CastUint64:
		return uint64(0)
	case CastFloat64:
		return float64(0)
	case CastDuration:
		return time.Duration(0)
	case CastStringSlice:
		return []string(nil)
	case CastIntSlice:
		return []int(nil)
	default:
		return nil
	}
}

// KeySpec is one row of the legacy configuration read-site manifest, made
// executable: a key, the field it resolves into, and the semantics the reader
// applies on the way. A suite that enumerates every row is a suite that pins
// every row, which is the whole point of writing the manifest down as data
// instead of as a comment.
type KeySpec struct {
	// Key is the dotted path the reader passes to appOpts.Get.
	Key string

	// Path is the Dump path of the struct field the value lands in, e.g.
	// "MemIAVLConfig.AsyncCommitBuffer".
	Path string

	// Cast is the conversion the reader applies.
	Cast CastKind

	// Unguarded marks a read written as `field = cast.ToX(opts.Get(k))` with no
	// `v != nil` check. Such a read cannot distinguish an absent key from a nil
	// one and writes the zero value over the in-code default in both cases. That
	// is a documented sharp edge of the legacy path, not a defect this suite
	// fixes; recording it here is what makes the difference between "pinned" and
	// "accidentally still true".
	Unguarded bool

	// Checked marks a read that propagates a conversion failure as an error
	// (cast.ToXE). When false the reader swallows the failure and keeps the zero
	// value, so a malformed app.toml entry is silently inert.
	Checked bool

	// AlsoWrites lists any further Dump paths the reader touches when this key is
	// present, beyond Path.
	//
	// The row assertion compares the whole resolved document, not just the line at
	// Path, so a reader that lands the nominated key correctly and also perturbs a
	// field it never declared fails. A reader that legitimately writes more than one
	// field from one key declares the rest here, which keeps the fan-out recorded in
	// the manifest rather than absorbed by a weaker assertion. The listed paths are
	// exempted from the comparison, so this is a statement that a field is expected
	// to move, not a prediction of its value.
	AlsoWrites []string

	// Why records what the row is protecting, for the failure message. Rows whose
	// behavior is unremarkable can leave it empty.
	Why string
}

// CheckAbsent asserts that reading with no keys present yields exactly the
// package's declared defaults.
//
// This is the load-bearing property of the whole appOpts surface. Every section
// reader starts from an in-code default struct and overwrites fields it finds
// keys for, so a node whose app.toml predates a key must resolve that key to its
// default. When a guard is dropped the default is replaced by the zero value
// instead, and the node quietly downgrades: ss-keep-recent 0 means keep
// everything and grow without bound, sc-async-commit-buffer 0 means commit
// synchronously. Neither failure is visible at boot.
func CheckAbsent(t testing.TB, name string, read func(AppOpts) (any, error), want any) {
	t.Helper()
	got, err := read(AppOpts{})
	if err != nil {
		t.Fatalf("%s: reading an empty AppOpts must succeed, got %v", name, err)
	}
	if gotDump, wantDump := Dump(got), Dump(want); gotDump != wantDump {
		t.Fatalf("%s: an empty AppOpts must resolve to the in-code defaults\n--- got\n%s\n--- want\n%s",
			name, gotDump, wantDump)
	}
}

// CheckKey asserts that one key carrying one raw value resolves the way the
// reader's cast and guard say it must. It is the per-row body of the fuzz
// targets: the fuzzer supplies value, the KeySpec supplies the prediction.
//
// The comparison for an absent-equivalent value is against the reader's own
// empty-AppOpts result rather than the package's default struct, because some
// readers fill fields from outside the config (parseSCConfigs stamps the build
// version into the hash-logger config). CheckAbsent pins that empty result
// against the declared defaults separately, so nothing goes unchecked.
func CheckKey(t testing.TB, name string, read func(AppOpts) (any, error), spec KeySpec, value any) {
	t.Helper()

	baseline, baselineErr := read(AppOpts{})
	if baselineErr != nil {
		t.Fatalf("%s: reading an empty AppOpts must succeed, got %v", name, baselineErr)
	}

	cfg, err := read(AppOpts{spec.Key: value})

	// A nil value is indistinguishable from an absent key through the
	// appOpts.Get interface, so a guarded read keeps the default and an unguarded
	// one clobbers it with the cast's zero.
	if value == nil {
		if err != nil {
			t.Fatalf("%s: %s present with a nil value must not error, got %v", name, spec.Key, err)
		}
		want := spec.Path + " = " + leafOf(spec.Path, spec.Cast.zero())
		if spec.Unguarded {
			assertResolvedView(t, name, spec, baseline, cfg, want)
			return
		}
		if gotDump, wantDump := Dump(cfg), Dump(baseline); gotDump != wantDump {
			t.Fatalf("%s: %s present with a nil value must resolve exactly as an absent key does (%s)\n--- got\n%s\n--- want\n%s",
				name, spec.Key, spec.Why, gotDump, wantDump)
		}
		return
	}

	converted, castErr := spec.Cast.cast(value)

	if castErr != nil && spec.Checked {
		if err == nil {
			t.Fatalf("%s: %s = %#v does not convert to %s (%v), so the read must return an error",
				name, spec.Key, value, spec.Cast, castErr)
		}
		return
	}
	if err != nil {
		// The wording branches on castErr because this line is also reached when the
		// conversion failed on an unchecked row: the checked branch above returns early only
		// for spec.Checked, so saying "converts cleanly" there would point a future reader at
		// the wrong half of the row.
		if castErr != nil {
			t.Fatalf("%s: %s = %#v does not convert to %s (%v), and the read is unchecked, so it "+
				"must swallow the failure and keep the zero value rather than error, got %v",
				name, spec.Key, value, spec.Cast, castErr, err)
		}
		t.Fatalf("%s: %s = %#v converts to %s cleanly, so the read must succeed, got %v",
			name, spec.Key, value, spec.Cast, err)
	}
	assertResolvedView(t, name, spec, baseline, cfg, DumpAt(spec.Path, converted))
}

// assertResolvedView compares the whole resolved document against the baseline with the
// expected leaf spliced in.
//
// Asserting only the line at spec.Path would pass a reader that resolves the nominated key
// correctly and also perturbs an unrelated field, and that is the failure mode a
// characterization oracle exists to catch: the manifest's claim is that one key moves one
// field, so the assertion has to cover the fields it says do not move. Splicing rather than
// predicting the whole document keeps the row's prediction to the one leaf it owns.
//
// Paths in spec.AlsoWrites are dropped from both sides, so a reader with declared fan-out
// still gets every undeclared field checked.
func assertResolvedView(t testing.TB, name string, spec KeySpec, baseline, cfg any, want string) {
	t.Helper()

	got, ok := leafAt(Dump(cfg), spec.Path)
	if !ok {
		t.Fatalf("%s: %s claims to resolve into field %q, which is not present in the resolved view:\n%s",
			name, spec.Key, spec.Path, Dump(cfg))
	}
	if got != want {
		t.Fatalf("%s: %s resolved into the wrong value\n got: %s\nwant: %s", name, spec.Key, got, want)
	}

	expected, spliced := spliceLeaf(Dump(baseline), spec.Path, want)
	if !spliced {
		t.Fatalf("%s: field %q is absent from the baseline view, so the row cannot say which "+
			"fields are supposed to stay put:\n%s", name, spec.Path, Dump(baseline))
	}
	actual := Dump(cfg)
	for _, path := range spec.AlsoWrites {
		expected = dropLeaf(expected, path)
		actual = dropLeaf(actual, path)
	}
	if actual != expected {
		t.Fatalf("%s: %s resolved its own field correctly but also moved a field it does not "+
			"declare. One key writes one field unless the row says otherwise, so either the "+
			"reader changed or the row needs the extra path in AlsoWrites.\n--- got\n%s\n--- want\n%s",
			name, spec.Key, actual, expected)
	}
}

// spliceLeaf replaces the lines describing path in dump with replacement, reporting
// whether the path was found. A composite field spans several lines, so the whole run is
// replaced at the position of its first line.
func spliceLeaf(dump, path, replacement string) (string, bool) {
	var out []string
	found := false
	for _, line := range strings.Split(dump, "\n") {
		if !isLeafLine(line, path) {
			out = append(out, line)
			continue
		}
		if !found {
			out = append(out, strings.Split(replacement, "\n")...)
			found = true
		}
	}
	return strings.Join(out, "\n"), found
}

// dropLeaf removes the lines describing path, for fields a row declares as expected to
// move without predicting their value.
func dropLeaf(dump, path string) string {
	var out []string
	for _, line := range strings.Split(dump, "\n") {
		if !isLeafLine(line, path) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// LeafAt pulls the lines describing one field out of a rendered view, so a test
// can assert on a single resolved value instead of a whole document. It returns
// false when the path names nothing in the view.
func LeafAt(dump, path string) (string, bool) { return leafAt(dump, path) }

// leafAt pulls the single dump line describing path out of a rendered view. A
// composite field (a slice, say) renders as several indexed lines, so the match
// is on the path prefix and the lines are rejoined.
func leafAt(dump, path string) (string, bool) {
	var matched []string
	for _, line := range strings.Split(dump, "\n") {
		if isLeafLine(line, path) {
			matched = append(matched, line)
		}
	}
	if len(matched) == 0 {
		return "", false
	}
	return strings.Join(matched, "\n"), true
}

// isLeafLine reports whether a rendered line describes path. It is shared by every
// path-scoped operation so that reading, splicing and dropping a field cannot disagree
// about which lines belong to it.
func isLeafLine(line, path string) bool {
	return line == path+" = <nil>" || strings.HasPrefix(line, path+" = ") ||
		strings.HasPrefix(line, path+"[")
}

// leafOf renders a bare value the way Dump renders it at path, minus the path.
func leafOf(path string, v any) string {
	rendered := DumpAt(path, v)
	return strings.TrimPrefix(rendered, path+" = ")
}

// Pick selects a manifest row from a fuzzer-supplied index. Reducing modulo the
// table length keeps every generated index on a real row instead of collapsing
// out-of-range values onto row zero.
func Pick(specs []KeySpec, idx uint) KeySpec {
	// An empty table makes the modulo a divide-by-zero, and that panic would point at the
	// harness rather than at the manifest that lost its rows.
	if len(specs) == 0 {
		panic("configtest.Pick: empty KeySpec table, the section's manifest has no rows")
	}
	return specs[idx%uint(len(specs))]
}

// CheckRow runs the full property set for one manifest row carrying one raw
// value. It is the body of every per-section fuzz target, so a section's suite is
// its manifest plus its seeds and nothing else.
func CheckRow(t testing.TB, name string, read func(AppOpts) (any, error), spec KeySpec, value any) {
	t.Helper()
	CheckKey(t, name, read, spec, value)
	CheckDeterministic(t, name, read, AppOpts{spec.Key: value})
}

// CheckDeterministic asserts a reader is a pure function of its AppOpts: two
// reads of the same input agree on both the value and the error. Randomized map
// iteration inside a reader, a cached package-level value, or a default struct
// shared by pointer rather than copied all show up here.
func CheckDeterministic(t testing.TB, name string, read func(AppOpts) (any, error), opts AppOpts) {
	t.Helper()
	first, firstErr := read(opts)
	second, secondErr := read(opts)

	if (firstErr == nil) != (secondErr == nil) {
		t.Fatalf("%s: repeated reads disagree on failure: %v then %v", name, firstErr, secondErr)
	}
	if firstErr != nil {
		return
	}
	if a, b := Dump(first), Dump(second); a != b {
		t.Fatalf("%s: repeated reads of the same AppOpts disagree\n--- first\n%s\n--- second\n%s", name, a, b)
	}
}
