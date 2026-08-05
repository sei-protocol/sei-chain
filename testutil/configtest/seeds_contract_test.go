package configtest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cast"
)

// probeConfig stands in for a section's resolved view: one field with a real
// in-code default and one whose default is already the zero value, which is the
// pair of cases the discrimination check has to tell apart.
type probeConfig struct {
	Count int
	Name  string
}

func defaultProbeConfig() probeConfig { return probeConfig{Count: 100, Name: "pebbledb"} }

// probeReader builds the four reader shapes the legacy path actually has, so the
// predicate is exercised against real cast-and-guard behavior rather than against a
// model of it. keyRead is the key the reader looks up, which is what a rename
// changes and what the whole check exists to notice.
func probeReader(keyRead string, guarded, checked bool) func(AppOpts) (any, error) {
	return func(opts AppOpts) (any, error) {
		cfg := defaultProbeConfig()
		v := opts.Get(keyRead)
		if guarded && v == nil {
			return cfg, nil
		}
		if checked {
			n, err := cast.ToIntE(v)
			if err != nil {
				return cfg, fmt.Errorf("count: %w", err)
			}
			cfg.Count = n
			return cfg, nil
		}
		cfg.Count = cast.ToInt(v)
		return cfg, nil
	}
}

// TestSeedDiscriminatesMatchesWhatARenameWouldSurvive pins the predicate
// CheckEveryRowHasADiscriminatingSeed rests on, case by case.
//
// Each case is a (reader shape, seeded value) pair and the verdict the predicate must
// reach. The pairs that must come out false are the ones the audit found in the
// manifest: a nil on a guarded row, and — on an unguarded row, whose absent-key value
// is already the cast's zero — a nil, a value the cast rejects on an unchecked read,
// and an explicit zero. Each of those resolves the row's leaf to exactly what an
// absent key resolves it to, so no assertion built on it can see a reader stop
// reading the key.
func TestSeedDiscriminatesMatchesWhatARenameWouldSurvive(t *testing.T) {
	const key = "probe.count"
	spec := KeySpec{Key: key, Path: "Count", Cast: CastInt}

	cases := []struct {
		what      string
		guarded   bool
		checked   bool
		value     any
		want      bool
		whyItSays string
	}{
		{
			what: "guarded row, nil", guarded: true, value: nil, want: false,
			whyItSays: "the guard keeps the default, which is the absent-key value",
		},
		{
			what: "guarded row, a value the cast rejects", guarded: true, value: "not-a-number", want: true,
			whyItSays: "the unchecked cast yields 0 over a default of 100",
		},
		{
			what: "guarded row, the default spelled out", guarded: true, value: 100, want: false,
			whyItSays: "the resolved leaf is the default either way",
		},
		{
			what: "guarded checked row, a value the cast rejects", guarded: true, checked: true,
			value: "not-a-number", want: true,
			whyItSays: "the read must return an error, which an absent key does not",
		},
		{
			what: "unguarded row, nil", value: nil, want: false,
			whyItSays: "an absent key reaches the reader as a nil, so this is the absent-key read",
		},
		{
			what: "unguarded row, a value the cast rejects", value: "not-a-number", want: false,
			whyItSays: "the unchecked cast yields 0, which is what the absent key already clobbered to",
		},
		{
			what: "unguarded row, an explicit zero", value: 0, want: false,
			whyItSays: "0 is what the clobber already produced",
		},
		{
			what: "unguarded row, a value that converts", value: 7, want: true,
			whyItSays: "7 is not the clobbered 0",
		},
	}

	for _, c := range cases {
		read := probeReader(key, c.guarded, c.checked)
		absent := absentLeaf(t, read, spec)
		if got := seedDiscriminates(read, spec, c.value, absent); got != c.want {
			t.Errorf("%s: seedDiscriminates = %v, want %v (%s)", c.what, got, c.want, c.whyItSays)
		}
	}
}

