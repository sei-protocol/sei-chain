package configtest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"testing"
)

// This file closes the hole that made the manifest's central promise untrue.
//
// A row's assertion is a comparison against the absent-key view with the row's own
// leaf spliced in (assertResolvedView). When the value a seed carries resolves that
// leaf to what an absent key already resolves it to, the two sides of the comparison
// are identical and the assertion holds for a reader that reads the key and for one
// that never looks it up — the resolved documents are the same document. A row whose
// every seed is of that shape is pinned in appearance only: renaming its key in the
// production reader leaves the suite green.
//
// Whether a row has such a seed is a property of the corpus, not of any one value,
// and a corpus written as a series of f.Add calls is invisible to the harness. So the
// corpus is recorded on its way to f.Add, and the property is asserted once per
// target, where "all the seeds" is a thing that exists.

// DecodeSeed turns the scalar tuple a fuzz target's seed carries into the
// configuration value it stands for.
//
// It is fuzzing.ConfigValue, handed in rather than imported: this package
// deliberately depends on no other sei-chain package so that the packages under
// test can import it.
type DecodeSeed func(kind uint8, s string, n int64, b bool) any

// Seeds is a manifest-driven fuzz target's seed corpus, recorded as it is added.
//
// Add and AddRow forward to f.Add before recording, so a seed cannot be recorded
// without being seeded, and a seed added through f.Add directly is invisible here
// and so fails CheckEveryRowHasADiscriminatingSeed rather than satisfying it. Both
// directions are fail-closed, which is what keeps the recorder from becoming a
// second place the corpus is declared and drifts.
type Seeds struct {
	f       seedAdder
	decode  DecodeSeed
	entries []seedEntry
}

// seedAdder is the one method of *testing.F the recorder uses.
//
// Named as an interface only so this package's own contract tests can drive
// CheckEveryRowHasADiscriminatingSeed with a corpus they build, which needs a recorder
// outside a fuzz target. NewSeeds still takes the concrete *testing.F, so the
// forwarding property holds for every caller: no package outside this one can build a
// Seeds that records without seeding.
type seedAdder interface {
	Add(args ...any)
}

// seedEntry is one recorded seed: the manifest row it selects and the value it
// decodes to. The raw tuple is not kept, because the value is what the row is
// driven with and what a failure needs to talk about.
type seedEntry struct {
	row   uint
	value any
}

// NewSeeds returns a recorder that seeds f and remembers what it seeded.
func NewSeeds(f *testing.F, decode DecodeSeed) *Seeds {
	return &Seeds{f: f, decode: decode}
}

// AddRow seeds a target whose fuzz function selects a manifest row by index,
// taking the arguments f.Add takes.
func (s *Seeds) AddRow(row uint, kind uint8, str string, n int64, b bool) {
	s.f.Add(row, kind, str, n, b)
	s.entries = append(s.entries, seedEntry{row: row, value: s.decode(kind, str, n, b)})
}

// Add seeds a target whose fuzz function carries no row index because its section
// has a single row to drive.
func (s *Seeds) Add(kind uint8, str string, n int64, b bool) {
	s.f.Add(kind, str, n, b)
	s.entries = append(s.entries, seedEntry{row: 0, value: s.decode(kind, str, n, b)})
}

