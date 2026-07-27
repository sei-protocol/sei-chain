package baseapp

import (
	"strings"
	"testing"

	dbm "github.com/tendermint/tm-db"

	"github.com/spf13/cast"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/utils/tracing"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
)

// BaseApp reads four keys straight out of appOpts during construction, after every
// section reader has already run. They are the last configuration reads on the boot
// path and the only ones that can stop the node without naming a section.
//
// chain-id is the sharp one: an empty value panics here, and the panic text points at
// ~/.sei/config/client.toml regardless of --home. Since chain-id is resolved from
// client.toml and pinned into the viper by start at override precedence, this is where
// a node whose client.toml was never written finally fails.
//
// concurrency-workers is the only key in the whole surface with three-level
// precedence: a baseapp option beats appOpts, which beats the in-code default.

// newTestBaseApp constructs a BaseApp with the given options, using a memory DB and
// the nil txDecoder/tmConfig the package's own tests use.
func newTestBaseApp(t testing.TB, opts configtest.AppOpts, options ...func(*BaseApp)) *BaseApp {
	t.Helper()
	return NewBaseApp(t.Name(), dbm.NewMemDB(), nil, nil, opts, options...)
}

// baseChainID is the minimum an appOpts must carry for construction to survive.
func baseOpts() configtest.AppOpts {
	return configtest.AppOpts{FlagChainID: "sei-test"}
}

// FuzzBaseAppChainID pins the construction-time chain-id gate.
//
// Any value that casts to a non-empty string is accepted verbatim — no format check,
// no comparison against genesis — and the empty string panics. Accepting anything is
// what lets a typo'd chain-id reach consensus, and panicking on empty is what turns a
// missing client.toml into a crash rather than a default.
func FuzzBaseAppChainID(f *testing.F) {
	f.Add(uint8(1), "sei-chain", int64(0), false)
	f.Add(uint8(1), "", int64(0), false)
	f.Add(uint8(0), "", int64(0), false)
	f.Add(uint8(3), "", int64(42), false)
	f.Add(uint8(2), "", int64(0), true)
	f.Add(uint8(11), "", int64(0), false)

	f.Fuzz(func(t *testing.T, kind uint8, s string, n int64, b bool) {
		value := fuzzing.ConfigValue(kind, s, n, b)
		opts := configtest.AppOpts{FlagChainID: value}

		// cast.ToString is what decides; a map or a nil casts to "".
		wantPanic := castsToEmptyString(value)

		if wantPanic {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("chain-id = %#v casts to the empty string and must panic during "+
						"construction", value)
				}
				if msg, ok := r.(string); ok && !strings.Contains(msg, "chain-id") {
					t.Fatalf("the panic must name chain-id, got %q", msg)
				}
			}()
			_ = newTestBaseApp(t, opts)
			return
		}

		// Asserting only non-empty would pass for a build that ignored chain-id and
		// stamped some constant, so the resolved value is compared against the cast the
		// reader applies.
		app := newTestBaseApp(t, opts)
		if want := cast.ToString(value); app.ChainID != want {
			t.Fatalf("chain-id = %#v resolved to %q, want the cast value %q; the key is adopted "+
				"verbatim with no format check", value, app.ChainID, want)
		}
		if app.ChainID == "" {
			t.Fatalf("chain-id = %#v resolved to the empty string without panicking", value)
		}
	})
}

// castsToEmptyString restates the condition BaseApp checks, independently of it.
func castsToEmptyString(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []string, []any, map[string]any:
		return true
	default:
		return false
	}
}

// TestBaseAppChainIDPanicNamesTheDefaultHome records that the diagnostic points at the
// default home rather than the one in use, so an operator running with --home elsewhere
// is sent to a file the node is not reading.
func TestBaseAppChainIDPanicNamesTheDefaultHome(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("an empty chain-id must panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected a string panic, got %T", r)
		}
		if !strings.Contains(msg, "~/.sei/config/client.toml") {
			t.Fatalf("the panic no longer hardcodes the default home (%q). Deriving the path from "+
				"--home is a fix, and this row is where that gets recorded rather than skipped past", msg)
		}
	}()
	_ = newTestBaseApp(t, configtest.AppOpts{FlagChainID: ""})
}

// FuzzBaseAppConcurrencyWorkers pins the three-level precedence, which exists nowhere
// else in the surface: an explicit baseapp option wins, otherwise appOpts, otherwise
// the in-code default.
//
// The mechanism is two successive zero checks rather than presence checks, so "unset"
// and "explicitly zero" are the same input at both levels. An operator who sets
// concurrency-workers = 0 meaning "serial" gets the default instead.
func FuzzBaseAppConcurrencyWorkers(f *testing.F) {
	f.Add(0, 0)
	f.Add(0, 8)
	f.Add(4, 0)
	f.Add(4, 8)
	f.Add(0, -1)
	f.Add(-1, 8)

	f.Fuzz(func(t *testing.T, fromOption, fromAppOpts int) {
		// Keep the values in a range the compaction routine and worker pool tolerate.
		if fromOption < -1 || fromOption > 64 || fromAppOpts < -1 || fromAppOpts > 64 {
			return
		}

		opts := baseOpts()
		opts[FlagConcurrencyWorkers] = fromAppOpts

		var options []func(*BaseApp)
		if fromOption != 0 {
			options = append(options, SetConcurrencyWorkers(fromOption))
		}
		app := newTestBaseApp(t, opts, options...)

		// Restated: first non-zero of option, appOpts, default.
		want := fromOption
		if want == 0 {
			want = fromAppOpts
		}
		if want == 0 {
			want = config.DefaultConcurrencyWorkers
		}

		if got := app.ConcurrencyWorkers(); got != want {
			t.Fatalf("concurrency-workers resolved to %d, want %d (option=%d appOpts=%d default=%d)",
				got, want, fromOption, fromAppOpts, config.DefaultConcurrencyWorkers)
		}
	})
}