// TestNoSeedDiscriminatesAgainstARenamedKey is the property the deliverable reduces
// to, asserted directly: against a reader that looks up a different key — which is
// what renaming one produces — no value discriminates at all.
//
// It is the converse of the check being useful. If some value could discriminate
// against a reader that never reads the key, the predicate would be reporting
// something other than "this seed notices the rename", and a row could satisfy the
// check while staying renameable.
func TestNoSeedDiscriminatesAgainstARenamedKey(t *testing.T) {
	spec := KeySpec{Key: "probe.count", Path: "Count", Cast: CastInt}
	values := []any{nil, 0, 7, -1, "not-a-number", "7", true, []string{"7"}, map[string]any{"a": 1}}

	for _, guarded := range []bool{true, false} {
		for _, checked := range []bool{true, false} {
			renamed := probeReader("probe.count-v2", guarded, checked)
			absent := absentLeaf(t, renamed, spec)
			for _, v := range values {
				if seedDiscriminates(renamed, spec, v, absent) {
					t.Errorf("guarded=%v checked=%v: %#v discriminated against a reader that reads "+
						"a different key, so the predicate is not reporting what a rename survives",
						guarded, checked, v)
				}
			}
		}
	}
}

// TestSomeSeedDiscriminatesForEveryReaderShape closes the other half: every reader
// shape has a value that notices it, so a row failing the check is a statement about
// its seeds and never about the shape being unpinnable.
func TestSomeSeedDiscriminatesForEveryReaderShape(t *testing.T) {
	const key = "probe.count"
	spec := KeySpec{Key: key, Path: "Count", Cast: CastInt}
	values := []any{nil, 0, 7, "not-a-number"}

	for _, guarded := range []bool{true, false} {
		for _, checked := range []bool{true, false} {
			read := probeReader(key, guarded, checked)
			absent := absentLeaf(t, read, spec)
			found := false
			for _, v := range values {
				if seedDiscriminates(read, spec, v, absent) {
					found = true
				}
			}
			if !found {
				t.Errorf("guarded=%v checked=%v: no value in %v discriminates, so this reader shape "+
					"cannot be pinned at all", guarded, checked, values)
			}
		}
	}
}

// absentLeaf renders the leaf a reader handed no keys resolves at spec.Path, the
// value every discrimination verdict is measured against.
func absentLeaf(t *testing.T, read func(AppOpts) (any, error), spec KeySpec) string {
	t.Helper()
	cfg, err := read(AppOpts{})
	if err != nil {
		t.Fatalf("reading an empty AppOpts must succeed, got %v", err)
	}
	leaf, ok := leafAt(Dump(cfg), spec.Path)
	if !ok {
		t.Fatalf("%q is not in the resolved view:\n%s", spec.Path, Dump(cfg))
	}
	return leaf
}

// The tests below cover CheckEveryRowHasADiscriminatingSeed rather than the predicate it
// calls. The predicate decides one row against one value; the aggregation decides which
// values belong to which row, what to say when a row has none, and whether one bad row
// hides the next. Each of those was established by hand when the check was written and
// nothing held it afterwards, which is the position a row with no seed is in.

// probeSection is the section name the aggregation tests record and read under. It is not a
// real TOML section; the record it needs is written into a directory of the test's own.
const probeSection = "probe-table"

// tableConfig stands in for a section resolved by a reader with more than one key, which is
// the shape the aggregation has to attribute seeds across. Every field defaults to a real
// value rather than a zero, so a seed of 7 discriminates and a nil does not.
type tableConfig struct {
	A, B, C int
}

// tableSpecs is the manifest tableRead is described by: three guarded, unchecked rows.
var tableSpecs = []KeySpec{
	{Key: "probe.a", Path: "A", Cast: CastInt},
	{Key: "probe.b", Path: "B", Cast: CastInt},
	{Key: "probe.c", Path: "C", Cast: CastInt},
}

// tableReaderReading builds a reader over the keys given, in tableSpecs' field order.
//
// Taking the keys as an argument is what lets a test move the read sites without moving the
// manifest, which is what renaming a key in production does to a row spelled as a literal.
func tableReaderReading(keys ...string) func(AppOpts) (any, error) {
	return func(opts AppOpts) (any, error) {
		cfg := tableConfig{A: 100, B: 100, C: 100}
		fields := []*int{&cfg.A, &cfg.B, &cfg.C}
		for i, key := range keys {
			if v := opts.Get(key); v != nil {
				*fields[i] = cast.ToInt(v)
			}
		}
		return cfg, nil
	}
}

// tableRead is the reader the manifest describes: it reads exactly the keys tableSpecs names.
var tableRead = tableReaderReading("probe.a", "probe.b", "probe.c")

