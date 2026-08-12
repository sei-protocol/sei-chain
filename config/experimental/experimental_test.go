package experimental_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/config/experimental"
)

// PR3 of the ConfigManager stack, rebuilt against the ratified LLD.
//
// Each test names the acceptance criterion it holds. The criteria live in the LLD; the names here
// are what makes a failure traceable to the decision it breaks.

// fakeSource is a Source a test controls byte for byte, so the sweep is hermetic. A viper would
// bring case folding and environment consultation into every assertion, and the sweep's contract
// is a pure function of its input.
type fakeSource struct {
	keys   []string
	values map[string]any
}

func (f fakeSource) AllKeys() []string { return f.keys }
func (f fakeSource) Get(k string) any  { return f.values[k] }

// src builds a source whose enumerated keys are exactly the map's keys.
func src(values map[string]any) fakeSource {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	return fakeSource{keys: keys, values: values}
}

// declare registers one well-formed key and returns it, resetting the registry first.
func declare(t *testing.T) *experimental.Key[int] {
	t.Helper()
	experimental.Reset()
	return experimental.Int(experimental.Decl[int]{
		Name: "probe.workers", Default: 8, Owner: "configtest", Since: "v6.6.0",
	})
}

// TestA6AbsentResolvesToTheDeclaredDefault holds A-6: never the type's zero.
func TestA6AbsentResolvesToTheDeclaredDefault(t *testing.T) {
	k := declare(t)

	if got := k.Get(src(nil)); got != 8 {
		t.Errorf("an absent key read %d, want the declared default 8. An operator who wrote nothing "+
			"gets the value the team shipped, not the type's zero", got)
	}
}

// TestA7RejectedValuesResolveToTheDefaultAndAreNamed holds A-7's rejection set.
//
// Each row is a coercion the legacy path performs silently. cast reads a blank as zero, a bool as
// 0 or 1, and a bare number as nanoseconds, and discards the error every time.
func TestA7RejectedValuesResolveToTheDefaultAndAreNamed(t *testing.T) {
	experimental.Reset()
	i := experimental.Int(experimental.Decl[int]{Name: "probe.n", Default: 8, Owner: "o", Since: "v1"})
	b := experimental.Bool(experimental.Decl[bool]{Name: "probe.b", Default: true, Owner: "o", Since: "v1"})
	s := experimental.String(experimental.Decl[string]{Name: "probe.s", Default: "d", Owner: "o", Since: "v1"})
	d := experimental.Duration(experimental.Decl[time.Duration]{Name: "probe.d", Default: time.Minute, Owner: "o", Since: "v1"})

	for _, tc := range []struct {
		name     string
		raw      any
		rejected bool
		reject   func(any) (*experimental.ValueError, bool)
	}{
		{"int from empty string", "", true, i.Reject},
		{"int from bool", true, true, i.Reject},
		{"int from float", 1.5, true, i.Reject},
		{"int from leading zero", "0755", true, i.Reject},
		{"int from decimal string", "16", false, i.Reject},
		{"int from int", 16, false, i.Reject},
		{"bool from number", 1, true, b.Reject},
		{"bool from empty", "", true, b.Reject},
		{"bool from string", "false", false, b.Reject},
		{"string from empty", "", false, s.Reject}, // an empty string is a legitimate string
		{"string from number", 5, true, s.Reject},
		{"duration unit-less", "30", true, d.Reject},
		{"duration from number", 30, true, d.Reject},
		{"duration with unit", "30s", false, d.Reject},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ve, usable := tc.reject(tc.raw)
			if usable == tc.rejected {
				t.Fatalf("Reject(%#v) reported usable=%v, want %v", tc.raw, usable, !tc.rejected)
			}
			if tc.rejected {
				if ve == nil {
					t.Fatal("a rejected value carried no ValueError, so nothing can report why")
				}
				// A-7 requires the raw value, its Go type, the declared type and the value used.
				for _, want := range []string{ve.Key, ve.Want, ve.Used} {
					if want == "" {
						t.Errorf("the ValueError omits a field an operator needs: %+v", ve)
					}
				}
			} else if ve != nil {
				t.Errorf("a usable value carried a ValueError: %v", ve)
			}
		})
	}
}