// CheckEveryRowHasADiscriminatingSeed asserts that every manifest row is seeded
// with at least one value whose assertion would fail if the reader stopped reading
// the row's key.
//
// This is one of the two checks that make the standing rule true for key names, and it
// is the half that watches the reader: it catches a read site that stopped reading the
// key the row names. It cannot catch the row and the reader moving together, which is
// what editing a shared flag constant does, and CheckKeyNames is the half that does.
// requireKeyNameRecord below ties them, so a section cannot be seeded without also
// recording its names.
//
// A row states that the reader resolves spec.Key into spec.Path, and CheckKey holds it
// to that by comparing the resolved view against the absent-key view with the predicted
// leaf spliced in. The comparison only carries information when the predicted leaf
// differs from the absent-key one: where they agree, the assertion is satisfied by
// a reader that ignores the key entirely, which is exactly what a renamed key
// produces. Nine of the ninety-six rows the suite then held were in that position when this was
// written, spread over the two shapes discriminationHint names.
//
// It runs before f.Fuzz rather than inside the fuzz function on purpose. Only the
// corpus as a whole can answer the question — a nil value on a guarded row is
// legitimately non-discriminating, and asserting per value would forbid the seed
// that pins the guard — and only the target body sees the corpus as a whole. That
// also makes the verdict identical in every mode the suite runs in: it depends on
// the seeds the target declares, not on which of them a given process happens to
// execute, so a single-seed -run filter and a -fuzz worker reach the same answer as
// a plain go test.
//
// What makes that last claim true is that the corpus it reads is the recorder's and
// nothing else: seeds.entries, never the entries a -fuzz run has cached under
// GOCACHE. One target was measured at 140 cached corpus entries against 6 declared
// seeds, so a cached entry that happens to discriminate a row cannot stand in for a
// seed the target does not declare, and a row's verdict does not depend on whose
// machine it runs on.
//
// One consequence is worth knowing before it surprises someone. F.Fuzz returns
// without running anything when the target has already failed, so a section whose
// seeds fail here does not run its row assertions until the seeds are repaired.
// Asserting after f.Fuzz instead — which would let both run — is worse than it
// looks: under -fuzz, F.Fuzz does not return until a problem is found, fuzztime runs
// out, or a signal interrupts the process (testing/fuzz.go:208-210), so the verdict
// would be withheld for the whole fuzzing window and skipped altogether on an
// interrupt. The same lines sanction this placement, saying F.Fuzz is called exactly
// once unless the target has already failed beforehand. A check whose verdict arrives
// only sometimes is the failure this suite exists to prevent, so it is placed where
// it cannot.
//
// specs is the same table the fuzz function picks from, and row indices are reduced
// modulo its length exactly as Pick reduces them. alsoRecorded names the section's
// keys that have no row — the ones a target of their own asserts — and is here
// because this is where the record is compared: it must be the same list
// CheckKeyNames is given, or the two disagree with the file and both say so.
func CheckEveryRowHasADiscriminatingSeed(
	t testing.TB, name string, read func(AppOpts) (any, error), specs []KeySpec, seeds *Seeds,
	alsoRecorded ...KeyName,
) {
	t.Helper()

	// An empty table leaves the loop below with nothing to check, so the section would pass while
	// pinning nothing. Panics as Pick does, because an empty table is a defect in the test that
	// called this rather than in the behavior under test.
	if len(specs) == 0 {
		panic("configtest.CheckEveryRowHasADiscriminatingSeed: empty KeySpec table, " +
			"the section's manifest has no rows")
	}

	recordPath := goldenFilePath(t, name, keyNameRecordSuffix)
	requireKeyNameRecord(t, name, recordPath, specs, alsoRecorded)

	baseline, err := read(AppOpts{})
	if err != nil {
		t.Fatalf("%s: reading an empty AppOpts must succeed, got %v", name, err)
	}
	baselineDump := Dump(baseline)

	// row counts up alongside the range rather than converting the index, so the comparison
	// against Pick's reduction happens entirely in uint. The conversion this replaces was
	// provably in range — a value reduced modulo len(specs) cannot exceed an int — but gosec's
	// G115 cannot see that, and a //nolint on arithmetic this check depends on would be a
	// suppression a reader has to take on trust.
	var row uint
	for _, spec := range specs {
		current := row
		row++

		absent, ok := leafAt(baselineDump, spec.Path)
		if !ok {
			t.Errorf("%s: %s claims to resolve into field %q, which is not present in the "+
				"resolved view:\n%s", name, spec.Key, spec.Path, baselineDump)
			continue
		}

		seeded, discriminating := 0, false
		for _, e := range seeds.entries {
			// The same reduction Pick applies, compared without leaving uint.
			if e.row%uint(len(specs)) != current {
				continue
			}
			seeded++
			if seedDiscriminates(read, spec, e.value, absent) {
				discriminating = true
				break
			}
		}
		if discriminating {
			continue
		}

		hint := discriminationHint(spec, absent)
		if seeded == 0 {
			t.Errorf("%s: %s has no seed, so an ordinary `go test` run never drives the row and "+
				"only -fuzz would. Seed it with a value that resolves %s to something other than "+
				"%s%s.", name, spec.Key, spec.Path, absent, hint)
			continue
		}

		// Two different mistakes reach this point, and they need opposite repairs, so they are
		// told apart rather than described together. If some value discriminates, the reader
		// does read the key and the seeds are simply absent-equivalent. If nothing does, the
		// key the row names is not a key the reader looks up, and reseeding is wasted effort.
		//
		// This is the branch that has to carry the diagnosis on its own for a row spelled as a
		// literal. Such a row does not move when the reader's literal moves, so its recorded
		// key name still matches and CheckKeyNames stays green; a rename shows up here and
		// nowhere else.
		if example, ok := aDiscriminatingValue(read, spec, absent); ok {
			resolve := fmt.Sprintf("all %d of its seeds resolve", seeded)
			if seeded == 1 {
				resolve = "its only seed resolves"
			}
			t.Errorf("%s: %s has no discriminating seed: %s to %s, which is what an absent key "+
				"resolves to, so every assertion the row makes is satisfied by a reader that never "+
				"looks %s up. Renaming the key in the reader would leave this suite green, which is "+
				"the one thing the manifest exists to prevent. Seed the row with a value that resolves "+
				"to a different leaf%s — %#v is one that does.",
				name, spec.Key, resolve, absent, spec.Key, hint, example)
			continue
		}
		t.Errorf("%s: %s resolves %s to %s no matter what value is put under it, which is what a "+
			"reader that does not read that key looks like: an AppOpts holding only %s reads "+
			"identically to an empty one. So this is not a seeding problem and adding seeds will "+
			"not fix it — the likely cause is a read site renamed while the row kept the old "+
			"spelling, so compare the key in the reader against the row, and against %s. The other "+
			"possibility is that %s is not the field this key reaches. %d seed(s) were tried, and "+
			"so were %d further value shapes.",
			name, spec.Key, spec.Path, absent, spec.Key,
			recordPath, spec.Path, seeded, len(discriminationProbes))
	}
}

