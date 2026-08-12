package experimental

import (
	"sort"
	"strings"
)

// Finding is one thing the check pass saw.
type Finding struct {
	// Path is the key as it resolves, including the section.
	Path string
	// Unrecognized is true where no declaration matches the key.
	Unrecognized bool
	// Err is a conversion failure on a declared key, and nil otherwise.
	Err error
	// Owner is the declaring team, empty for an unrecognized key.
	Owner string
	// Kind is the declared type's name, empty for an unrecognized key.
	Kind string
}

// String renders a finding for a caller that has nowhere structured to put it.
//
// It says what this binary does not declare rather than what no binary declares. The check
// pass reads one in-process registry and has no knowledge of any other binary or of a release,
// and an operator told their key is dead everywhere deletes it, which is the loss this
// framework exists to prevent.
func (f Finding) String() string {
	switch {
	case f.Unrecognized:
		return f.Path + ": this binary does not declare it"
	case f.Err != nil:
		return f.Path + " (owner " + f.Owner + "): " + f.Err.Error()
	default:
		return f.Path
	}
}

// Check reports what this binary cannot use from a configuration source.
//
// It only reads. It writes nothing and touches no file, so it cannot change what a node boots
// on, and it never halts.
func Check(src Source) []Finding {
	findings := append(checkDeclaredKeys(src), findUndeclaredKeys(src)...)
	// Sorted so one configuration produces one order across boots, since a source enumerates
	// in map order.
	sort.Slice(findings, func(i, j int) bool { return findings[i].Path < findings[j].Path })
	return findings
}

// checkDeclaredKeys reports declared keys whose value does not convert.
//
// It walks the registry rather than the source, and that direction is the whole point. A value
// can reach Handle.Get through a channel the source does not enumerate, an environment variable
// being the one that matters in practice, so a pass driven by what was written cannot see it.
// Walking what this binary declares and asking the source for each one covers every channel the
// read path can use.
func checkDeclaredKeys(src Source) []Finding {
	var out []Finding
	for _, key := range Keys() {
		d, ok := Lookup(key)
		if !ok {
			// Only reachable if a declaration were removed between the two calls, which nothing
			// does; registrations land during package init.
			continue
		}
		if err := d.Check(src.Get(d.Path())); err != nil {
			out = append(out, Finding{Path: d.Path(), Err: err, Owner: d.Owner(), Kind: d.Kind()})
		}
	}
	return out
}

// findUndeclaredKeys reports keys written under the section that no declaration matches.
//
// This half has to walk the source, because a key nobody declared is by definition absent from
// the registry. It therefore sees only what the source enumerates, which is what an operator
// wrote in a file.
func findUndeclaredKeys(src Source) []Finding {
	prefix := Section + "."

	var out []Finding
	for _, path := range src.AllKeys() {
		// A source enumerates in lower case and resolves case-insensitively, so both the test
		// and the key derived from it are taken on the lowered path. Deriving from the original
		// would produce a key Lookup cannot match.
		lowered := strings.ToLower(path)
		if lowered == Section {
			// The section written as a scalar rather than a table. Every declared key under it is
			// then shadowed and resolves to its default, which is worth a word.
			out = append(out, Finding{Path: path, Unrecognized: true})
			continue
		}
		if !strings.HasPrefix(lowered, prefix) {
			continue
		}
		if _, ok := Lookup(lowered[len(prefix):]); !ok {
			out = append(out, Finding{Path: lowered, Unrecognized: true})
		}
	}
	return out
}

// Undeclared returns the keys no declaration matches.
func Undeclared(findings []Finding) []string {
	var out []string
	for _, f := range findings {
		if f.Unrecognized {
			out = append(out, f.Path)
		}
	}
	return out
}

// Invalid returns the declared keys whose value does not convert.
func Invalid(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Err != nil {
			out = append(out, f)
		}
	}
	return out
}
