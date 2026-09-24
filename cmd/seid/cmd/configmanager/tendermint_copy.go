package configmanager

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/go-viper/mapstructure/v2"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// detachReferences replaces every reference under cfg with one nothing else holds, so a decode into cfg
// cannot write through to whatever cfg was copied from.
//
// A copy of the struct alone shares every section, every list and every map it points at. A decoder writes
// a list into the array its target already holds, so a shared one means the rehearsal edits the original
// and a refused value leaves exactly the half-written configuration the copy exists to prevent.
//
// Walked over the type rather than field by field, so a section or a list added to the node's configuration
// is detached without this changing. Every exported reference gets one of its own and one that cannot is an
// error; an unexported field keeps what it was copied with, which is safe only because the decoder this
// guards against cannot write to one either. The test beside this walks the same type and holds each
// reference it finds to having been detached.
func detachReferences(cfg *tmcfg.Config) error {
	if cfg == nil {
		return fmt.Errorf("no configuration to detach")
	}
	return detachValue(reflect.ValueOf(cfg).Elem(), "")
}

// detachValue replaces every reference under v with one nothing else holds.
//
// An unexported field is skipped rather than refused. The copy this walks was made by assigning the struct,
// which copies unexported fields by value, and a decoder cannot write to one either.
func detachValue(v reflect.Value, path string) error {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || !v.CanSet() {
			return nil
		}
		fresh := reflect.New(v.Type().Elem())
		fresh.Elem().Set(v.Elem())
		if err := detachValue(fresh.Elem(), path); err != nil {
			return err
		}
		v.Set(fresh)

	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !v.Field(i).CanSet() {
				continue
			}
			if err := detachValue(v.Field(i), join(path, f.Name)); err != nil {
				return err
			}
		}

	case reflect.Slice:
		if v.IsNil() || !v.CanSet() {
			return nil
		}
		fresh := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(fresh, v)
		for i := 0; i < fresh.Len(); i++ {
			if err := detachValue(fresh.Index(i), path); err != nil {
				return err
			}
		}
		v.Set(fresh)

	case reflect.Map:
		if v.IsNil() || !v.CanSet() {
			return nil
		}
		fresh := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, key := range v.MapKeys() {
			elem := reflect.New(v.Type().Elem()).Elem()
			elem.Set(v.MapIndex(key))
			if err := detachValue(elem, path); err != nil {
				return err
			}
			fresh.SetMapIndex(key, elem)
		}
		v.Set(fresh)

	case reflect.Interface:
		if v.IsNil() || !v.CanSet() {
			return nil
		}
		inner := v.Elem()
		fresh := reflect.New(inner.Type()).Elem()
		fresh.Set(inner)
		if err := detachValue(fresh, path); err != nil {
			return err
		}
		v.Set(fresh)

	case reflect.Array:
		// An array holds its elements rather than pointing at them, so the copy already has its own. Each
		// element still needs detaching, because what an element holds can be a reference.
		if !v.CanSet() {
			return nil
		}
		for i := 0; i < v.Len(); i++ {
			if err := detachValue(v.Index(i), path); err != nil {
				return err
			}
		}

	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return fmt.Errorf("%s is a %s, which cannot be copied", path, v.Kind())
	}
	return nil
}

// join builds a field path for a message.
func join(path, field string) string {
	if path == "" {
		return field
	}
	return path + "." + field
}

// describe reads the value the node's configuration currently holds for each key, as text, and names the
// keys it could not read.
//
// Read through the same tags the decode writes through, so a key names the same field in both directions.
// Held as text. A report needs to know whether two values differ, and what they are. Comparing the shapes a
// decode produced against the shapes a struct holds would answer a different question.
//
// The unread keys are returned rather than left out of the answer. A key missing from a map reads as an
// empty value, so a caller comparing two answers finds an unread key equal on both sides and reports that
// it did not move. That is the same statement as a key an operator wrote and got, produced by having read
// nothing.
func describe(cfg *tmcfg.Config, keys []string) (values map[string]string, unread []string, err error) {
	held, unread, err := whatEachKeyHolds(cfg, keys)
	values = make(map[string]string, len(held))
	for key, v := range held {
		values[key] = fmt.Sprint(v)
	}
	return values, unread, err
}

// whatEachKeyHolds reads the value a node's configuration holds for each key, and names the keys that are
// not in it.
//
// The value as the struct holds it. A caller writing one into a file needs the type the key carries, and a
// number rendered as text reaches its setting as a zero.
func whatEachKeyHolds(cfg *tmcfg.Config, keys []string) (values map[string]any, unread []string, err error) {
	values = map[string]any{}
	if cfg == nil {
		return values, keys, fmt.Errorf("no configuration to read")
	}
	var nested map[string]any
	if err := mapstructure.Decode(cfg, &nested); err != nil {
		return values, keys, err
	}
	flat := map[string]any{}
	flatten("", nested, flat)
	for _, key := range keys {
		v, ok := flat[key]
		if !ok {
			unread = append(unread, key)
			continue
		}
		values[key] = v
	}
	sort.Strings(unread)
	return values, unread, nil
}

