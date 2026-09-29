package configmanager

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/go-viper/mapstructure/v2"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// detachReferences replaces every exported pointer, slice, map and interface under cfg with a copy, so a
// decode into cfg cannot write through to what it was copied from. A channel or func is an error.
// Unexported fields stay shared; the decoder cannot write them.
func detachReferences(cfg *tmcfg.Config) error {
	if cfg == nil {
		return fmt.Errorf("no configuration to detach")
	}
	return detachValue(reflect.ValueOf(cfg).Elem(), "")
}

// detachValue replaces every settable reference under v with a copy.
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
		// Elements are already copies, but may hold references.
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

// describe renders, through mapstructure tags, the value cfg holds for each key as text, and returns the
// keys it could not find so a caller does not compare them as equal.
func describe(cfg *tmcfg.Config, keys []string) (values map[string]string, unread []string, err error) {
	values = map[string]string{}
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
		values[key] = fmt.Sprint(v)
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

// whatAPointerHolds returns the value behind a pointer, or the value itself, so a pointer leaf renders as
// its value rather than an address.
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

// TestReporter is the subset of testing.TB DescribeForTest needs, so this file does not import testing.
type TestReporter interface {
	Helper()
	Fatalf(format string, args ...any)
}

// DescribeForTest is describe for tests, failing the test if any key cannot be read.
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

// publishNodeConfig copies candidate into target through each section pointer rather than replacing the
// pointers, so a component already holding a section sees the delivered values.
func publishNodeConfig(target, candidate *tmcfg.Config) error {
	if target == nil || candidate == nil {
		return fmt.Errorf("no configuration to publish into")
	}
	return publishValue(reflect.ValueOf(target).Elem(), reflect.ValueOf(candidate).Elem(), "")
}

// publishValue assigns candidate into target field by field, assigning through non-nil struct pointers
// on both sides and skipping unexported fields.
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
