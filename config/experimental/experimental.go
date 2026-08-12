package experimental

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cast"
)

// Section is the configuration table experimental keys live under.
const Section = "experimental"

// ErrEmptyValue is returned for an empty value on a key whose type is not a string.
//
// cast reads "" as the zero value with no error, so without this a blanked number or boolean
// would pin the key to zero instead of its declared default. sei-config rejects the same shape
// one layer down, at io.go's rejectEmptyScalarStringHook, for the same reason: blanking a limit
// has to be an error rather than a silent zero.
var ErrEmptyValue = errors.New("empty value")

// FlatView reads a configuration value by key.
//
// It matches servertypes.AppOptions structurally, so a *viper.Viper satisfies both and a read
// site needs no adapter.
type FlatView interface {
	Get(string) any
}

// Source is a configuration source the check pass reads.
//
// The check pass needs both methods: one to find keys nobody declared, one to read the values
// of keys somebody did. Handle.Get takes the narrower FlatView, because a read site does not
// enumerate.
type Source interface {
	AllKeys() []string
	Get(string) any
}

// Declared is the registry's view of a key, independent of its Go type.
type Declared interface {
	// Key is the operator-facing identity, without the section.
	Key() string
	// Path is where the value resolves, Section plus Key.
	Path() string
	// Owner is the team that declared it.
	Owner() string
	// Kind names the declared type.
	Kind() string
	// Check reports whether a raw value converts to the declared type.
	Check(raw any) error
}

// Handle is a declared experimental key, read back typed.
type Handle[T any] struct {
	key    string
	def    T
	owner  string
	kind   string
	decode func(any) (T, error)
}

// Get returns the operator's value, or the declared default where the key is absent or its
// value does not convert.
func (h *Handle[T]) Get(v FlatView) T {
	out, err := h.resolve(v.Get(h.Path()))
	if err != nil {
		return h.def
	}
	return out
}

// resolve converts a raw value, and is the one place Get and Check agree on what a value means.
// Two callers with two conversions would let a value read one way and validate another.
func (h *Handle[T]) resolve(raw any) (T, error) {
	var zero T
	if raw == nil {
		return zero, errors.New("absent")
	}
	if s, ok := raw.(string); ok && s == "" && h.kind != "string" {
		return zero, ErrEmptyValue
	}
	return h.decode(raw)
}

// Key returns the operator-facing identity, without the section.
func (h *Handle[T]) Key() string { return h.key }

// Path returns where the value resolves.
func (h *Handle[T]) Path() string { return Section + "." + h.key }

// Owner returns the declaring team.
func (h *Handle[T]) Owner() string { return h.owner }

// Kind returns the declared type's name.
func (h *Handle[T]) Kind() string { return h.kind }

// Default returns the baseline this key carries when nothing writes it.
func (h *Handle[T]) Default() T { return h.def }

// Check reports whether raw converts to this key's declared type. An absent value is not an
// error, since an unwritten key resolves to its default.
func (h *Handle[T]) Check(raw any) error {
	if raw == nil {
		return nil
	}
	if _, err := h.resolve(raw); err != nil {
		return fmt.Errorf("%s: %w", h.Path(), err)
	}
	return nil
}

// Option configures a declaration.
type Option func(*options)

type options struct{ owner string }

// WithOwner names the team that declared a key.
func WithOwner(team string) Option {
	return func(o *options) { o.owner = team }
}

var (
	mu       sync.RWMutex
	registry = map[string]Declared{}
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
// scope.
//
// Every guard on a key's shape lives here, because this is the one function every declaration
// passes through. A guard in Check instead would be a convention the next entry point could
// forget. It panics rather than returning an error because a package's init has no way to
// report one, and a bad declaration must not resolve to something plausible.
func register[T any](h *Handle[T], opts []Option) *Handle[T] {
	var o options
	for _, apply := range opts {
		apply(&o)
	}
	h.owner = o.owner
	if h.owner == "" {
		h.owner = "unknown"
	}

	requireWellFormedKey(h.key)

	mu.Lock()
	defer mu.Unlock()
	if prior, ok := registry[h.key]; ok {
		panic(fmt.Sprintf("experimental: %q is declared twice, by %s and %s; one of the two reads "+
			"a value the other one owns", h.key, prior.Owner(), h.owner))
	}
	registry[h.key] = h
	return h
}

// requireWellFormedKey panics on a key that cannot round-trip through a configuration source.
//
// The lower-case rule is not a style preference. Configuration sources resolve
// case-insensitively and enumerate in lower case, so a mixed-case declaration registers under
// one spelling and is enumerated under another: an operator who wrote it would be told no
// binary declares it, and its value would be discarded with nothing reported.
func requireWellFormedKey(key string) {
	if key == "" {
		panic("experimental: a key cannot be empty")
	}
	if lower := strings.ToLower(key); key != lower {
		panic(fmt.Sprintf("experimental: key %q must be lower case, as %q; a configuration source "+
			"enumerates in lower case, so a mixed-case key reports to an operator as undeclared "+
			"while its value is discarded", key, lower))
	}
}

// Keys returns every declared key, sorted so a report or a hash over the set is stable.
func Keys() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Lookup returns the declaration for a key.
func Lookup(key string) (Declared, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := registry[key]
	return d, ok
}