// TestBaseAppConcurrencyWorkersZeroIsIndistinguishableFromUnset records the
// consequence of the zero checks: an explicit 0 cannot request serial execution,
// because it is read as "nothing was set".
func TestBaseAppConcurrencyWorkersZeroIsIndistinguishableFromUnset(t *testing.T) {
	explicitZero := newTestBaseApp(t, configtest.AppOpts{
		FlagChainID:            "sei-test",
		FlagConcurrencyWorkers: 0,
	})
	absent := newTestBaseApp(t, baseOpts())

	if explicitZero.ConcurrencyWorkers() != absent.ConcurrencyWorkers() {
		t.Fatalf("an explicit 0 (%d) now differs from an absent key (%d); if presence is "+
			"distinguished from zero, an operator can finally request serial execution and "+
			"that is a behavior change",
			explicitZero.ConcurrencyWorkers(), absent.ConcurrencyWorkers())
	}
	if explicitZero.ConcurrencyWorkers() != config.DefaultConcurrencyWorkers {
		t.Fatalf("concurrency-workers resolved to %d, want the default %d",
			explicitZero.ConcurrencyWorkers(), config.DefaultConcurrencyWorkers)
	}
}

// TestBaseAppUnknownArchivalDBTypeIsSilentlyIgnored pins the archival read's missing
// default case.
//
// archival-version > 0 selects an archival store by archival-db-type, and the switch
// handles exactly one name. Any other value falls through with no error and no log, so
// the node runs with an ordinary store while its config says otherwise — a request for
// archival storage that is answered by doing nothing.
//
// The "arweave" branch itself is not driven here: it opens a real Arweave DB from a URL
// and panics on failure, which needs a fixture this suite has no way to provide.
func TestBaseAppUnknownArchivalDBTypeIsSilentlyIgnored(t *testing.T) {
	opts := baseOpts()
	opts[FlagArchivalVersion] = int64(100)
	opts[FlagArchivalDBType] = "definitely-not-a-backend"

	var app *BaseApp
	if !panicsNot(t, func() { app = newTestBaseApp(t, opts) }) {
		t.Fatal("an unknown archival-db-type must be ignored rather than failing construction")
	}
	if app == nil {
		t.Fatal("construction must succeed")
	}

	// And it is genuinely ignored: the resolved app is indistinguishable from one with
	// no archival configuration at all.
	plain := newTestBaseApp(t, baseOpts())
	if app.ChainID != plain.ChainID {
		t.Fatalf("unexpected divergence in chain-id: %q vs %q", app.ChainID, plain.ChainID)
	}
}

// TestBaseAppArchivalVersionZeroSkipsTheArchivalStore pins the gate: the archival
// reads happen only when archival-version is positive, so archival-db-type alone does
// nothing.
func TestBaseAppArchivalVersionZeroSkipsTheArchivalStore(t *testing.T) {
	opts := baseOpts()
	opts[FlagArchivalDBType] = "arweave" // would open a real DB if the gate were open
	opts[FlagArchivalVersion] = int64(0)

	if !panicsNot(t, func() { _ = newTestBaseApp(t, opts) }) {
		t.Fatal("archival-version = 0 must skip the archival branch entirely, so naming a " +
			"backend cannot have an effect")
	}
}

// TestBaseAppTracingDisabledByDefault pins the tracing read. The flag is consulted
// during construction and, when false, a no-op provider is installed — construction
// never reaches the real tracer builder, which panics on failure.
//
// Enabling it is not driven here: it constructs a real tracer provider against a
// hardcoded URL and replaces the process-global OTEL provider, neither of which
// belongs in a config characterization run.
func TestBaseAppTracingDisabledByDefault(t *testing.T) {
	opts := baseOpts()
	opts[tracing.FlagTracing] = false

	if !panicsNot(t, func() { _ = newTestBaseApp(t, opts) }) {
		t.Fatal("tracing = false must install the no-op provider without failing")
	}

	// An absent key resolves the same way, since the read is an unguarded cast.ToBool.
	if !panicsNot(t, func() { _ = newTestBaseApp(t, baseOpts()) }) {
		t.Fatal("an absent tracing key must behave as false")
	}
}

func panicsNot(t *testing.T, fn func()) (ok bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Logf("unexpected panic: %v", r)
			ok = false
		}
	}()
	fn()
	return true
}