// The two seed shapes the aggregation tests need, in the position fuzzing.Kind* occupies in
// a real target.
const (
	probeNil uint8 = iota
	probeInt
)

// probeDecode stands in for fuzzing.ConfigValue, carrying the two shapes that decide a
// guarded row: a nil, which resolves as an absent key does, and a number, which does not.
func probeDecode(kind uint8, _ string, n int64, _ bool) any {
	if kind == probeInt {
		return int(n)
	}
	return nil
}

// seedCollector stands in for *testing.F, which cannot be constructed outside a fuzz
// target. It keeps what it was handed so that the forwarding the real recorder does — seed
// first, then record — is still exercised rather than bypassed.
type seedCollector struct{ added [][]any }

func (c *seedCollector) Add(args ...any) { c.added = append(c.added, args) }

// newRecordedSeeds returns a recorder that can be driven from an ordinary test.
func newRecordedSeeds() *Seeds {
	return &Seeds{f: &seedCollector{}, decode: probeDecode}
}

// TestDiscriminatingSeedRowIndicesReduceExactlyAsPickDoes pins the aggregation's row
// attribution against the reduction the fuzz function uses.
//
// A seed's row index is reduced modulo the table length in two places: here, deciding which
// row a seed speaks for, and in Pick, deciding which row the fuzz function drives it
// against. If the two ever disagreed, a row could be reported as covered by a seed that in
// fact drives a different row, and the check would certify coverage it does not have. So the
// verdict is compared against Pick itself rather than against a second copy of the
// arithmetic, across indices that wrap the table several times.
func TestDiscriminatingSeedRowIndicesReduceExactlyAsPickDoes(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)

	for idx := uint(0); idx < uint(3*len(tableSpecs)+2); idx++ {
		seeds := newRecordedSeeds()
		seeds.AddRow(idx, probeInt, "", 7, false)

		reported := capture(t, func(tb testing.TB) {
			CheckEveryRowHasADiscriminatingSeed(tb, probeSection, tableRead, tableSpecs, seeds)
		})

		satisfied := Pick(tableSpecs, idx)
		if named := reported.mentioning(satisfied.Key); len(named) != 0 {
			t.Errorf("a seed at row index %d was reported as not covering %s, but Pick(%d) drives "+
				"that row, so the seed and the assertion it is credited to would disagree:\n%s",
				idx, satisfied.Key, idx, strings.Join(named, "\n"))
		}
		if want := len(tableSpecs) - 1; len(reported.failures) != want {
			t.Errorf("a seed at row index %d left %d rows reported rather than %d, so it was "+
				"credited to more or fewer rows than the one Pick selects:\n%s",
				idx, len(reported.failures), want, strings.Join(reported.failures, "\n"))
		}
	}
}

// TestDiscriminatingSeedCheckNamesAnUnseededRowAsUnseeded pins the seeded == 0 branch.
//
// A row with no seeds and a row whose seeds all land on the absent-key value both leave the
// manifest unpinned, but the repairs differ and so do the messages: one row needs a seed at
// all, the other needs a different one. Reporting the second wording for the first case
// would send an engineer looking for a seed that is already there.
func TestDiscriminatingSeedCheckNamesAnUnseededRowAsUnseeded(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)

	seeds := newRecordedSeeds()
	seeds.AddRow(0, probeInt, "", 7, false)
	seeds.AddRow(1, probeInt, "", 7, false)
	// Row 2 is left unseeded, which is the case under test.

	reported := capture(t, func(tb testing.TB) {
		CheckEveryRowHasADiscriminatingSeed(tb, probeSection, tableRead, tableSpecs, seeds)
	})
	msg := reported.only(t)

	if !strings.Contains(msg, tableSpecs[2].Key) {
		t.Errorf("the unseeded row's key is not named in its own failure:\n%s", msg)
	}
	if !strings.Contains(msg, "has no seed") {
		t.Errorf("an unseeded row must be reported as unseeded:\n%s", msg)
	}
	if strings.Contains(msg, "has no discriminating seed") {
		t.Errorf("an unseeded row was reported as having non-discriminating seeds, which sends the "+
			"reader looking for seeds that do not exist:\n%s", msg)
	}
}

