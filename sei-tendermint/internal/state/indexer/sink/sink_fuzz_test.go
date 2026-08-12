package sink_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/sink"
)

// tx-index.indexer is a list, and it is the one configuration key in the surface
// whose elements interact: "null" does not add a sink, it replaces the whole set.
// So an operator who appends "null" to an existing ["kv"] to "also record nothing"
// ends up with a node that indexes nothing at all, and a node whose transactions
// have stopped being searchable has a config file that still mentions kv.
//
// The selection loop ranges over a map, and it returns from inside the loop, so
// which entry it reaches first can matter. Where it does and does not is worth
// being precise about, because only one of the three cases changes what the node
// resolves:
//
//   - null alongside a *recognized* sink: the result is stable. null always wins,
//     whichever order the range yields, because both orders end at the same early
//     return. What varies is a side effect — see
//     TestNullSinkCanOpenAnIndexStoreItThenDiscards.
//   - null alongside an *unrecognized* name: the result is not stable. null returns
//     the null sink and never sees the bad name; the bad name hits the default branch
//     and fails the boot. Same file, different outcome per start.
//   - two recognized sinks, one of them unconfigured: stable. Both orders reach the
//     same error.

// memDBProvider supplies the in-memory store the kv sink needs, so the fuzz target
// exercises sink selection without touching disk.
func memDBProvider(*config.DBContext) (dbm.DB, error) { return dbm.NewMemDB(), nil }

// closeCounted records whether anything ever closed the store it wraps, which is the
// difference between a store that was opened and one that was handed back.
type closeCounted struct {
	dbm.DB
	closes *int
}

func (c closeCounted) Close() error {
	*c.closes++
	return c.DB.Close()
}

// resolvedTypes reports which sinks came back, sorted, since selection ranges over a map
// and the slice order is not fixed even when the set is.
//
// The rows assert on these types rather than on the count because a count of one is equally
// true of the null sink and of the kv sink null is supposed to have replaced: a node
// indexing every transaction and a node indexing none agree on every count.
func resolvedTypes(sinks []indexer.EventSink) []indexer.EventSinkType {
	types := make([]indexer.EventSinkType, 0, len(sinks))
	for _, s := range sinks {
		types = append(types, s.Type())
	}
	slices.Sort(types)
	return types
}

// nullAlone is what the null-wins rule promises: the null sink and nothing else.
func nullAlone() []indexer.EventSinkType { return []indexer.EventSinkType{indexer.NULL} }

