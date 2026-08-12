package experimental

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// AppOptions reads a configuration value by key.
//
// Declared here rather than imported from sei-cosmos/server/types, because that import creates a
// cycle: it reaches sei-cosmos/server/config, which imports sei-db/config, and sei-db/config has a
// test that imports this package to check its own keys for collisions.
//
// The method set is identical, so a *viper.Viper and any servertypes.AppOptions satisfy both and no
// read site needs an adapter. Two other packages in this tree declare the same one-method interface
// for the same reason, at sei-cosmos/server/types/app.go and sei-db/config/receipt_config.go.
type AppOptions interface {
	Get(string) any
}

// Namespace is the single configuration table experimental keys live under. It is a constant,
// not configurable: the on-disk contract names one table.
const Namespace = "experimental"

// Value is the closed set of types an experimental key may hold.
//
// A type set rather than any, because the set is closed by design: a knob needing a list is a
// knob needing a section.
type Value interface {
	int | bool | string | time.Duration
}

// Decl carries everything a declaration needs.
//
// A struct rather than a name plus functional options, so Owner and Since are visible at every
// call site, which an unpassed option is not. Non-emptiness is not expressible in Go's type
// system, so the declaration check still rejects an empty one.
type Decl[T Value] struct {
	// Name is the key's dotted path without the namespace prefix, mirroring the stable section
	// spelling. It is exactly the path the key occupies after promotion.
	Name string
	// Default is the value every read resolves when the key is absent, unreadable, or fails Check.
	Default T
	// Owner names the team accountable for the key. Required, and checked non-empty.
	Owner string
	// Since names the first binary version that recognized the key, so a report can tell a node
	// operator whether their binary predates it. Required, and checked non-empty.
	Since string
	// Check is an optional value-domain check. A value that fails it is treated exactly as one
	// that fails to parse.
	//
	// It must be a pure function of its argument: no I/O, no blocking, no package-state mutation.
	// Nothing enforces that. It is never called during package initialisation, because a Check
	// that panicked there would take down every command including --help, so the rule that
	// Default must satisfy it is asserted by the declaration check instead.
	Check func(T) error
}

// Key is one declared experimental configuration key.
//
// Self-contained: Get consults the Key and never the registry, so a read takes no lock and does
// not depend on package initialisation order or on which packages the binary linked.
//
// The zero Key is valid and inert. Every method is safe on it, Get returns the zero value of T,
// Name and Path return the empty string, and Reject reports every value usable, since there is
// no declaration to reject against.
type Key[T Value] struct {
	name    string
	def     T
	kind    string
	parse   func(any) (T, error)
	check   func(T) error
	inert   bool
	defect  *DeclarationError
	renders func(T) string
}

// Get resolves the key and cannot fail.
//
// An absent key, a value that does not convert to T, and a value that fails Check all yield the
// declared default. The sweep is what makes the last two visible.
func (k *Key[T]) Get(opts AppOptions) T {
	v, _ := k.GetE(opts)
	return v
}

// GetE resolves the key and reports a rejected operator value, for a feature that wants to
// refuse on its own terms.
//
// Its error is always *ValueError and never reports a declaration defect. A defective
// declaration has no operator value to reject, is reported at boot through Defects, and
// returning it here would make the prescribed `v, err := K.GetE(opts); if err != nil` refuse
// boot fleet-wide because a developer forgot Owner.
//
// The value returned alongside a non-nil error is always the declared default, so a caller that
// ignores the error behaves exactly as Get.
func (k *Key[T]) GetE(opts AppOptions) (T, error) {
	if k == nil || k.parse == nil || k.inert || opts == nil {
		return k.zeroOrDefault(), nil
	}
	raw := opts.Get(k.Path())
	if raw == nil {
		return k.def, nil
	}
	v, err := k.resolve(raw)
	if err != nil {
		return k.def, &ValueError{
			Key:   k.Path(),
			Raw:   raw,
			Want:  k.kind,
			Used:  k.renders(k.def),
			Cause: err,
		}
	}
	return v, nil
}