// aDiscriminatingValue looks for any value at all that makes spec's row observable,
// returning the first it finds.
//
// It is what lets the failure above distinguish "these seeds happen to be
// absent-equivalent" from "no seed could ever work here". The probe list is fixed and
// deliberately crude: it is not trying to find the best seed for the row, only to answer
// whether the reader reads the key, and a value that moves the leaf or errors proves it
// does. Its cost is paid only on the failure path.
//
// Like seedDiscriminates it consults neither Cast, Checked nor Unguarded, so the verdict
// cannot be steered by editing the columns that describe the reader.
func aDiscriminatingValue(read func(AppOpts) (any, error), spec KeySpec, absent string) (any, bool) {
	for _, v := range discriminationProbes {
		if seedDiscriminates(read, spec, v, absent) {
			return v, true
		}
	}
	return nil, false
}

// discriminationProbes are the value shapes tried when none of a row's own seeds
// discriminate.
//
// Every entry is a shape fuzzing.ConfigValue can produce, so one reported back as an
// example is a seed an engineer can actually write. Between them they move the leaf of
// every cast the manifest declares — a bool either way, a numeric that is neither zero nor
// a plausible default, a non-empty string, a duration spelling, a float, a slice and a map
// — and on a checked row the ones a cast rejects discriminate through the error instead.
//
// A number is tried first because it is the one shape that reads sensibly whatever the row's
// cast is: cast turns it into a string, a duration, a float and a non-zero bool, so a row of
// any of those casts gets an example an engineer would plausibly have written by hand rather
// than a bool offered to an integer key.
var discriminationProbes = []any{
	int64(917), probeMarker, true, false, int64(-1), 917.5, "917", "7h13m", "true",
	[]string{probeMarker}, []any{probeMarker}, map[string]any{probeMarker: 1}, nil,
}

// probeMarker is the string the probes carry. Named so that a value reported back as an
// example is recognizable as this harness's rather than as something an operator wrote, and
// so that it cannot collide with a section's in-code default and stop discriminating.
const probeMarker = "sei-configtest-probe"