// TestA17ADefectiveDeclarationIsInertAndNeverPanics is the criterion my first implementation
// violated outright.
//
// A package-level panic in a library every feature imports is a boot failure for every invocation
// including seid --help, which turns a compile-time-fixable mistake into a fleet-wide incident.
// Three layers instead: the check fails CI, the declaration is recorded, and the handle is inert.
func TestA17ADefectiveDeclarationIsInertAndNeverPanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		decl experimental.Decl[int]
	}{
		{"mixed case", experimental.Decl[int]{Name: "Probe.Workers", Default: 8, Owner: "o", Since: "v1"}},
		{"one segment", experimental.Decl[int]{Name: "workers", Default: 8, Owner: "o", Since: "v1"}},
		{"namespace prefix", experimental.Decl[int]{Name: "experimental.a.b", Default: 8, Owner: "o", Since: "v1"}},
		{"empty segment", experimental.Decl[int]{Name: "probe..workers", Default: 8, Owner: "o", Since: "v1"}},
		{"no owner", experimental.Decl[int]{Name: "probe.workers", Default: 8, Since: "v1"}},
		{"no since", experimental.Decl[int]{Name: "probe.workers", Default: 8, Owner: "o"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			experimental.Reset()

			var k *experimental.Key[int]
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("declaring %+v panicked with %v. A panic here is a boot failure for "+
							"every seid invocation including --help", tc.decl, r)
					}
				}()
				k = experimental.Int(tc.decl)
			}()

			if len(experimental.Defects()) != 1 {
				t.Fatalf("a refused declaration produced %d defects, want 1; without one the boot "+
					"report says nothing and CI has nothing to fail on", len(experimental.Defects()))
			}
			// Inert: the operator's value is ignored, because nothing about the key is trustworthy.
			written := src(map[string]any{k.Path(): "16"})
			if got := k.Get(written); got != 8 {
				t.Errorf("an inert key read %d from a written value, want its declared default 8", got)
			}
			// And GetE never surfaces the declaration defect, or the prescribed refuse-on-error
			// idiom would refuse boot fleet-wide because a developer forgot Owner.
			if _, err := k.GetE(written); err != nil {
				t.Errorf("GetE on an inert key returned %v; a declaration defect is not an operator's "+
					"value error and must not reach a caller that refuses on it", err)
			}
			if len(experimental.Declarations()) != 0 {
				t.Error("a refused declaration was recorded as a declaration, so the golden would " +
					"present it as honoured")
			}
		})
	}
}

// TestA17ADefaultFailingItsOwnCheckIsNotInert holds the one deliberate exception.
//
// Inertness discards the operator's value in favour of the default, which is right when the name
// or metadata is wrong. It is wrong here: the default is the thing at fault, and a Decl omitting
// Default entirely would make Get return zero even when the operator wrote 8.
func TestA17ADefaultFailingItsOwnCheckIsNotInert(t *testing.T) {
	experimental.Reset()
	k := experimental.Int(experimental.Decl[int]{
		Name: "probe.workers", Default: 0, Owner: "o", Since: "v1",
		Check: func(v int) error {
			if v < 1 {
				return errors.New("must be at least 1")
			}
			return nil
		},
	})

	if ve, ok := k.RejectDefault(); ok {
		t.Error("RejectDefault reported a default of 0 usable against a check demanding at least 1")
	} else if ve == nil {
		t.Error("RejectDefault refused the default but carried no ValueError")
	}
	// Not inert: the operator's sound value is still honoured.
	if got := k.Get(src(map[string]any{k.Path(): "16"})); got != 16 {
		t.Errorf("a key whose default fails its own check read %d, want the operator's 16. Going "+
			"inert here would turn a developer's omission into silently ignored configuration", got)
	}
}