// TestDiscriminatingSeedCheckReportsEveryFailingRow pins that the verdict is per row.
//
// The check reports through Errorf rather than Fatalf so that one run names every unpinned
// row. With Fatalf, repairing a section would be a sequence of runs each revealing one more
// row, and the count of unpinned rows — the number that says how much of the manifest is
// decorative — would never appear at all.
func TestDiscriminatingSeedCheckReportsEveryFailingRow(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)

	seeds := newRecordedSeeds()
	seeds.AddRow(0, probeNil, "", 0, false) // guarded: keeps the default, so absent-equivalent
	seeds.AddRow(1, probeInt, "", 7, false) // discriminates
	seeds.AddRow(2, probeNil, "", 0, false) // absent-equivalent again

	reported := capture(t, func(tb testing.TB) {
		CheckEveryRowHasADiscriminatingSeed(tb, probeSection, tableRead, tableSpecs, seeds)
	})

	if len(reported.failures) != 2 {
		t.Fatalf("two rows are unpinned and %d were reported, so the check stops at the first "+
			"rather than naming them all:\n%s", len(reported.failures),
			strings.Join(reported.failures, "\n---\n"))
	}
	for _, spec := range []KeySpec{tableSpecs[0], tableSpecs[2]} {
		if len(reported.mentioning(spec.Key)) != 1 {
			t.Errorf("%s is unpinned and is not named exactly once:\n%s", spec.Key,
				strings.Join(reported.failures, "\n---\n"))
		}
	}
	if named := reported.mentioning(tableSpecs[1].Key); len(named) != 0 {
		t.Errorf("%s has a discriminating seed and was reported anyway:\n%s",
			tableSpecs[1].Key, strings.Join(named, "\n"))
	}
	for _, msg := range reported.failures {
		if !strings.Contains(msg, "has no discriminating seed") {
			t.Errorf("a row whose reader does read its key must be reported as a seeding problem:\n%s", msg)
		}
	}
}

// TestDiscriminatingSeedCheckPointsAtTheReaderWhenNothingDiscriminates pins the message an
// engineer sees for the failure this whole check exists to produce.
//
// When a read site is renamed and the row keeps the old spelling, no seed can discriminate,
// because an AppOpts holding only a key nobody reads resolves as an empty one does. The
// check fires — that part always worked — but describing it as a shortage of seeds sends the
// engineer to add seeds, one after another, none of which can ever pass. So the two causes
// are told apart by whether any value at all discriminates, and the rename is named.
func TestDiscriminatingSeedCheckPointsAtTheReaderWhenNothingDiscriminates(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)

	// The reader now looks up different keys, which is what renaming them produces.
	renamed := tableReaderReading("probe.a-v2", "probe.b-v2", "probe.c-v2")

	seeds := newRecordedSeeds()
	for i := range tableSpecs {
		seeds.AddRow(uint(i), probeInt, "", 7, false)
	}

	reported := capture(t, func(tb testing.TB) {
		CheckEveryRowHasADiscriminatingSeed(tb, probeSection, renamed, tableSpecs, seeds)
	})

	if len(reported.failures) != len(tableSpecs) {
		t.Fatalf("every row's key is unread and %d of %d were reported:\n%s", len(reported.failures),
			len(tableSpecs), strings.Join(reported.failures, "\n---\n"))
	}
	for _, msg := range reported.failures {
		if !strings.Contains(msg, "does not read that key") {
			t.Errorf("a row no value can discriminate must be reported as an unread key rather than "+
				"as a seeding problem:\n%s", msg)
		}
		if strings.Contains(msg, "Seed the row with") {
			t.Errorf("the report asks for another seed, which cannot fix an unread key:\n%s", msg)
		}
	}
}

