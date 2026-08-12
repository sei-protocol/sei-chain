package experimental

import (
	"fmt"
	"sort"
	"sync"

	"github.com/spf13/cast"
)

// Section is the configuration table experimental keys live under.
//
// A key's own identity omits it. OCCWorkers below is declared as
// "giga.executor.occ_worker_count" and resolves at
// "experimental.giga.executor.occ_worker_count", so the table is a property of where the
// value is written rather than part of what the key is called. That is what lets promotion to
// the stable registry move a declaration without renaming the key.
const Section = "experimental"

// FlatView reads a configuration value by key. It matches servertypes.AppOptions
// structurally, so a *viper.Viper satisfies both and a read site needs no adapter.
type FlatView interface {
	Get(string) any
}

// Declared is the registry's view of a key, independent of its Go type.
//
// The registry holds these rather than typed handles because a fingerprint, a doctor report
// and a promotion check all need to walk every key without knowing any of their types.
type Declared interface {
	// Key is the operator-facing identity, without the section.
	Key() string
	// Path is where the value resolves, Section plus Key.
	Path() string
	// Owner is the team that declared it, so a stale key has someone to ask.
	Owner() string
	// Kind names the declared type for a report.
	Kind() string
	// Check reports whether a raw value converts to the declared type.
	Check(raw any) error
}

// Handle is a declared experimental key, read back typed.
//
// Declaring one is the entire cost of shipping an experimental value. It carries no schema
// entry, needs no migration, and makes no compatibility promise across releases.
type Handle[T any] struct {
	key    string
	def    T
	owner  string
	kind   string
	decode func(any) (T, error)
}

// Get returns the operator's value, or the declared default where the key is absent.
//
// A value that does not convert also yields the default, and that is safe only because
// Check reports it during the boot's validation pass. Get is the read site's call, and a read
// site cannot do anything useful with a conversion error at the point it needs the value.
// Removing that pass would make this a silent substitution, which is the legacy path's
// defect.
func (h *Handle[T]) Get(v FlatView) T {
	raw := v.Get(h.Path())
	if raw == nil {
		return h.def
	}
	out, err := h.decode(raw)
	if err != nil {
		return h.def
	}
	return out
}

// Key returns the operator-facing identity, without the section.
func (h *Handle[T]) Key() string { return h.key }

// Path returns where the value resolves.
func (h *Handle[T]) Path() string { return Section + "." + h.key }

// Owner returns the declaring team.
func (h *Handle[T]) Owner() string { return h.owner }

// Kind returns the declared type's name.
func (h *Handle[T]) Kind() string { return h.kind }

// Check reports whether raw converts to this key's declared type.
func (h *Handle[T]) Check(raw any) error {
	if raw == nil {
		return nil
	}
	if _, err := h.decode(raw); err != nil {
		return fmt.Errorf("%s: %w", h.Path(), err)
	}
	return nil
}

// Default returns the baseline this key carries when nothing writes it.
func (h *Handle[T]) Default() T { return h.def }

// Option configures a declaration.
type Option func(*options)

type options struct{ owner string }

// Owner names the team that declared a key.
//
// It is required in practice rather than in the type, because an unowned experimental key is
// the one nobody can decide to promote or delete. A declaration without it registers and
// reports its owner as unknown, which doctor can then surface.
func Owner(team string) Option {
	return func(o *options) { o.owner = team }
}

var (
	mu       sync.RWMutex
	declared = map[string]Declared{}
)

// Int declares an experimental integer key.
func Int(key string, def int, opts ...Option) *Handle[int] {
	return register(&Handle[int]{key: key, def: def, kind: "int", decode: cast.ToIntE}, opts)
}

// String declares an experimental string key.
func String(key string, def string, opts ...Option) *Handle[string] {
	return register(&Handle[string]{key: key, def: def, kind: "string", decode: cast.ToStringE}, opts)
}

// Bool declares an experimental boolean key.
func Bool(key string, def bool, opts ...Option) *Handle[bool] {
	return register(&Handle[bool]{key: key, def: def, kind: "bool", decode: cast.ToBoolE}, opts)
}

// register records a handle and returns it, so a declaration is one expression at package
// scope. It panics on an empty or duplicate key, because both are programming errors that a
// package's init has no way to report and that would otherwise make one of two declarations
// silently unreachable.
func register[T any](h *Handle[T], opts []Option) *Handle[T] {
	var o options
	for _, apply := range opts {
		apply(&o)
	}
	h.owner = o.owner
	if h.owner == "" {
		h.owner = "unknown"
	}
	if h.key == "" {
		panic("experimental: a key cannot be empty")
	}

	mu.Lock()
	defer mu.Unlock()
	if prior, ok := declared[h.key]; ok {
		panic(fmt.Sprintf("experimental: %q is declared twice, by %s and %s; one of the two reads "+
			"a value the other one owns", h.key, prior.Owner(), h.owner))
	}
	declared[h.key] = h
	return h
}

// Keys returns every declared key, sorted.
//
// Sorted because a caller rendering a report or hashing a set needs a stable order, and Go's
// map iteration is deliberately randomized.
func Keys() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(declared))
	for k := range declared {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Lookup returns the declaration for a key.
func Lookup(key string) (Declared, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := declared[key]
	return d, ok
}