// FuzzEventSinksFromConfig pins the list semantics: the null-wins rule, the
// duplicate rejection, the unknown-name rejection, and the empty-list default.
//
// The null-wins rule is order-independent, which is the part worth pinning
// precisely — iteration runs over a map, so "null" last behaves exactly like "null"
// first, and no ordering of the file can avoid it.
func FuzzEventSinksFromConfig(f *testing.F) {
	f.Add("kv")
	f.Add("null")
	f.Add("kv,null")   // null wins from second position
	f.Add("null,kv")   // and from first
	f.Add("")          // empty list: defaults to null
	f.Add("kv,kv")     // duplicate: rejected
	f.Add("KV")        // case-folded before matching
	f.Add("kv,psql")   // psql with no connection string: rejected
	f.Add("bogus")     // unknown: rejected
	f.Add("null,null") // duplicate null still counts as a duplicate

	f.Fuzz(func(t *testing.T, list string) {
		var names []string
		if list != "" {
			names = strings.Split(list, ",")
		}

		cfg := config.DefaultConfig()
		cfg.TxIndex.Indexer = names

		sinks, err := sink.EventSinksFromConfig(cfg, memDBProvider, "sei-test")

		// Restate the rules independently of the implementation.
		lowered := make([]string, 0, len(names))
		for _, s := range names {
			lowered = append(lowered, strings.ToLower(s))
		}
		hasDuplicate := false
		seen := map[string]bool{}
		for _, s := range lowered {
			if seen[s] {
				hasDuplicate = true
			}
			seen[s] = true
		}

		switch {
		case len(names) == 0:
			// An absent or empty list is not an error: the node gets the null sink,
			// so transactions are simply not indexed.
			if err != nil {
				t.Fatalf("an empty indexer list must default to the null sink, got %v", err)
			}
			if got := resolvedTypes(sinks); !slices.Equal(got, nullAlone()) {
				t.Fatalf("an empty indexer list resolved to %v, want the null sink alone", got)
			}
			return

		case hasDuplicate:
			if err == nil {
				t.Fatalf("indexer = %v repeats a sink and must be rejected", names)
			}
			return

		case slices.Contains(lowered, "null"):
			// null discards the other sinks, but only reliably when every other
			// entry is a name the switch recognizes. A list mixing null with an
			// unsupported name resolves nondeterministically — see
			// TestNullMixedWithAnUnsupportedSinkIsUnspecified — so it is out of
			// scope here rather than asserted either way.
			for _, s := range lowered {
				if s != "null" && s != "kv" && s != "psql" {
					return
				}
			}
			// psql without a connection string fails even alongside null, again
			// depending on which entry the map yields first.
			if slices.Contains(lowered, "psql") {
				return
			}
			if err != nil {
				t.Fatalf("indexer = %v contains null alongside supported names and must resolve to "+
					"the null sink, got %v", names, err)
			}
			if got := resolvedTypes(sinks); !slices.Equal(got, nullAlone()) {
				t.Fatalf("indexer = %v contains null, so every other sink must be discarded; resolved "+
					"to %v", names, got)
			}
			return
		}

		// No null, no duplicates: every remaining name must be a supported sink,
		// and psql additionally needs a connection string it does not have here.
		supported := true
		for _, s := range lowered {
			if s != "kv" && s != "psql" {
				supported = false
			}
		}
		if !supported || slices.Contains(lowered, "psql") {
			if err == nil {
				t.Fatalf("indexer = %v must be rejected (unsupported name, or psql with no conn)", names)
			}
			return
		}
		if err != nil {
			t.Fatalf("indexer = %v is all-supported and must be accepted, got %v", names, err)
		}
		want := make([]indexer.EventSinkType, 0, len(lowered))
		for _, s := range lowered {
			want = append(want, indexer.EventSinkType(s))
		}
		slices.Sort(want)
		if got := resolvedTypes(sinks); !slices.Equal(got, want) {
			t.Fatalf("indexer = %v resolved to %v, want %v", names, got, want)
		}
	})
}

// TestNullMixedWithAnUnsupportedSinkIsUnspecified pins the one place in the
// configuration surface where a static file does not determine the outcome.
//
// EventSinksFromConfig loops over a map, and Go randomizes map iteration order. When
// the list holds "null" and an unrecognized name, whichever the range yields first
// decides the run: "null" returns the null sink and never sees the bad name, while the
// bad name hits the default branch and fails the boot. Same config.toml, same binary,
// different result per start.
//
// A node with a typo'd sink alongside "null" therefore starts most of the time and
// crashloops occasionally, which is the hardest possible shape to diagnose. The test
// asserts both outcomes occur so the nondeterminism cannot be quietly resolved in one
// direction without a decision — and if it is resolved deliberately, this row is where
// that gets recorded.
//
// The run count is sized so that a minority branch of the order this row was written against
// is missed with negligible probability, and a one-sided sample escalates rather than failing
// outright, so the exact split does not have to hold for the row to stay correct. The split
// comes from where the two names land: a two-key map puts the first-listed one in the first of
// its eight slots and iteration starts at a slot chosen at random, so the second name is
// reached first one time in eight. Measured 12.8% over 4000 runs. Treat that as the origin of
// the sizing, not as an invariant the code depends on.
func TestNullMixedWithAnUnsupportedSinkIsUnspecified(t *testing.T) {
	const runs = 250
	booted, failed := countMixedOutcomes(runs)
	if booted == 0 || failed == 0 {
		resolveOneSidedOutcome(t, "boots / boot-failures", runs, booted, failed, countMixedOutcomes)
	}
}