// zeroOrDefault returns the declared default, or T's zero on a zero Key.
func (k *Key[T]) zeroOrDefault() T {
	var zero T
	if k == nil {
		return zero
	}
	return k.def
}

// resolve converts a raw value, and is the one place Get and Reject agree on what a value means.
// Two conversions would let a value read one way and classify another.
func (k *Key[T]) resolve(raw any) (T, error) {
	var zero T
	v, err := k.parse(raw)
	if err != nil {
		return zero, err
	}
	if k.check != nil {
		if err := k.check(v); err != nil {
			return zero, err
		}
	}
	return v, nil
}

// Reject reports whether a raw configuration value would be used, and why not when it would not.
//
// Two values rather than a bare *ValueError, because a nil *ValueError assigned into an error
// interface is not nil: a caller writing `return v, k.Reject(raw)` would refuse boot on a valid
// value. The boolean removes the trap from the signature.
func (k *Key[T]) Reject(raw any) (*ValueError, bool) {
	if k == nil || k.parse == nil {
		return nil, true
	}
	if _, err := k.resolve(raw); err != nil {
		return &ValueError{
			Key:   k.Path(),
			Raw:   raw,
			Want:  k.kind,
			Used:  k.renders(k.def),
			Cause: err,
		}, false
	}
	return nil, true
}

// RejectDefault reports whether the declared default satisfies the declaration's own Check.
//
// Separate from Reject because Reject applies the cast rules first, and those reject a non-string
// raw for Duration, so Reject on a Duration key's own typed default would reject a perfectly
// valid declaration. This applies Check alone, to the typed default.
func (k *Key[T]) RejectDefault() (*ValueError, bool) {
	if k == nil || k.check == nil {
		return nil, true
	}
	if err := k.check(k.def); err != nil {
		return &ValueError{
			Key:   k.Path(),
			Raw:   k.def,
			Want:  k.kind,
			Used:  k.renders(k.def),
			Cause: err,
		}, false
	}
	return nil, true
}

// Name is the declared name, and the exact path the key occupies after promotion.
func (k *Key[T]) Name() string {
	if k == nil {
		return ""
	}
	return k.name
}

// Path is Namespace plus the name: the dotted path Get reads and the identity in the file.
func (k *Key[T]) Path() string {
	if k == nil || k.name == "" {
		return ""
	}
	return Namespace + "." + k.name
}

// Default is the value every failed or absent read resolves to.
func (k *Key[T]) Default() T { return k.zeroOrDefault() }

// DeclarationError reports a declaration this binary refused to honour.
type DeclarationError struct{ Name, Reason string }

func (e *DeclarationError) Error() string {
	return fmt.Sprintf("experimental declaration %q refused: %s", e.Name, e.Reason)
}

// ValueError reports a configured value that was not used.
type ValueError struct {
	Key   string // the full dotted path
	Raw   any    // the value as the configuration layer supplied it, type preserved
	Want  string // the declared type
	Used  string // the rendered value that resolved instead
	Cause error  // the cast failure or the Decl.Check error
}

func (e *ValueError) Error() string {
	return fmt.Sprintf("%s: %#v (%T) is not a usable %s, using %s: %v",
		e.Key, e.Raw, e.Raw, e.Want, e.Used, e.Cause)
}

func (e *ValueError) Unwrap() error { return e.Cause }

// ---------------------------------------------------------------------------------------
// Constructors. Nothing in a Decl is executed here: every rule about a declaration is
// asserted by the declaration check, because a caller-supplied Check that panicked during
// package initialisation would take down every command including --help.
// ---------------------------------------------------------------------------------------

// Int declares an int-valued experimental key and registers it.
func Int(d Decl[int]) *Key[int] {
	return register(&Key[int]{
		name: d.Name, def: d.Default, kind: "int", check: d.Check,
		parse:   parseInt,
		renders: func(v int) string { return strconv.Itoa(v) },
	}, d.Owner, d.Since)
}

