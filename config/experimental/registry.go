package experimental

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Checker is the non-generic view of a declared key, so the sweep and the declaration check can
// be handed the declared set as data instead of reaching into package state.
//
// *Key[T] implements it. Every Key method has a pointer receiver, so the value type does not.
type Checker interface {
	// Path is the full dotted path, including the namespace.
	Path() string
	// Reject reports whether raw would be used, resolving it exactly as Get would.
	Reject(raw any) (*ValueError, bool)
	// RejectDefault reports whether the declared default satisfies the declaration's own Check.
	RejectDefault() (*ValueError, bool)
}

// Declaration is one registry row as recorded in the golden. Default is rendered so the golden
// reads as text and cannot drift on formatting.
type Declaration struct{ Name, Type, Default, Owner, Since string }

// Tombstone records a key's afterlife once it is no longer declared, so a stale entry in a node
// operator's file reads as a migration prompt rather than as a typo.
type Tombstone struct {
	Name, Type, Owner, Since string
	RetiredIn                string // the binary version that stopped declaring it
	PromotedTo               string // the first-class path, empty when merely removed
}

var (
	mu           sync.RWMutex
	declarations []Declaration
	checkers     []Checker
	tombstones   []Tombstone
	defects      []*DeclarationError
)

// register records a key and returns it, so a declaration is one expression at package scope.
//
// It runs no caller code and never panics. A package-level panic in a library every feature
// imports is a boot failure for every invocation including seid --help, which converts a
// compile-time-fixable error into a fleet-wide incident. Every rule about a declaration is
// asserted by the declaration check instead, where a panicking Check is a test failure with a
// stack trace and a hanging one is a test timeout.
//
// A declaration whose name or metadata is refused yields an inert Key plus a DeclarationError, so
// a defect that reaches a binary is a no-op with a boot report rather than a live wrong value.
func register[T Value](k *Key[T], owner, since string) *Key[T] {
	if d := refuse(k.name, owner, since); d != nil {
		k.inert, k.defect = true, d

		mu.Lock()
		defer mu.Unlock()
		defects = append(defects, d)
		return k
	}

	mu.Lock()
	defer mu.Unlock()
	if prior := indexOf(k.name); prior >= 0 {
		d := &DeclarationError{Name: k.name, Reason: fmt.Sprintf(
			"declared twice, second by %s; one of the two reads a value the other owns", owner)}
		k.inert, k.defect = true, d
		defects = append(defects, d)
		return k
	}
	declarations = append(declarations, Declaration{
		Name: k.name, Type: k.kind, Default: k.renders(k.def), Owner: owner, Since: since,
	})
	checkers = append(checkers, k)
	return k
}

// indexOf returns the position of a declared name, or -1. Caller holds the lock.
func indexOf(name string) int {
	for i, d := range declarations {
		if d.Name == name {
			return i
		}
	}
	return -1
}

// refuse reports why a declaration cannot be honoured, or nil.
//
// Every rule here is a property of the name or the metadata, checkable without running caller
// code. Whether the declared default satisfies its own Check is deliberately not here: that
// needs the Check, and running it at registration is what this package refuses to do.
func refuse(name, owner, since string) *DeclarationError {
	bad := func(format string, args ...any) *DeclarationError {
		return &DeclarationError{Name: name, Reason: fmt.Sprintf(format, args...)}
	}
	switch {
	case owner == "":
		return bad("no Owner; a key nobody owns is one nobody can decide to promote or delete")
	case since == "":
		return bad("no Since; without it a report cannot tell an operator whether their binary predates the key")
	case name == "":
		return bad("no Name")
	}
	if name != strings.ToLower(name) {
		return bad("%q is not lower case; a configuration source enumerates lower-cased, so this key "+
			"would be reported undeclared forever while Get happened to work through case folding", name)
	}

	segs := strings.Split(name, ".")
	if len(segs) < 2 {
		return bad("%q has one segment; a name needs at least two so it is a section path after "+
			"promotion, when the namespace prefix drops", name)
	}
	if len(segs) > MaxKeySegments {
		return bad("%q has %d segments, more than the %d the sweep will resolve, so the key would be "+
			"skipped forever while Get worked", name, len(segs), MaxKeySegments)
	}
	for _, s := range segs {
		if s == "" {
			return bad("%q has an empty segment", name)
		}
	}
	if segs[0] == Namespace {
		return bad("%q starts with %q; the namespace is a prefix this package adds, and repeating it "+
			"would survive promotion", name, Namespace)
	}
	if len(Namespace)+1+len(name) > MaxKeyBytes {
		return bad("%q is longer than the %d bytes the sweep will resolve", name, MaxKeyBytes)
	}
	return nil
}

// Declarations returns every key declared in this binary, sorted by name.
func Declarations() []Declaration {
	mu.RLock()
	defer mu.RUnlock()
	out := append([]Declaration(nil), declarations...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Checkers returns the non-generic view of every declared key, sorted by path.
func Checkers() []Checker {
	mu.RLock()
	defer mu.RUnlock()
	out := append([]Checker(nil), checkers...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path() < out[j].Path() })
	return out
}

// Retired registers a tombstone. Tombstones appear in the same golden as declarations.
func Retired(t Tombstone) {
	mu.Lock()
	defer mu.Unlock()
	tombstones = append(tombstones, t)
}

// Tombstones returns every registered tombstone, sorted by name.
func Tombstones() []Tombstone {
	mu.RLock()
	defer mu.RUnlock()
	out := append([]Tombstone(nil), tombstones...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Defects returns every refused declaration, sorted by name.
func Defects() []*DeclarationError {
	mu.RLock()
	defer mu.RUnlock()
	out := append([]*DeclarationError(nil), defects...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Reset clears the registry. For tests only, so one test's declarations cannot leak into
// another's golden or sweep.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	declarations, checkers, tombstones, defects = nil, nil, nil, nil
}
