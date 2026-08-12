package baseapp

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/utils/tracing"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	"github.com/spf13/cast"
	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
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
	f.Add(fuzzing.KindString, "sei-chain", int64(0), false)
	f.Add(fuzzing.KindString, "", int64(0), false)
	f.Add(fuzzing.KindNil, "", int64(0), false)
	f.Add(fuzzing.KindInt64, "", int64(42), false)
	f.Add(fuzzing.KindBool, "", int64(0), true)
	f.Add(fuzzing.KindMap, "", int64(0), false)

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

	// And it is genuinely ignored: the commit store must carry no archival wiring at all.
	//
	// Comparing ChainID proved nothing on its own, since both apps take it from the same
	// fixture. The archival branch's only effect is to replace the commit multistore with one
	// built by NewStoreWithArchival, so that is what has to be checked.
	plain := newTestBaseApp(t, baseOpts())
	if wired, version := archivalWiring(t, app.cms); wired || version != 0 {
		t.Fatalf("an unknown archival-db-type produced an archival store (db wired=%v, version=%d). "+
			"It must be ignored rather than partially wired, or a node names a backend that does "+
			"not exist and silently gets archival behavior anyway", wired, version)
	}
	if wired, version := archivalWiring(t, plain.cms); wired || version != 0 {
		t.Fatalf("the control app has archival wiring (db wired=%v, version=%d), so this row cannot "+
			"tell the two apart", wired, version)
	}
	if app.ChainID != plain.ChainID {
		t.Fatalf("unexpected divergence in chain-id: %q vs %q", app.ChainID, plain.ChainID)
	}

	// A positive control, so the reader above is known to detect wiring rather than to always
	// report none. Without it the two assertions could both pass on a reader that never sees
	// anything, which is the shape of the check this replaced.
	archival := store.NewCommitMultiStoreWithArchival(dbm.NewMemDB(), dbm.NewMemDB(), 100)
	if wired, version := archivalWiring(t, archival); !wired || version != 100 {
		t.Fatalf("archivalWiring cannot see a store that is definitely archival (wired=%v, "+
			"version=%d); the assertions above prove nothing until it can", wired, version)
	}
}