// flatten turns a nested map into one keyed by dotted path.
func flatten(prefix string, in map[string]any, out map[string]any) {
	for name, value := range in {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if inner, nested := value.(map[string]any); nested {
			flatten(path, inner, out)
			continue
		}
		out[path] = whatAPointerHolds(value)
	}
}

// whatAPointerHolds returns the value behind a pointer, or the value itself.
//
// The decoder flattens a pointer to a struct by following it and leaves a pointer to anything else as a
// pointer. Rendering one of those gives an address. Publishing assigns a fresh pointer for a leaf that is
// not a struct, so the address differs on each side of a delivery. The report would name the key as moved
// on every boot and print two addresses for it.
//
// Nothing reaches this today: every pointer leaf the node's configuration carries is left
// undeclared. The walk around it is driven from the type so that a field added later is covered, and this
// keeps that true for one more shape.
func whatAPointerHolds(value any) any {
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Pointer {
		return value
	}
	if v.IsNil() {
		return nil
	}
	return v.Elem().Interface()
}

// TestReporter is the part of a test's own type this file needs.
//
// Named as an interface rather than taking *testing.T, so this file does not pull the testing package into
// a binary the boot path links. The behaviour is the point: the helper below has to be able to end the
// test, because a caller that could carry on would compare two answers produced by reading nothing.
type TestReporter interface {
	Helper()
	Fatalf(format string, args ...any)
}

// DescribeForTest reads what a node's configuration holds for each key, as text.
//
// Exported for the tests that measure a booted node's configuration, which live beside the boot because
// only a boot produces one.
//
// It fails the test rather than answering partially. Two empty answers compare equal across a hundred keys.
// An answer produced by reading nothing is therefore indistinguishable from a node where nothing moved.
func DescribeForTest(t TestReporter, cfg *tmcfg.Config, keys []string) map[string]string {
	t.Helper()
	values, unread, err := describe(cfg, keys)
	if err != nil {
		t.Fatalf("reading %d keys off the node's configuration: %v", len(keys), err)
	}
	if len(unread) > 0 {
		t.Fatalf("%d of %d keys are not present in the node's configuration, so a comparison over them "+
			"would find every one unchanged: %v", len(unread), len(keys), unread)
	}
	return values
}

// publishNodeConfig makes a node's configuration hold what the candidate holds, without replacing it.
//
// Field by field rather than by assigning the whole struct. The configuration is one struct behind one
// pointer, and every section under it is a pointer of its own that components take and keep: constructors
// throughout the node ask for a section rather than for the configuration holding it. Assigning the whole
// struct swaps every one of those pointers for a fresh one, so anything already holding a section goes on
// reading the values that section had before the delivery, and nothing says so.
//
// Assigning through each pointer instead leaves every pointer identity as it was, so a component reads the
// delivered values whether it took its pointer before this ran or after. That removes the ordering the
// delivery would otherwise depend on, which is an ordering nothing states and no test holds.
func publishNodeConfig(target, candidate *tmcfg.Config) error {
	if target == nil || candidate == nil {
		return fmt.Errorf("no configuration to publish into")
	}
	return publishValue(reflect.ValueOf(target).Elem(), reflect.ValueOf(candidate).Elem(), "")
}

// publishValue assigns candidate into target, following a pointer rather than replacing it.
//
// A pointer to a struct is followed and assigned through, which is what keeps the identity whatever holds it
// depends on. Everything else is assigned, and that is what carries the values. A pointer the target does
// not have yet is assigned rather than followed, because there is nothing to assign through.
//
// An unexported field is skipped, for the reason the detach skips one: the candidate was made by assigning
// the struct, so it already holds the same value, and a decoder cannot write to one either.
func publishValue(target, candidate reflect.Value, path string) error {
	if target.Kind() != reflect.Struct {
		target.Set(candidate)
		return nil
	}
	for i := 0; i < target.NumField(); i++ {
		f := target.Type().Field(i)
		tf, cf := target.Field(i), candidate.Field(i)
		if !tf.CanSet() {
			continue
		}
		at := join(path, f.Name)
		followable := f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct
		if followable && !tf.IsNil() && !cf.IsNil() {
			if err := publishValue(tf.Elem(), cf.Elem(), at); err != nil {
				return err
			}
			continue
		}
		if f.Type.Kind() == reflect.Struct {
			if err := publishValue(tf, cf, at); err != nil {
				return err
			}
			continue
		}
		tf.Set(cf)
	}
	return nil
}
