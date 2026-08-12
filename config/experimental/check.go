package experimental

import (
	"fmt"
	"sort"
	"strings"
)

// KeySpace enumerates the keys a configuration source actually carries.
//
// This is deliberately a second interface rather than a method on FlatView. Reading a
// declared key needs only Get, and every AppOptions in the tree has that. Finding a key
// nobody declared needs the written set, which Get cannot produce, and only a *viper.Viper
// has it. Keeping them apart means a read site is not forced to accept a type it does not
// need.
type KeySpace interface {
	AllKeys() []string
}

// Finding is one thing the check pass saw.
type Finding struct {
	// Path is the key as it resolves, including the section.
	Path string
	// Unrecognized is true where no declaration matches the key.
	Unrecognized bool
	// Err is a conversion failure on a recognized key, and nil otherwise.
	Err error
	// Owner is the declaring team, empty for an unrecognized key.
	Owner string
}

// String renders a finding for a log line.
func (f Finding) String() string {
	switch {
	case f.Unrecognized:
		return f.Path + ": no binary in this release declares it"
	case f.Err != nil:
		return fmt.Sprintf("%s (owner %s): %v", f.Path, f.Owner, f.Err)
	default:
		return f.Path
	}
}

// Check reports what an operator wrote under [experimental] that this binary cannot use.
//
// Two findings, and the difference between them is the whole trade the framework makes.
//
// An unrecognized key is reported and left alone. It is not an error, because a config
// written for the next release has to stay bootable on this one, and because deleting it
// would lose a value a rollback needs. Callers must not halt on one.
//
// A recognized key whose value does not convert carries an Err. The freedom experimental
// keys get is exemption from versioning ceremony, not from definition, so a declared type is
// still a type. Whether that halts is the caller's decision, and today no caller does.
//
// Check reads only the written set and the registry. It resolves nothing and touches no file,
// so it cannot change what a node boots on.
func Check(keys KeySpace) []Finding {
	prefix := Section + "."

	var out []Finding
	for _, path := range keys.AllKeys() {
		// AllKeys lowercases, and viper resolves case-insensitively, so the comparison has to
		// as well or a key written [Experimental] would read as absent rather than as
		// unrecognized.
		if !strings.HasPrefix(strings.ToLower(path), prefix) {
			continue
		}
		key := path[len(prefix):]

		d, ok := Lookup(key)
		if !ok {
			out = append(out, Finding{Path: path, Unrecognized: true})
			continue
		}
		if err := d.Check(rawOf(keys, path)); err != nil {
			out = append(out, Finding{Path: path, Err: err, Owner: d.Owner()})
		}
	}
	// Sorted so a log line is stable across boots of the same configuration, since AllKeys
	// order follows map iteration.
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// rawOf reads a value where the KeySpace can also be read by key, which every caller's
// concrete type can. A KeySpace that cannot yields nil, and Check then reports only the
// unrecognized keys rather than failing.
func rawOf(keys KeySpace, path string) any {
	if v, ok := keys.(FlatView); ok {
		return v.Get(path)
	}
	return nil
}

// Unrecognized returns just the keys no declaration matches.
func Unrecognized(findings []Finding) []string {
	var out []string
	for _, f := range findings {
		if f.Unrecognized {
			out = append(out, f.Path)
		}
	}
	return out
}

// Invalid returns just the recognized keys whose value does not convert.
func Invalid(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Err != nil {
			out = append(out, f)
		}
	}
	return out
}