// countMixedOutcomes resolves the ["null", <unknown>] list n times and reports how many runs
// booted and how many failed. Shared with the escalation path so the larger sample is drawn
// exactly the same way as the first one.
func countMixedOutcomes(n int) (booted, failed int) {
	for range n {
		cfg := config.DefaultConfig()
		cfg.TxIndex.Indexer = []string{"null", "definitely-not-a-sink"}
		if _, err := sink.EventSinksFromConfig(cfg, memDBProvider, "sei-test"); err != nil {
			failed++
		} else {
			booted++
		}
	}
	return booted, failed
}

// resolveOneSidedOutcome decides what a one-sided result means and reports accordingly.
//
// Both probabilistic rows characterize nondeterminism, so both rest on a runtime property Go
// does not promise: map iteration is randomized by design, but nothing guarantees it for a
// two-element map. A one-sided result therefore has three causes, and only one of them is a
// finding about sei.
//
//  1. Selection became deterministic. That is the finding these rows exist to force into the
//     open, and it fails.
//  2. The runtime stopped randomizing maps this small. Nothing to do with sei, so it skips.
//  3. The split skewed far enough that the sample missed the minority branch. The
//     characterization still holds, so it passes with the skew reported.
//
// Case 3 is why this escalates rather than judging the first sample. The probe distinguishes
// 2 from the others, but it cannot separate 1 from 3: at 250 runs a collapsed-but-nonzero
// split and a deterministic outcome look identical. Drawing a much larger sample separates
// them, and it costs nothing in the common case because it only runs when a row is about to
// fail. Without it a split moving from the measured one-in-eight to a fraction of a percent
// would redden the shard while blaming a change in selection that never happened.
//
// label names what the two counts mean, because the callers count different things.
func resolveOneSidedOutcome(t *testing.T, label string, runs, majority, minority int, resample func(int) (int, int)) {
	t.Helper()

	if !smallMapIterationVaries(runs) {
		t.Skipf("this runtime started a two-element map range at the same key in all %d probes, so "+
			"it no longer randomizes maps this small and the mechanism this row characterizes does "+
			"not exist here (%s: %d / %d). Sink selection itself is unchanged: rewrite the row "+
			"against whatever ordering the runtime now guarantees", runs, label, majority, minority)
	}

	// The runtime still supplies the mechanism, so this is sei's own behavior. Draw a larger
	// sample before calling it deterministic.
	const escalated = 20000
	bigMajority, bigMinority := resample(escalated)
	if bigMinority > 0 && bigMajority > 0 {
		t.Logf("%s was one-sided over %d runs (%d / %d) but two-sided over %d (%d / %d), so the "+
			"outcome is still undetermined by the config and this row's premise holds. The split "+
			"has skewed far below the minority share this row was sized against; if it keeps "+
			"falling, raise the run count rather than treating a miss as a fix",
			label, runs, majority, minority, escalated, bigMajority, bigMinority)
		return
	}
	t.Errorf("%s stayed one-sided over %d runs (%d / %d) and again over %d (%d / %d). The "+
		"map-order probe still varies, so the runtime is not the cause and sink selection has "+
		"become deterministic",
		label, runs, majority, minority, escalated, bigMajority, bigMinority)
}

// smallMapIterationVaries reports whether this runtime still starts a two-element map range
// at a varying key, which is the mechanism both probabilistic rows depend on.
//
// The split is not close to even in practice: the two keys sit in the first two of eight slots
// and the range starts at a slot chosen at random, so it runs 7 to 1. That is
// enough to answer "does the order vary at all", which is all this is asked, and it is not a
// yardstick for the sink's own split.
func smallMapIterationVaries(runs int) bool {
	probe := map[string]struct{}{"a": {}, "b": {}}
	starts := map[string]int{}
	for range runs {
		for k := range probe {
			starts[k]++
			break // only which key comes first matters
		}
	}
	return len(starts) > 1
}