// requireKeyNameRecord fails the section when its recorded key names are not the names its
// manifest resolves.
//
// This check and CheckKeyNames cover complementary halves of one promise, and a section
// wired for one and not the other is protected against only half of a rename: the seeds
// catch a reader that drifted away from the manifest's spelling, the record catches a
// manifest that moved with it. Nothing about a manifest declares which checks it is
// subject to, so the record is asserted from the one check every manifest already calls,
// and a section cannot acquire seeds without acquiring the record.
//
// It compares the names rather than only looking for the file, and that is the difference
// between a guarantee and a convention. Presence enforces acquiring the record and not
// retaining the comparison: deleting a single CheckKeyNames call while leaving the record
// on disk re-greened a verified rename of a [state-commit] key. Deleting a check to clear a
// failure is the move the harness guide forbids for rows, so the tie is a comparison that
// would have to be broken in two places rather than one.
//
// The cost is that a genuine rename is reported twice, which is the cheaper side of the
// trade: one duplicate report on a rename someone meant, against a one-line path that
// silences one they did not.
//
// It renders that report itself rather than deferring to CheckKeyNames, because the run where this
// matters most is the run where CheckKeyNames is gone: a reader told to consult a call someone has
// just deleted is told to read output that will never appear. So the disagreement is named here, in
// the same terms, and the instruction points at the record rather than at a call site.
func requireKeyNameRecord(t testing.TB, name, path string, specs []KeySpec, alsoRecorded []KeyName) {
	t.Helper()

	// -update rewrites the record, and CheckKeyNames is what rewrites it. Comparing against a file
	// being regenerated in the same run would report the rename someone is recording on purpose,
	// and would do it or not depending on which of the two tests the runner reached first.
	if goldenUpdateRequested() {
		return
	}

	want := keyNameRecord(specs, alsoRecorded)

	raw, err := os.ReadFile(path) //nolint:gosec // testdata/<section>.keys.golden; the whole file name is validated by goldenFilePath
	switch {
	case errors.Is(err, fs.ErrNotExist):
		t.Errorf("%s: this section's seeds are checked but its key names are not recorded: %v.\n"+
			"A seeded manifest whose rows name their keys through the reader's own constants is "+
			"protected against the reader drifting from the constant and not against the constant "+
			"itself being edited, which is how an operator-facing key gets renamed. Add a "+
			"CheckKeyNames call for this section and create the record with "+
			"`go test ./<pkg>/ -run TestKeyNames -update`.", name, err)
	case err != nil:
		// Not a missing record, so the advice above would be wrong: an unreadable file or an
		// inaccessible directory is not repaired by regenerating anything.
		t.Errorf("%s: cannot read this section's key-name record: %v", name, err)
	case recordText(raw) != want:
		t.Errorf("%s: the key names in %s are no longer the names this manifest resolves.\n%s\n"+
			"This check compares that record so that deleting the CheckKeyNames call cannot turn a "+
			"rename back into a green run, which is why it reports the difference itself rather than "+
			"naming a call that may no longer be there. Once the change is deliberate, regenerate the "+
			"record with `go test ./<pkg>/ -run TestKeyNames -update` and keep the diff in the review.",
			name, path, keyNameDiff(splitRecord(recordText(raw)), splitRecord(want), len(specs)))
	}
}

// seedDiscriminates reports whether the assertion CheckKey makes about spec for
// value would come out differently if the reader stopped reading spec.Key.
//
// The test is observational rather than predictive. A reader consults an AppOpts
// only through Get, so one that no longer looks spec.Key up resolves an AppOpts
// holding nothing but that key exactly as it resolves an empty one — the absent-key
// view. A value therefore discriminates precisely when the read it produces differs
// from that view, either by failing where the absent-key read succeeded (the shape a
// checked row's malformed seed pins) or by moving the leaf the row nominates.
//
// The two are what assertResolvedView compares, so this agrees with the assertion it
// is reasoning about rather than modeling it: the leaf at spec.Path, and the error.
// The remaining fields are compared there too, but a row that moves one of those
// fails rather than passes, and the paths in spec.AlsoWrites are dropped from both
// sides, so neither can carry a row's discriminating power.
//
// It deliberately consults neither Cast, Checked nor Unguarded. Those three columns
// describe the reader, and editing them to match a reader you changed is one of the
// four ways of silencing a failure the harness guide forbids; a check that read them
// could be silenced the same way.
func seedDiscriminates(read func(AppOpts) (any, error), spec KeySpec, value any, absent string) bool {
	got, err := read(AppOpts{spec.Key: value})
	if err != nil {
		// The caller has already established that the absent-key read succeeds, so an
		// error is a difference on its own.
		return true
	}
	leaf, ok := leafAt(Dump(got), spec.Path)
	return !ok || leaf != absent
}

// discriminationHint names why the seeds a row carries cannot discriminate, when the
// reason follows from the row itself rather than from the values chosen.
//
// The failure it decorates is nearly always one of two shapes, and they need
// different repairs. A row whose absent-key leaf is already its cast's zero gets
// nothing from a nil or from a value the cast rejects, because both resolve to that
// same zero — every unguarded row is in this position, since an absent key reaches
// its reader as a nil. A row whose absent-key leaf is a real default is instead
// undone by a seed that happens to carry that default.
func discriminationHint(spec KeySpec, absent string) string {
	if absent != DumpAt(spec.Path, spec.Cast.zero()) {
		return fmt.Sprintf(" (%s is the in-code default, so a seed carrying that same value "+
			"cannot discriminate; any other %s can)", absent, spec.Cast)
	}
	if spec.Checked {
		return fmt.Sprintf(" (the absent-key leaf is already the %s zero, so a nil resolves to "+
			"it; a value the cast rejects discriminates through the error a checked read must "+
			"return, and so does any value that converts to something else)", spec.Cast)
	}
	return fmt.Sprintf(" (the absent-key leaf is already the %s zero and the read is unchecked, "+
		"so a nil and a value the cast rejects both resolve to it; only a value that converts to "+
		"something else can discriminate)", spec.Cast)
}