// archivalWiring reports whether a commit multistore was built with an archival database,
// and at what version.
//
// rootmulti keeps both on unexported fields and exposes no accessor, so they are read
// reflectively. Reading is legal on an unexported field even though Interface() is not, and
// adding an accessor would mean changing production code this PR does not touch. The row
// above pairs this with a positive control, since a reflective reader that quietly stopped
// finding the fields would otherwise report "no archival wiring" forever.
func archivalWiring(t *testing.T, cms sdk.CommitMultiStore) (wired bool, version int64) {
	t.Helper()

	rs := reflect.ValueOf(cms)
	if rs.Kind() != reflect.Ptr || rs.IsNil() {
		t.Fatalf("commit multistore is %T, want a pointer to rootmulti.Store", cms)
	}
	rs = rs.Elem()

	db := rs.FieldByName("archivalDb")
	ver := rs.FieldByName("archivalVersion")
	if !db.IsValid() || !ver.IsValid() {
		t.Fatalf("rootmulti.Store no longer has archivalDb/archivalVersion; this reader needs "+
			"updating before the row above means anything (type %T)", cms)
	}
	return !db.IsNil(), ver.Int()
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
// The property is that nothing traces, so that is what is asserted. Surviving construction
// does not show it: the builder panics only when the exporter cannot be created, and an
// unreachable Jaeger endpoint creates fine, so a build that installed a real provider would
// construct just as quietly. Both things the read controls are checked — the flag it stores on
// TracingInfo, which gates every ABCI path, and the process-global provider it installs for
// every otel caller in the process.
//
// A live span context is checked alongside IsRecording because IsRecording alone cannot tell
// the no-op provider from a real Jaeger provider whose sampler is off. Both report false,
// while the second has already started the exporter goroutine, so a build that traded the flag
// for a sampler would pass on IsRecording. SpanContext().IsValid() is false for the no-op
// provider and true for any SDK provider whatever its sampler.
//
// TestBaseAppTracingEnabledInstallsAProviderNothingShutsDown drives the other value.
func TestBaseAppTracingDisabledByDefault(t *testing.T) {
	opts := baseOpts()
	opts[tracing.FlagTracing] = false
	checkNothingTraces(t, opts, "tracing = false")

	// An absent key resolves the same way, since the read is an unguarded cast.ToBool.
	checkNothingTraces(t, baseOpts(), "an absent tracing key")

	// A positive control, so the two signals above are known to distinguish the cases rather
	// than to always report false, for the same reason archivalWiring is paired with one. A
	// provider with no exporter still records, and it still mints a span context. The sampler
	// is passed explicitly because the SDK applies OTEL_TRACES_SAMPLER first and a passed
	// option overrides it: without this, always_off in the environment reddens the shard here
	// and blames sei for it.
	control := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	_, span := control.Tracer("component-main").Start(context.Background(), "control")
	if !span.IsRecording() || !span.SpanContext().IsValid() {
		t.Fatalf("a provider that definitely samples reported recording=%v valid=%v (%T); the "+
			"assertions above prove nothing until both hold", span.IsRecording(),
			span.SpanContext().IsValid(), span)
	}
}

// checkNothingTraces constructs an app from opts and asserts it traces nowhere, on two signals:
// what TracingInfo hands back, and the process-global provider.
//
// The first covers the flag, not the tracer. Info.Start returns the tracing package's NoOpSpan
// on !tracingEnabled.Load() and never reaches i.tracer, so what it pins is that construction
// stored a false flag — the gate every ABCI path passes through, which is the property. The
// tracer stored beside it is deferred rather than covered: behind that short-circuit a live one
// cannot produce a span either way. If the short-circuit ever goes, this signal starts covering
// the tracer, and that is the change to notice here.
//
// It captures the global before constructing so the failure can name both values. Reading it
// after is sound because construction installs the no-op provider unconditionally before
// consulting the flag, so the window between that install and this read is the whole exposure.
// A build that dropped the unconditional install would make the same read report a provider
// some earlier row left behind, which is why the message says the global traces after
// construction rather than that construction installed it.
//
// It is also why no row in this file calls t.Parallel. Being the only file to abstain is not
// enough on its own: TestBaseAppCreateQueryContext and
// TestCreateQueryContextUsesCheckStateBeforeFirstCommit do call it, and they construct
// BaseApps that set the same global. What keeps them out of the window is that Go parks a
// top-level parallel test until every sequential top-level test in the binary has finished, so
// a parallel row cannot overlap a sequential one. A row here that called t.Parallel would join
// that pool and give the guarantee up.
func checkNothingTraces(t *testing.T, opts configtest.AppOpts, what string) {
	t.Helper()

	globalBefore := otel.GetTracerProvider()

	var app *BaseApp
	if !panicsNot(t, func() { app = newTestBaseApp(t, opts) }) {
		t.Fatalf("%s: construction must install the no-op provider without failing", what)
	}
	if app == nil {
		t.Fatalf("%s: construction must succeed", what)
	}
	if app.TracingInfo == nil {
		t.Fatalf("%s: the app carries no TracingInfo, so nothing here can tell whether tracing "+
			"is on", what)
	}
	if _, span := app.TracingInfo.Start("component-main"); span.IsRecording() || span.SpanContext().IsValid() {
		t.Errorf("%s: the app handed back a live span through TracingInfo (%T, recording=%v "+
			"valid=%v), so every ABCI path allocates and records a span for a node that asked for "+
			"none", what, span, span.IsRecording(), span.SpanContext().IsValid())
	}
	global := otel.GetTracerProvider()
	_, span := global.Tracer("component-main").Start(context.Background(), "probe")
	if span.IsRecording() || span.SpanContext().IsValid() {
		t.Errorf("%s: the process-global tracer provider traces after construction (%T, "+
			"recording=%v valid=%v; it was %T before), so every otel caller in the process gets a "+
			"live tracer for a node that asked for none", what, global, span.IsRecording(),
			span.SpanContext().IsValid(), globalBefore)
	}
	// And a Set is known to have happened, because otherwise the assertion above is a
	// tautology. otel's untouched global is a delegating placeholder whose Start returns a
	// non-recording span with no span context, so a build that dropped the unconditional
	// otel.SetTracerProvider call would read false on both signals forever while leaving
	// whatever the process already had in place.
	if _, ok := global.(noop.TracerProvider); !ok {
		t.Errorf("%s: the process-global provider is %T, not the no-op provider construction "+
			"installs. Installing a different one is a behavior change to record here; installing "+
			"none leaves the assertion above unable to fail", what, global)
	}
}

// TestBaseAppTracingEnabledInstallsAProviderNothingShutsDown drives the other value of the
// flag and pins the two things construction leaves behind.
//
// tracing = true builds a real Jaeger provider with trace.WithBatcher, which starts one
// background goroutine that stops only on Shutdown. NewBaseApp never calls it: the enabled
// branch declares its own tp with :=, shadowing the one the function opened with. The provider
// stays referenced — the tracer it hands TracingInfo holds it on an unexported field, and the
// provider's own named-tracer map holds that tracer — but no exported handle reaches it, which
// is why this row goes through otel.GetTracerProvider to clean up what construction started. A
// node with tracing on therefore pays one goroutine per BaseApp for the life of the process, and
// nothing in baseapp gives it back: app.Close, production's lifecycle close, releases the db, the
// commit store, the snapshot manager and the close handler, and never touches the provider. This
// row drives Close so that claim is falsifiable rather than narrated — see the assertion below.
//
// A live span context is asserted rather than IsRecording because the provider is built with
// no sampler option, so OTEL_TRACES_SAMPLER in the environment decides whether it records.
// Whether a real provider was installed is the property; whether it samples is not, and
// making the row depend on it would hand a stray environment variable a red shard.
//
// No span is ended, so nothing is ever enqueued and nothing here touches the network: the
// collector exporter is a struct holding an http.Client it has not called yet, so neither
// construction nor Shutdown opens a connection and an unreachable collector is not a
// dependency of this row.
func TestBaseAppTracingEnabledInstallsAProviderNothingShutsDown(t *testing.T) {
	globalBefore := otel.GetTracerProvider()
	// Registered first so it runs last, because the Shutdown registered below has to precede it.
	// Restoring the handle is not all of what construction changed: otel wires the placeholder
	// global's delegate to the first real provider it is handed and never unwires it, so after
	// this restore the placeholder still resolves tracers through this row's provider. That is
	// harmless only because a provider that has been shut down hands out no-op tracers — which
	// is why the Shutdown is a Cleanup rather than the last statement of the body, where a
	// Fatalf above it would skip it and leave the process global delegating to a live provider.
	t.Cleanup(func() { otel.SetTracerProvider(globalBefore) })
	exportersBefore := batchSpanProcessorGoroutines()

	opts := baseOpts()
	opts[tracing.FlagTracing] = true

	var app *BaseApp
	if !panicsNot(t, func() { app = newTestBaseApp(t, opts) }) {
		t.Fatal("tracing = true must construct without failing: the exporter is created rather " +
			"than connected, so an unreachable collector cannot stop a node booting")
	}
	if app == nil || app.TracingInfo == nil {
		t.Fatal("construction must succeed and carry TracingInfo")
	}

	if _, span := app.TracingInfo.Start("component-main"); !span.SpanContext().IsValid() {
		t.Errorf("tracing = true left the app's tracer on the no-op provider (%T), so no ABCI path "+
			"would produce a span on a node that asked for tracing", span)
	}
	global := otel.GetTracerProvider()
	if _, span := global.Tracer("component-main").Start(context.Background(), "probe"); !span.SpanContext().IsValid() {
		t.Errorf("tracing = true left the process-global provider on the no-op one (%T), so every "+
			"otel caller outside baseapp would keep dropping spans", global)
	}

	// The leak as a measurement rather than a note.
	provider, ok := global.(*sdktrace.TracerProvider)
	if !ok {
		t.Fatalf("the process-global provider is %T, so this row has no handle to shut down what "+
			"construction installed and would leak it into every later row", global)
	}
	t.Cleanup(func() {
		// Bounded, because Shutdown flushes through the exporter and this row must not be able to
		// hang a shard on a collector endpoint that neither refuses nor answers.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := provider.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown of the provider construction installed failed (%v), so the goroutine "+
				"it started cannot be given back and this row leaks it into the rest of the binary", err)
		}
		if got := waitForBatchSpanProcessorGoroutines(exportersBefore) - exportersBefore; got != 0 {
			t.Errorf("%d batch-processor goroutines outlived Shutdown, so this row leaks what it "+
				"created", got)
		}
	})

	leaked := waitForBatchSpanProcessorGoroutines(exportersBefore+1) - exportersBefore
	if leaked != 1 {
		t.Errorf("tracing = true started %d batch-processor goroutines, want 1. Zero has four causes "+
			"and this line cannot tell them apart: construction now shuts the provider down, the "+
			"provider is built with WithSyncer, it is built with no span-processor option at all, or "+
			"the SDK renamed the frame the counter matches — which is what the control at the end of "+
			"this row separates from the other three. More than one means the enabled branch builds "+
			"more than one batcher. Each is a change to record here rather than a number to widen; "+
			"while it stays at one, this is what a node with tracing on pays for the life of the "+
			"process", leaked)
	}

	// The name's claim, driven rather than read off the shape of the code. Close is production's
	// only lifecycle close and it does not release the provider, so the count has to survive it.
	// A build that unshadowed the enabled branch's tp, kept the handle on BaseApp and shut it down
	// here — the fix this row's own message asks for — reddens the two assertions below. Without
	// driving Close, nothing in this row can see that fix land.
	if err := app.Close(); err != nil {
		t.Fatalf("app.Close failed (%v), so this row cannot say whether Close releases the provider",
			err)
	}
	// Asked of the provider rather than only of the goroutine count, because a shut-down provider
	// answers Tracer with a no-op one the moment its flag flips, while its goroutine takes a
	// moment to unwind. The delta below measures the cost; this decides the question.
	if _, span := provider.Tracer("component-main").Start(context.Background(), "after-close"); !span.SpanContext().IsValid() {
		t.Errorf("app.Close shut the tracer provider down (its tracer is now %T), so a node with "+
			"tracing on stops paying for it at shutdown. That is the fix this row exists to record — "+
			"rewrite the row against the new lifecycle rather than deleting this line", span)
	}
	// Compared against the count taken before Close rather than against a literal 1, so that a
	// build which never started a batch processor reddens only the assertion above — the one whose
	// message names that cause — instead of also being reported here as a change to Close.
	if got := batchSpanProcessorGoroutines() - exportersBefore; got != leaked {
		t.Errorf("app.Close took the batch-processor delta from %d to %d: Close now releases the "+
			"tracer provider, so a node with tracing on stops paying for it at shutdown. That is the "+
			"fix this row exists to record — rewrite the row against the new lifecycle rather than "+
			"deleting this line", leaked, got)
	}

	// A positive control, so the counter is known to see a batch processor rather than to always
	// report none, for the same reason archivalWiring and the disabled row's two signals are each
	// paired with one. It is what separates a renamed SDK frame from a change in sei: on a rename
	// this fails too, and on a change in sei only the assertions above do.
	controlBefore := batchSpanProcessorGoroutines()
	control := sdktrace.NewTracerProvider(sdktrace.WithBatcher(tracetest.NewInMemoryExporter()))
	if got := waitForBatchSpanProcessorGoroutines(controlBefore+1) - controlBefore; got != 1 {
		t.Errorf("a provider built with exactly one batcher moved the count by %d, want 1. The frame "+
			"batchSpanProcessorGoroutines matches no longer names a batch-processor goroutine, so "+
			"every count in this row reads zero and none of its assertions can fail", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := control.Shutdown(ctx); err != nil {
		t.Errorf("shutting down the control provider failed (%v), so this row leaks the goroutine it "+
			"started to prove the counter works", err)
	}
	if got := waitForBatchSpanProcessorGoroutines(controlBefore) - controlBefore; got != 0 {
		t.Errorf("%d batch-processor goroutines outlived the control provider's Shutdown", got)
	}
}

// batchSpanProcessorGoroutines counts the batch-processor goroutines the otel SDK has running.
// Every trace.WithBatcher starts exactly one and only Shutdown stops it, so the count is how
// the row above sees a provider nobody owns rather than describing one.
//
// It counts the processQueue frame that remains on the stack for the goroutine's lifetime. The
// SDK starts that goroutine through sync.WaitGroup.Go, so its creator frame no longer identifies
// NewBatchSpanProcessor. The startup assertions poll until processQueue is scheduled. If the SDK
// renames it this reads zero for every provider, sei's and the row's control alike — which is why
// the row above pairs it with one, since a zero delta on its own says nothing about whether sei
// stopped leaking.
func batchSpanProcessorGoroutines() int {
	const frame = "go.opentelemetry.io/otel/sdk/trace.(*batchSpanProcessor).processQueue"
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n]), frame)
		}
		buf = make([]byte, 2*len(buf))
	}
}

// waitForBatchSpanProcessorGoroutines polls until the count reaches want, or gives up and
// reports what it saw. A new processor may not have reached processQueue when construction
// returns. Shutdown can return a moment before that frame finishes unwinding. Waiting keeps
// both transitions from making the characterization flaky.
func waitForBatchSpanProcessorGoroutines(want int) int {
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := batchSpanProcessorGoroutines()
		if got == want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(time.Millisecond)
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