// TestEventSinksDistinguishesADuplicateFromAnUnsupportedName pins the diagnostic
// distinction between the two rejection reasons without pinning either one's wording, for
// the same reason as the mode rows in sei-tendermint/config: the guide asks for
// errors.Is/As, the production site builds bare errors with no identity to match on, and
// giving it one means editing production code this PR does not touch. Tracked as PLT-855, so
// the exemption is a recorded decision rather than a precedent set in a comment.
//
// The distinction matters because the two mistakes have different fixes. A duplicate means
// remove a line; an unsupported name means correct a spelling.
func TestEventSinksDistinguishesADuplicateFromAnUnsupportedName(t *testing.T) {
	duplicate := config.DefaultConfig()
	duplicate.TxIndex.Indexer = []string{"kv", "kv"}
	unsupported := config.DefaultConfig()
	unsupported.TxIndex.Indexer = []string{"definitely-not-a-sink"}

	_, duplicateErr := sink.EventSinksFromConfig(duplicate, memDBProvider, "sei-test")
	_, unsupportedErr := sink.EventSinksFromConfig(unsupported, memDBProvider, "sei-test")
	if duplicateErr == nil || unsupportedErr == nil {
		t.Fatalf("both a duplicate and an unsupported name must be rejected, got %v and %v",
			duplicateErr, unsupportedErr)
	}
	// Inequality alone is a proxy, for the same reason as the mode rows: one template
	// interpolating the offending name would satisfy it while collapsing the diagnosis. Until
	// PLT-855 adds sentinels there is no identity to match on, so the two reasons are pinned by
	// the one word each has to carry. That is narrower than wording-matching and it is the
	// distinction an operator acts on: remove a line, or fix a spelling.
	if !strings.Contains(duplicateErr.Error(), "duplicat") {
		t.Fatalf("the duplicate-sink failure (%v) no longer says the name was repeated", duplicateErr)
	}
	if !strings.Contains(unsupportedErr.Error(), "unsupported") {
		t.Fatalf("the unsupported-sink failure (%v) no longer says the name was unrecognized",
			unsupportedErr)
	}
	if duplicateErr.Error() == unsupportedErr.Error() {
		t.Fatalf("a repeated sink and an unrecognized one now report the same failure (%v), so an "+
			"operator cannot tell whether to remove a line or fix a spelling", duplicateErr)
	}
}

// TestNullSinkAlwaysWinsOverARecognizedSink pins the stable half of the null rule.
// Mixed with kv, the resolved set is the null sink alone on every run — the map order
// cannot change it, because both orders end at the same early return. Repeating the
// call is what makes "always" an assertion rather than a hope.
func TestNullSinkAlwaysWinsOverARecognizedSink(t *testing.T) {
	const runs = 250
	for _, list := range [][]string{{"kv", "null"}, {"null", "kv"}} {
		for range runs {
			cfg := config.DefaultConfig()
			cfg.TxIndex.Indexer = list

			sinks, err := sink.EventSinksFromConfig(cfg, memDBProvider, "sei-test")
			if err != nil {
				t.Fatalf("indexer = %v: %v", list, err)
			}
			if got := resolvedTypes(sinks); !slices.Equal(got, nullAlone()) {
				t.Fatalf("indexer = %v resolved to %v; null must discard the kv sink", list, got)
			}
		}
	}

	// kv alone does produce a kv sink, so the assertion above is about null rather than
	// about kv never working — and it is the case the null rows have to be distinguished
	// from, since a node that indexes everything and one that indexes nothing agree on
	// every count.
	cfg := config.DefaultConfig()
	cfg.TxIndex.Indexer = []string{"kv"}
	sinks, err := sink.EventSinksFromConfig(cfg, memDBProvider, "sei-test")
	if err != nil {
		t.Fatalf("indexer = [kv]: %v", err)
	}
	if got := resolvedTypes(sinks); !slices.Equal(got, []indexer.EventSinkType{indexer.KV}) {
		t.Fatalf("indexer = [kv] resolved to %v, want the kv sink alone", got)
	}
}