// TestA9ClassificationComesFromResolution holds A-9 and A-10.
//
// A key can be enumerated and still resolve to nothing, and reporting that as unrecognized sends
// an operator hunting a missing declaration when a variable ate their value.
func TestA9ClassificationComesFromResolution(t *testing.T) {
	k := declare(t)

	in := experimental.SweepInput{
		Source: fakeSource{
			// Enumerated, resolving to nil: the shadow case.
			keys:   []string{k.Path(), "experimental.other.key"},
			values: map[string]any{"experimental.other.key": "1"},
		},
		Checkers: experimental.Checkers(),
	}

	f := experimental.Sweep(in)
	if len(f.Shadowed) != 1 || f.Shadowed[0].Key != k.Path() {
		t.Fatalf("a declared key that resolves to nothing produced Shadowed=%+v, want one entry for "+
			"%s. Reported as unrecognized instead, an operator looks for a declaration that exists",
			f.Shadowed, k.Path())
	}
	if len(f.Unrecognized) != 1 || f.Unrecognized[0].Key != "experimental.other.key" {
		t.Errorf("the undeclared key that did resolve produced Unrecognized=%+v", f.Unrecognized)
	}
	// An unexplained shadow must say so rather than render an empty cause.
	if f.Shadowed[0].Cause != "" {
		t.Errorf("a shadow with no environment pass named cause %q; with no Environ supplied there "+
			"is nothing to name", f.Shadowed[0].Cause)
	}
	if f.EnvPassRan {
		t.Error("EnvPassRan is true with no EnvPrefix or Environ supplied, so a caller could not " +
			"tell a clean environment from one that was never looked at")
	}
}

// TestA10TheShadowIsASet holds that a prefix variable is named for every key beneath it.
func TestA10TheShadowIsASet(t *testing.T) {
	vars := experimental.EnvShadowVars("SEID", "experimental.giga_executor.occ_workers")

	want := []string{"SEID_EXPERIMENTAL", "SEID_EXPERIMENTAL_GIGA_EXECUTOR"}
	if strings.Join(vars, ",") != strings.Join(want, ",") {
		t.Errorf("EnvShadowVars returned %v, want %v. One per proper prefix, derived over the whole "+
			"prefixed name the way the boot's replacer does", vars, want)
	}
	// The full path is not a shadow of itself; it delivers the value.
	for _, v := range vars {
		if v == "SEID_EXPERIMENTAL_GIGA_EXECUTOR_OCC_WORKERS" {
			t.Error("the key's own variable is listed as a shadow; it delivers the value instead")
		}
	}
}

// TestA25ARetiredKeyIsClassifiedFromItsTombstone holds A-25.
//
// Telling an operator a deleted knob was promoted is its own wrong diagnostic, so the record
// distinguishes the two.
func TestA25ARetiredKeyIsClassifiedFromItsTombstone(t *testing.T) {
	experimental.Reset()
	experimental.Retired(experimental.Tombstone{
		Name: "probe.promoted", Type: "int", Owner: "o", Since: "v1",
		RetiredIn: "v6.7.0", PromotedTo: "probe.workers",
	})
	experimental.Retired(experimental.Tombstone{
		Name: "probe.removed", Type: "int", Owner: "o", Since: "v1", RetiredIn: "v6.7.0",
	})

	f := experimental.Sweep(experimental.SweepInput{
		Source: src(map[string]any{
			"experimental.probe.promoted": "1",
			"experimental.probe.removed":  "1",
		}),
		Tombstones: experimental.Tombstones(),
	})

	if len(f.Promoted) != 2 {
		t.Fatalf("two tombstoned keys produced %d Promoted findings; without them both read as "+
			"unrecognized, byte-identical to a typo", len(f.Promoted))
	}
	if len(f.Unrecognized) != 0 {
		t.Errorf("a tombstoned key was also reported unrecognized: %+v", f.Unrecognized)
	}
	var promoted, removed experimental.PromotedKey
	for _, p := range f.Promoted {
		if strings.HasSuffix(p.Key, "promoted") {
			promoted = p
		} else {
			removed = p
		}
	}
	if promoted.PromotedTo == "" {
		t.Error("the promoted key does not name where its value should move")
	}
	if removed.PromotedTo != "" {
		t.Errorf("the removed key names PromotedTo=%q, which would tell an operator to move a value "+
			"to a key that does not exist", removed.PromotedTo)
	}
}

// TestA3SilenceWhenClean holds A-3, including the bounds the criterion calls out.
//
// A section whose only anomaly is one over-long key would otherwise produce total silence, which
// is the class the bounds themselves would have created.
func TestA3SilenceWhenClean(t *testing.T) {
	k := declare(t)

	clean := experimental.Sweep(experimental.SweepInput{
		Source:   src(map[string]any{k.Path(): "16"}),
		Checkers: experimental.Checkers(),
	})
	if !clean.Empty() {
		t.Errorf("a sweep over one valid declared key is not Empty: %+v. Anything but silence here "+
			"means every node with a well-formed section gets output", clean)
	}

	oversize := experimental.Sweep(experimental.SweepInput{
		Source: src(map[string]any{
			"experimental." + strings.Repeat("a.", experimental.MaxKeySegments+2) + "z": "1",
		}),
	})
	if oversize.OversizeNames == 0 {
		t.Fatal("an over-segmented candidate was not counted")
	}
	if oversize.Empty() {
		t.Error("a sweep whose only anomaly is a skipped candidate reports Empty, so the bounds " +
			"would silently hide the key they refused to resolve")
	}
}