// TestDiscriminatingSeedCheckRequiresARecordedKeyNameList pins the tie between the two
// halves of the rename protection.
//
// The seeds catch a reader that drifted away from the manifest's spelling; the recorded key
// names catch a manifest that moved with it. A section wired for one and not the other is
// covered against half of a rename, and nothing about a manifest declares which checks it is
// subject to — so the record's presence is asserted from the check every manifest already
// calls.
//
// The single failure also states the rest of it: a missing record does not stop the rows
// from being checked, since the two are independent statements.
func TestDiscriminatingSeedCheckRequiresARecordedKeyNameList(t *testing.T) {
	t.Chdir(t.TempDir()) // no testdata directory at all, which is the case under test

	seeds := newRecordedSeeds()
	for i := range tableSpecs {
		seeds.AddRow(uint(i), probeInt, "", 7, false)
	}

	reported := capture(t, func(tb testing.TB) {
		CheckEveryRowHasADiscriminatingSeed(tb, probeSection, tableRead, tableSpecs, seeds)
	})
	msg := reported.only(t)

	if !strings.Contains(msg, "key names are not recorded") {
		t.Errorf("a section with no key-name record must be told so:\n%s", msg)
	}
	if !strings.Contains(msg, "CheckKeyNames") {
		t.Errorf("the report must name the check that is missing, or the reader has to find it:\n%s", msg)
	}
}

// TestDiscriminatingSeedCheckComparesTheRecordedNames pins the tie as a comparison rather than as
// a presence check, and pins that its report stands on its own.
//
// A record that exists says the section acquired one; it does not say the comparison is still
// being made. Deleting one CheckKeyNames call while leaving the record on disk re-greened a
// verified rename of a [state-commit] key, which makes deleting a check a way to clear a failure —
// the move the harness guide forbids for rows. So the check every seeded manifest already calls
// compares the names too.
//
// The run where that matters is the run where CheckKeyNames has just been deleted, so this report
// cannot be a pointer at it: it has to name the record and both spellings itself, or the one person
// who needs it is sent to output that will never be printed.
func TestDiscriminatingSeedCheckComparesTheRecordedNames(t *testing.T) {
	// The record holds the names as they were. The manifest and the reader have since moved
	// together, which is what editing a shared flag constant does, so the rows still discriminate
	// and the record is the only copy that did not move.
	withKeyNameRecord(t, probeSection, tableSpecs)
	renamed := renamedKey(tableSpecs, 1, "probe.b-v2")
	reader := tableReaderReading("probe.a", "probe.b-v2", "probe.c")

	seeds := newRecordedSeeds()
	for i := range renamed {
		seeds.AddRow(uint(i), probeInt, "", 7, false)
	}

	msg := capture(t, func(tb testing.TB) {
		CheckEveryRowHasADiscriminatingSeed(tb, probeSection, reader, renamed, seeds)
	}).only(t)

	for _, want := range []string{probeSection + keyNameRecordSuffix, "probe.b", "probe.b-v2", "-update"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the report does not contain %q, so it depends on a CheckKeyNames call that the "+
				"case under test is someone having deleted:\n%s", want, msg)
		}
	}
}

// TestDiscriminatingSeedCheckComparesKeysThatHaveNoRow puts the keys a manifest cannot describe
// inside the same tie.
//
// A key recorded for its name alone has no row, so nothing about it reaches the seeds; what holds
// it is that this check compares the whole record, including the names appended after the rows.
// Without that, the three [state-commit] keys with targets of their own would be protected only by
// a CheckKeyNames call that deleting re-opens — and they are the keys that decide which storage
// engine a validator commits through.
func TestDiscriminatingSeedCheckComparesKeysThatHaveNoRow(t *testing.T) {
	const rowless KeyName = "probe.write-mode"
	withKeyNameRecord(t, probeSection, tableSpecs, rowless)

	seeds := newRecordedSeeds()
	for i := range tableSpecs {
		seeds.AddRow(uint(i), probeInt, "", 7, false)
	}

	if reported := capture(t, func(tb testing.TB) {
		CheckEveryRowHasADiscriminatingSeed(tb, probeSection, tableRead, tableSpecs, seeds, rowless)
	}); len(reported.failures) != 0 {
		t.Fatalf("a record holding a key with no row was rejected:\n%s",
			strings.Join(reported.failures, "\n"))
	}

	msg := capture(t, func(tb testing.TB) {
		CheckEveryRowHasADiscriminatingSeed(tb, probeSection, tableRead, tableSpecs, seeds,
			"probe.write-mode-v2")
	}).only(t)
	for _, want := range []string{string(rowless), "probe.write-mode-v2"} {
		if !strings.Contains(msg, want) {
			t.Errorf("renaming a key that has no row must fail from here too, naming %q:\n%s", want, msg)
		}
	}
}
