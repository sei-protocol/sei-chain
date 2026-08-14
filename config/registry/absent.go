package registry

import (
	"fmt"
	"reflect"
	"sort"
)

// zeroWhenAbsent holds the keys whose reader resolves an absent key to the zero value.
var zeroWhenAbsent = map[string]bool{}

// valueWhenAbsent holds the keys whose absent value is neither their baseline nor their zero.
var valueWhenAbsent = map[string]any{}

// DeclareZeroWhenAbsent records that a key resolves to its type's zero when nothing supplies it.
//
// Most readers check that a key was present before assigning, so an absent key keeps the default the
// reader started from. Some assign straight from the lookup, and then an absent key resolves to what the
// cast makes of nothing, which is the zero, and the default beside it is lost.
//
// The difference decides what a migration writes. A file built for a node that has been running has to
// carry the value the node runs, and for these keys that is the zero rather than the default. Writing the
// default instead changes what the node does, which for the state store is a node that stops starting.
//
// Declared by the owning package, beside its registration, because whether a read is checked is a fact
// about that package's reader. A check holds the declaration against the reader, so this cannot drift
// from what the code does.
func DeclareZeroWhenAbsent(section string, keys ...string) {
	mu.Lock()
	defer mu.Unlock()
	if len(keys) == 0 {
		defects = append(defects, Defect{Section: section, Err: fmt.Errorf(
			"declared no keys as resolving to zero when absent; a declaration covering nothing reads as " +
				"though the section had been examined")})
		return
	}
	for _, key := range keys {
		zeroWhenAbsent[key] = true
	}
}

// DeclareValueWhenAbsent records the value a key resolves to when neither rule describes it.
//
// Two rules cover almost every key: a reader that checks its read keeps the default, and one that does not
// resolves to the zero. A key needs this only when its answer is neither, and the case that requires it is
// a section whose baseline varies by node mode while its reader's default does not. The EVM service
// toggles are that: seid init writes them per mode, so the baseline matches a node it provisioned, and a
// node whose file lacks them serves those interfaces whatever kind of node it is.
//
// Held to the same standard as the two rules. A check writes this value and requires the reader's output
// to be unchanged, so a declaration that does not preserve behaviour fails rather than shipping.
func DeclareValueWhenAbsent(section, key string, value any) {
	mu.Lock()
	defer mu.Unlock()
	if value == nil {
		defects = append(defects, Defect{Section: section, Err: fmt.Errorf(
			"declared no value for %q when absent; a migration cannot write nothing", key)})
		return
	}
	valueWhenAbsent[key] = value
}

// ZeroWhenAbsent reports whether a key resolves to its type's zero when nothing supplies it.
func ZeroWhenAbsent(key string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return zeroWhenAbsent[key]
}

// ValueWhenAbsent returns the value a section named for a key neither rule describes.
func ValueWhenAbsent(key string) (any, bool) {
	mu.RLock()
	defer mu.RUnlock()
	value, ok := valueWhenAbsent[key]
	return value, ok
}

// ZeroWhenAbsentKeys returns those keys, sorted.
func ZeroWhenAbsentKeys() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(zeroWhenAbsent))
	for key := range zeroWhenAbsent {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// AbsentValues returns, for every declared key, the value a node resolves when nothing supplies it.
//
// This is what a migration writes for a key the existing configuration does not carry. For most keys it
// is the baseline, because the reader keeps its default when the key is absent. For a key declared
// through DeclareZeroWhenAbsent it is the zero of that key's type, because the reader does not. For the
// few where neither holds, it is whatever DeclareValueWhenAbsent named.
//
// Resolved for one mode, since a baseline may vary by mode. The zero does not.
func AbsentValues(mode Mode) (map[string]any, error) {
	resolved, err := Resolve(mode)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(resolved.Keys))
	mu.RLock()
	declared := make(map[string]any, len(valueWhenAbsent))
	for key, value := range valueWhenAbsent {
		declared[key] = value
	}
	mu.RUnlock()

	for key, resolution := range resolved.Keys {
		if value, named := declared[key]; named {
			out[key] = value
			continue
		}
		if !ZeroWhenAbsent(key) {
			out[key] = resolution.Value
			continue
		}
		if resolution.Value == nil {
			return nil, fmt.Errorf("%s resolves to nothing, so this cannot tell what its zero is", key)
		}
		out[key] = reflect.Zero(reflect.TypeOf(resolution.Value)).Interface()
	}
	return out, nil
}