// TestNullSinkCanOpenAnIndexStoreItThenDiscards records the side effect the stable
// result hides.
//
// If the range reaches kv first, the kv branch calls dbProvider, opens the tx_index store
// and appends the sink — and then null is reached, returns a fresh one-element slice, and
// nothing ever closes what was opened. The resolved config is identical either way, so the
// leak is invisible from the configuration: it depends only on map order.
//
// The discarded store is not garbage either. On the production provider it is a goleveldb
// handle, and opening one starts five goroutines that hold a reference to it for the life of
// the process — session.refLoop, DB.compactionError, DB.mpoolDrain, DB.tCompaction,
// DB.mCompaction — so the memory is not reclaimable and the exclusive flock is never
// released. A second open of the same path in the same process fails with
// "resource temporarily unavailable", and a data/tendermint/tx_index.db directory with a
// LOCK, a CURRENT, a MANIFEST and a write-ahead log materialises on a node configured to
// index nothing.
//
// Both list orders are driven, because they are not the same measurement. A two-key map puts
// the first-listed name in the first bucket slot and iteration starts at a random slot, so
// the first name listed is reached first roughly seven times in eight: ["kv","null"] opens a
// store on about 7/8 of boots and ["null","kv"] on about 1/8. ["kv","null"] is the order an
// operator produces by appending null to an existing list, so it is the common case and it
// is the worse one.
//
// It is asserted as "sometimes" because that is what it is. The point is that the config does
// not determine whether a store is opened, which is the fact a replacement manager needs to
// know before it reuses this selection logic.
func TestNullSinkCanOpenAnIndexStoreItThenDiscards(t *testing.T) {
	const runs = 250

	// countOpens resolves the given list n times and reports how often the kv branch opened a
	// store first. It also holds the two invariants that must be true on every run, whichever
	// order the range takes: exactly the null sink comes back, and nothing closes a store the
	// kv branch opened.
	countOpens := func(list []string) func(int) (quiet, opened int) {
		return func(n int) (quiet, opened int) {
			for range n {
				opens, closes := 0, 0
				provider := func(*config.DBContext) (dbm.DB, error) {
					opens++
					return closeCounted{DB: dbm.NewMemDB(), closes: &closes}, nil
				}
				cfg := config.DefaultConfig()
				cfg.TxIndex.Indexer = list

				sinks, err := sink.EventSinksFromConfig(cfg, provider, "sei-test")
				if err != nil {
					t.Fatalf("indexer = %v: %v", list, err)
				}
				if got := resolvedTypes(sinks); !slices.Equal(got, nullAlone()) {
					t.Fatalf("indexer = %v resolved to %v, want the null sink alone", list, got)
				}
				// Counting the opens alone cannot see the leak: it stays true of a build that
				// closes what it discards. Closing it is the fix, and this is where that has to
				// be recorded rather than merged as a green diff.
				if closes > 0 {
					t.Fatalf("indexer = %v: the discarded store was closed %d times. Selection now "+
						"releases what it opens, which is a fix — record it here, and drop the leak "+
						"this row describes", list, closes)
				}
				if opens > 0 {
					opened++
				} else {
					quiet++
				}
			}
			return quiet, opened
		}
	}

	for _, list := range [][]string{{"kv", "null"}, {"null", "kv"}} {
		count := countOpens(list)
		quiet, opened := count(runs)
		t.Logf("indexer = %v: %d of %d runs opened a store", list, opened, runs)
		if opened == 0 || quiet == 0 {
			// Both directions are one-sided results of the same coin, so the shared resolver
			// decides between a deterministic selection, a runtime that stopped randomizing, and a
			// split that skewed past this sample.
			resolveOneSidedOutcome(t, fmt.Sprintf("indexer = %v: runs without a store opened / runs "+
				"with one", list), runs, quiet, opened, count)
		}
	}
}
