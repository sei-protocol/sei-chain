package configcli

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/config/experimental"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
)

// Change is what set or unset did to one key.
type Change struct {
	// Key is the dotted key.
	Key string
	// From is the value the file held, if it held one.
	From any
	// Had reports whether the file already wrote this key.
	Had bool
	// To is the value now written. Absent for unset.
	To any
	// Removed reports whether the key was taken out of the file.
	Removed bool
}

// Set writes one key's value, taking the value as an operator typed it.
//
// The value arrives as a string from a command line and is converted to the type the key's section
// declares, so a count lands in the file as a number rather than as a quoted string. Without that
// the file would parse, the value would look right to a reader, and the node would reject it at the
// next boot.
//
// A key no section declares is refused. Writing one would put a value in the file that doctor then
// refuses, so the tool would produce a file it will not accept.
func Set(path, key, raw string) (Change, error) {
	key = strings.ToLower(key)
	if err := writableKey(key); err != nil {
		return Change{}, err
	}
	types, err := declaredTypes()
	if err != nil {
		return Change{}, err
	}
	want, declared := types[key]
	if !declared {
		return Change{}, undeclaredKeyError(key, types)
	}
	value, err := convert(raw, want)
	if err != nil {
		return Change{}, fmt.Errorf("%s expects %s: %w", key, want, err)
	}

	file, err := seitoml.Load(path)
	if err != nil {
		return Change{}, err
	}
	before, had, err := file.Get(key)
	if err != nil {
		return Change{}, err
	}
	if err := file.Set(key, value); err != nil {
		return Change{}, err
	}
	if err := file.Save(path); err != nil {
		return Change{}, err
	}
	return Change{Key: key, From: before, Had: had, To: value}, nil
}

// Unset removes one key so it resolves to the running binary's baseline again.
//
// Removing rather than writing the baseline value, because the two look identical in the file and
// mean opposite things: a written value survives a release that changes the baseline, and falling
// back is the point of unset.
func Unset(path, key string) (Change, error) {
	key = strings.ToLower(key)
	if err := writableKey(key); err != nil {
		return Change{}, err
	}
	file, err := seitoml.Load(path)
	if err != nil {
		return Change{}, err
	}
	before, had, err := file.Get(key)
	if err != nil {
		return Change{}, err
	}
	removed, err := file.Unset(key)
	if err != nil {
		return Change{}, err
	}
	if removed {
		if err := file.Save(path); err != nil {
			return Change{}, err
		}
	}
	return Change{Key: key, From: before, Had: had, Removed: removed}, nil
}

// writableKey refuses the keys these verbs must not touch.
//
// The experimental table is written by an operator and never by seid. A section the binary created
// would read as a value someone chose, and this is the same reason a freshly generated home does not
// carry the table either. The schema version is refused because it records what the file is rather
// than configuring anything, and setting it by hand would make the file claim a shape its contents
// do not have.
func writableKey(key string) error {
	if key == seitoml.VersionKey {
		return fmt.Errorf("%s records which schema the file was written against, so it is not a "+
			"setting. Use upgrade to move the file forward", key)
	}
	if key == experimental.Namespace || strings.HasPrefix(key, experimental.Namespace+".") {
		return fmt.Errorf("%s is in the %s table, which is written by hand and never by seid. Edit "+
			"sei.toml directly to set it. A table this command created would be indistinguishable "+
			"from one an operator wrote", key, experimental.Namespace)
	}
	return nil
}

// undeclaredKeyError names the closest declared keys, so a typo is correctable from the message.
func undeclaredKeyError(key string, types map[string]reflect.Type) error {
	declared := make([]string, 0, len(types))
	for k := range types {
		declared = append(declared, k)
	}
	sort.Strings(declared)

	if near := nearest(key, declared); len(near) > 0 {
		return fmt.Errorf("no section declares %s. Closest: %s", key, strings.Join(near, ", "))
	}
	return fmt.Errorf("no section declares %s, and this binary declares %d keys", key, len(declared))
}

// nearest returns up to three declared keys sharing the most leading characters with key.
func nearest(key string, declared []string) []string {
	type scored struct {
		key   string
		score int
	}
	var best []scored
	for _, d := range declared {
		if n := commonPrefix(key, d); n >= 3 {
			best = append(best, scored{d, n})
		}
	}
	sort.SliceStable(best, func(i, j int) bool { return best[i].score > best[j].score })
	out := make([]string, 0, 3)
	for _, b := range best {
		if len(out) == 3 {
			break
		}
		out = append(out, b.key)
	}
	return out
}

// commonPrefix is how many leading characters two keys share.
func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// declaredTypes returns the Go type each declared key holds.
//
// Read off a resolved baseline, because a baseline value comes from the section's own struct field
// and therefore carries that field's type. Which mode produced it does not matter: a mode changes
// what a key's value is, never what type it is, and a test holds that across every mode.
func declaredTypes() (map[string]reflect.Type, error) {
	modes := registry.Modes()
	if len(modes) == 0 {
		return nil, fmt.Errorf("the registry declares no node modes")
	}
	resolved, err := registry.Resolve(modes[0])
	if err != nil {
		return nil, err
	}
	if len(resolved.Keys) == 0 {
		return nil, fmt.Errorf("no section has registered a key, so no key can be set")
	}
	out := make(map[string]reflect.Type, len(resolved.Keys))
	for key, res := range resolved.Keys {
		out[key] = reflect.TypeOf(res.Value)
	}
	return out, nil
}

// convert reads an operator's text as the type a key declares.
//
// Enumerated per type rather than delegated to a permissive caster, so text that is not the
// declared type is refused here instead of being coerced into a value nobody asked for. The legacy
// path reads a blank as zero and a bare number as nanoseconds, and this is where that stops.
func convert(raw string, want reflect.Type) (any, error) {
	if want == nil {
		return nil, fmt.Errorf("the key has no declared type")
	}
	if want == reflect.TypeOf(time.Duration(0)) {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("%q is not a duration; write a unit, as in 30s or 5m", raw)
		}
		return d, nil
	}
	switch want.Kind() {
	case reflect.Bool:
		// Only the two words TOML spells a bool with. ParseBool would also take 1, 0, t and F, and
		// reading a number as a bool is one of the coercions this whole path exists to stop.
		switch raw {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not true or false", raw)
	case reflect.String:
		return raw, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a whole number", raw)
		}
		return n, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a whole number that is zero or above", raw)
		}
		return n, nil
	case reflect.Float32, reflect.Float64:
		x, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", raw)
		}
		return x, nil
	case reflect.Slice:
		if want.Elem().Kind() != reflect.String {
			return nil, fmt.Errorf("a list of %s cannot be written from the command line", want.Elem())
		}
		return splitList(raw), nil
	default:
		return nil, fmt.Errorf("a %s cannot be written from the command line; edit sei.toml directly",
			want)
	}
}

// splitList reads a comma-separated list, dropping surrounding spaces.
func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}