// TestA14ResultsAreDeterministic holds A-14: field-identical across runs, every slice sorted.
func TestA14ResultsAreDeterministic(t *testing.T) {
	experimental.Reset()
	in := experimental.SweepInput{Source: src(map[string]any{
		"experimental.z.z": "1", "experimental.a.a": "1", "experimental.m.m": "1",
	})}

	first, second := experimental.Sweep(in), experimental.Sweep(in)

	a := make([]string, 0, len(first.Unrecognized))
	b := make([]string, 0, len(second.Unrecognized))
	for i := range first.Unrecognized {
		a = append(a, first.Unrecognized[i].Key)
	}
	for i := range second.Unrecognized {
		b = append(b, second.Unrecognized[i].Key)
	}
	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatalf("two sweeps of one input disagreed: %v then %v", a, b)
	}
	for i := 1; i < len(a); i++ {
		if a[i-1] > a[i] {
			t.Errorf("Unrecognized is not sorted at %d: %v. A source enumerates in map order, so "+
				"unsorted output changes across boots of one configuration", i, a)
		}
	}
}

// TestA19AZeroKeyIsSafe holds the zero-value contract.
//
// A struct field left unset must not panic inside a method documented not to fail.
func TestA19AZeroKeyIsSafe(t *testing.T) {
	var k experimental.Key[int]

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a zero Key panicked: %v", r)
		}
	}()
	if got := k.Get(src(nil)); got != 0 {
		t.Errorf("a zero Key read %d, want the zero value of its type", got)
	}
	if k.Name() != "" || k.Path() != "" {
		t.Errorf("a zero Key names %q at %q, want both empty", k.Name(), k.Path())
	}
	if _, ok := k.Reject("anything"); !ok {
		t.Error("a zero Key rejected a value; there is no declaration to reject against")
	}
}

// TestA19ReadsAgreeUnderRace holds the read path's concurrency contract.
//
// A Key is self-contained: Get consults the Key and never the registry, so a read takes no lock and
// does not depend on package initialisation order or on which packages the binary linked. That is
// only worth claiming if concurrent reads of one AppOptions actually agree, so this is the assertion
// that makes `go test -race` mean something for it.
func TestA19ReadsAgreeUnderRace(t *testing.T) {
	k := declare(t)
	opts := src(map[string]any{k.Path(): "16"})

	const readers = 32
	got := make([]int, readers)
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i] = k.Get(opts)
		}(i)
	}
	wg.Wait()

	for i, v := range got {
		if v != 16 {
			t.Fatalf("reader %d resolved %d, want 16. Concurrent reads of one source have to agree, "+
				"or a value read twice in one boot could differ", i, v)
		}
	}
}

// TestA19ConcurrentDeclarationAndReadAreSafe covers the one shape that could still race.
//
// The registry has a mutex as insurance against a lazily declared key, which the package doc
// forbids. Reads never touch it, so a declaration landing while another key is being read must not
// be observable by that read.
func TestA19ConcurrentDeclarationAndReadAreSafe(t *testing.T) {
	k := declare(t)
	opts := src(map[string]any{k.Path(): "16"})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			experimental.Int(experimental.Decl[int]{
				Name:    fmt.Sprintf("probe.late_%02d", i),
				Default: 1, Owner: "configtest", Since: "v1",
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if v := k.Get(opts); v != 16 {
				t.Errorf("a read returned %d while declarations were landing, want 16. Get must not "+
					"consult the registry, or a read's answer depends on init order", v)
				return
			}
		}
	}()
	wg.Wait()

	// And the accessors are safe to walk afterwards, since the sweep and the checks do exactly that.
	if len(experimental.Declarations()) == 0 {
		t.Error("no declarations survived the concurrent registration")
	}
	_ = experimental.Checkers()
	_ = experimental.Defects()
}