// Bool declares a bool-valued experimental key and registers it.
func Bool(d Decl[bool]) *Key[bool] {
	return register(&Key[bool]{
		name: d.Name, def: d.Default, kind: "bool", check: d.Check,
		parse:   parseBool,
		renders: func(v bool) string { return strconv.FormatBool(v) },
	}, d.Owner, d.Since)
}

// String declares a string-valued experimental key and registers it. An empty configured value
// is honoured as an intentional empty string, unlike the other types.
func String(d Decl[string]) *Key[string] {
	return register(&Key[string]{
		name: d.Name, def: d.Default, kind: "string", check: d.Check,
		parse:   parseString,
		renders: func(v string) string { return strconv.Quote(v) },
	}, d.Owner, d.Since)
}

// Duration declares a duration-valued experimental key and registers it. A configured value must
// be a string carrying a unit: a bare number would otherwise resolve as nanoseconds.
func Duration(d Decl[time.Duration]) *Key[time.Duration] {
	return register(&Key[time.Duration]{
		name: d.Name, def: d.Default, kind: "duration", check: d.Check,
		parse:   parseDuration,
		renders: func(v time.Duration) string { return v.String() },
	}, d.Owner, d.Since)
}

// ---------------------------------------------------------------------------------------
// Cast rules, one per type, enumerated rather than delegated to a permissive caster.
//
// The legacy path's defect is that cast coerces almost anything and discards the error, so a
// blanked number becomes zero and a bool becomes 1. Each rule below refuses a conversion that
// would silently change what an operator meant.
// ---------------------------------------------------------------------------------------

// parseInt accepts an integer, or a decimal string without a leading zero. It refuses a bool, a
// float, and an empty string, each of which cast would coerce.
func parseInt(raw any) (int, error) {
	switch v := raw.(type) {
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case bool:
		return 0, fmt.Errorf("a bool is not an int; cast would read it as 0 or 1")
	case float64:
		return 0, fmt.Errorf("a float is not an int; write it without a decimal point")
	case string:
		if v == "" {
			return 0, fmt.Errorf("empty; cast would read it as 0, which for a limit often means unlimited")
		}
		if len(v) > 1 && (v[0] == '0' || (v[0] == '+' && v[1] == '0')) {
			return 0, fmt.Errorf("%q has a leading zero, which is ambiguous between decimal and octal", v)
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("%q is not a decimal integer", v)
		}
		return n, nil
	}
	return 0, fmt.Errorf("%T is not an int", raw)
}

// parseBool accepts a bool, or a string TOML would produce for one. It refuses a number, which
// cast reads as true for anything non-zero.
func parseBool(raw any) (bool, error) {
	switch v := raw.(type) {
	case bool:
		return v, nil
	case int, int32, int64, float64:
		return false, fmt.Errorf("%v is a number, not a bool; write true or false", v)
	case string:
		if v == "" {
			return false, fmt.Errorf("empty; write true or false")
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			return false, fmt.Errorf("%q is not a bool; write true or false", v)
		}
		return b, nil
	}
	return false, fmt.Errorf("%T is not a bool", raw)
}

// parseString accepts a string only. A number or bool arriving here means the operator wrote an
// unquoted value where a string was declared, which is worth reporting rather than stringifying.
func parseString(raw any) (string, error) {
	if s, ok := raw.(string); ok {
		return s, nil
	}
	return "", fmt.Errorf("%T is not a string; quote the value", raw)
}

// parseDuration accepts a string carrying a unit. It refuses a bare number, which
// time.Duration would read as nanoseconds, so 30 would mean 30ns rather than the 30s an
// operator meant.
func parseDuration(raw any) (time.Duration, error) {
	s, ok := raw.(string)
	if !ok {
		return 0, fmt.Errorf("%T is not a duration; write a quoted string with a unit, like \"30s\"", raw)
	}
	if s == "" {
		return 0, fmt.Errorf("empty; write a quoted string with a unit, like \"30s\"")
	}
	if strings.TrimLeft(s, "+-0123456789") == "" {
		return 0, fmt.Errorf("%q has no unit; a bare number would read as nanoseconds, so write %qs", s, s)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration: %w", s, err)
	}
	return d, nil
}
