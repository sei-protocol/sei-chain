package sink_test

import (
	"slices"
	"strings"
	"testing"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
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
		var indexer []string
		if list != "" {
			indexer = strings.Split(list, ",")
		}

		cfg := config.DefaultConfig()
		cfg.TxIndex.Indexer = indexer

		sinks, err := sink.EventSinksFromConfig(cfg, memDBProvider, "sei-test")

		// Restate the rules independently of the implementation.
		lowered := make([]string, 0, len(indexer))
		for _, s := range indexer {
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
		case len(indexer) == 0:
			// An absent or empty list is not an error: the node gets the null sink,
			// so transactions are simply not indexed.
			if err != nil {
				t.Fatalf("an empty indexer list must default to the null sink, got %v", err)
			}
			if len(sinks) != 1 {
				t.Fatalf("an empty indexer list must yield exactly one sink, got %d", len(sinks))
			}
			return

		case hasDuplicate:
			if err == nil {
				t.Fatalf("indexer = %v repeats a sink and must be rejected", indexer)
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
					"the null sink, got %v", indexer, err)
			}
			if len(sinks) != 1 {
				t.Fatalf("indexer = %v contains null, so every other sink must be discarded; got %d sinks",
					indexer, len(sinks))
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
				t.Fatalf("indexer = %v must be rejected (unsupported name, or psql with no conn)", indexer)
			}
			return
		}
		if err != nil {
			t.Fatalf("indexer = %v is all-supported and must be accepted, got %v", indexer, err)
		}
		if len(sinks) != len(lowered) {
			t.Fatalf("indexer = %v resolved to %d sinks, want %d", indexer, len(sinks), len(lowered))
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
func TestNullMixedWithAnUnsupportedSinkIsUnspecified(t *testing.T) {
	const runs = 1000
	booted, failed := 0, 0
	for range runs {
		cfg := config.DefaultConfig()
		cfg.TxIndex.Indexer = []string{"null", "definitely-not-a-sink"}
		if _, err := sink.EventSinksFromConfig(cfg, memDBProvider, "sei-test"); err != nil {
			failed++
		} else {
			booted++
		}
	}
	if booted == 0 || failed == 0 {
		t.Fatalf("over %d runs the mixed list resolved consistently (%d booted, %d failed). "+
			"Map iteration order no longer decides the outcome, which is far more likely a "+
			"deliberate fix than a break: checking for null before kv opens a store is exactly "+
			"what the sibling row argues for. If you landed that, update this row and "+
			"TestNullSinkCanOpenAnIndexStoreItThenDiscards to assert the deterministic outcome",
			runs, booted, failed)
	}
}

// TestEventSinksDistinguishesADuplicateFromAnUnsupportedName pins the diagnostic
// distinction between the two rejection reasons without pinning either one's wording, for
// the same reason as the mode rows in sei-tendermint/config: the guide asks for
// errors.Is/As, the production site builds bare errors with no identity to match on, and
// giving it one means editing production code this PR does not touch.
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
	const runs = 400
	for _, list := range [][]string{{"kv", "null"}, {"null", "kv"}} {
		for range runs {
			cfg := config.DefaultConfig()
			cfg.TxIndex.Indexer = list

			sinks, err := sink.EventSinksFromConfig(cfg, memDBProvider, "sei-test")
			if err != nil {
				t.Fatalf("indexer = %v: %v", list, err)
			}
			if len(sinks) != 1 {
				t.Fatalf("indexer = %v resolved to %d sinks; null must discard the kv sink",
					list, len(sinks))
			}
		}
	}

	// kv alone does produce a sink, so the assertion above is about null rather than
	// about kv never working.
	cfg := config.DefaultConfig()
	cfg.TxIndex.Indexer = []string{"kv"}
	sinks, err := sink.EventSinksFromConfig(cfg, memDBProvider, "sei-test")
	if err != nil {
		t.Fatalf("indexer = [kv]: %v", err)
	}
	if len(sinks) != 1 {
		t.Fatalf("indexer = [kv] resolved to %d sinks, want 1", len(sinks))
	}
}

// TestNullSinkCanOpenAnIndexStoreItThenDiscards records the side effect the stable
// result hides.
//
// With ["null","kv"], if the range reaches kv first the kv branch calls dbProvider,
// opens the tx_index store and appends the sink — and then null is reached, returns a
// fresh one-element slice, and the opened store goes out of scope with nothing left
// holding it to close. The resolved config is identical either way, so the leak is
// invisible from the configuration: it depends only on map order.
//
// It is asserted as "sometimes" because that is what it is. The point is that the
// config does not determine whether a store is opened, which is the fact a
// replacement manager needs to know before it reuses this selection logic.
func TestNullSinkCanOpenAnIndexStoreItThenDiscards(t *testing.T) {
	const runs = 400
	opens := 0
	for range runs {
		provider := func(*config.DBContext) (dbm.DB, error) {
			opens++
			return dbm.NewMemDB(), nil
		}
		cfg := config.DefaultConfig()
		cfg.TxIndex.Indexer = []string{"null", "kv"}

		sinks, err := sink.EventSinksFromConfig(cfg, provider, "sei-test")
		if err != nil {
			t.Fatalf("indexer = [null kv]: %v", err)
		}
		if len(sinks) != 1 {
			t.Fatalf("indexer = [null kv] resolved to %d sinks, want the null sink alone", len(sinks))
		}
	}

	if opens == 0 {
		t.Fatalf("over %d runs the kv branch never ran before null returned. If the loop now "+
			"checks for null before opening anything, that removes an orphaned store and is "+
			"worth recording as the fix it is", runs)
	}
	if opens == runs {
		t.Fatalf("over %d runs the kv branch ran every time; null is supposed to short-circuit "+
			"whenever the range reaches it first", runs)
	}
}
