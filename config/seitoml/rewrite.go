package seitoml

import (
	"fmt"
)

// The rewrites a migration is built from.
//
// Each returns something usable as a Migration's Apply, and each shares one rule: a key the operator never
// wrote is left alone rather than treated as an error. A migration runs on files that are already correct,
// and most of them will not have written the setting being changed. Refusing those would refuse to upgrade
// the majority of a fleet in order to transform a minority.
//
// Only two are here, and both exist for a change already known to be coming. There is no SplitKey or
// MergeKeys, because nothing needs one yet and a rewrite with no caller is a rewrite whose semantics were
// decided by guessing.

// RenameKey moves a written value from one key to another.
//
// This is what a key correction needs. Ninety-two operator-facing keys reach their field only through a
// spelling their struct tags do not produce, and the reason those tags have not been corrected is that
// correcting one renames a key operators already have in their files. A rename in the file is what makes
// the correction safe, and this is that rename.
//
// A file writing both spellings is refused rather than resolved. One of the two values is the one the node
// should run and nothing here can tell which, so silently keeping either discards a setting somebody chose.
//
// The comment above the old key does not follow the value. It belongs to a line that no longer exists, and
// carrying it would attach an explanation written about one key to another.
func RenameKey(from, to string) func(*File) error {
	return func(f *File) error {
		value, written, err := f.Get(from)
		if err != nil {
			return fmt.Errorf("read %s: %w", from, err)
		}
		if !written {
			return nil
		}
		_, taken, err := f.Get(to)
		if err != nil {
			return fmt.Errorf("read %s: %w", to, err)
		}
		if taken {
			return fmt.Errorf("this file writes both %s and %s, and the rename can only keep one of them. "+
				"Remove whichever is not the value this node should run, then upgrade again", from, to)
		}
		if err := f.Set(to, value); err != nil {
			return fmt.Errorf("write %s: %w", to, err)
		}
		if _, err := f.Unset(from); err != nil {
			return fmt.Errorf("remove %s: %w", from, err)
		}
		return nil
	}
}

// MapValues rewrites the spellings of a key's value that a release has renamed.
//
// This is what an enumerated setting needs when its members are renamed. The state-commit write mode is the
// case: a file from an older release says "cosmos_only" for the routing a later one calls "memiavl_only",
// and the reader translates it on every start, for good. Translating the file once is what lets the reader
// stop.
//
// A value the replacements do not cover is left exactly as written, whatever it is. It may already be the
// current spelling, which most files will hold, and refusing those would refuse to upgrade a file that is
// right. A value that is neither is one a diagnostic reports and a node refuses, and turning it into a
// failed upgrade instead moves the report somewhere an operator is less able to act on it.
//
// Nothing here validates. A rewrite that decided which values are allowed would state the section's rules
// a second time, and the two statements would drift.
func MapValues(key string, replacements map[string]string) func(*File) error {
	return func(f *File) error {
		if len(replacements) == 0 {
			return fmt.Errorf("the rewrite of %s replaces nothing, so it would be a migration that does "+
				"not migrate", key)
		}
		value, written, err := f.Get(key)
		if err != nil {
			return fmt.Errorf("read %s: %w", key, err)
		}
		if !written {
			return nil
		}
		spelling, isText := value.(string)
		if !isText {
			return nil
		}
		replacement, covered := replacements[spelling]
		if !covered {
			return nil
		}
		if err := f.Set(key, replacement); err != nil {
			return fmt.Errorf("write %s: %w", key, err)
		}
		return nil
	}
}
