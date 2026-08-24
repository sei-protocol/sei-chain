package configmanager

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/go-viper/mapstructure/v2"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// detachSections makes a copy hold what the original holds without sharing anything a decode can write
// through.
//
// A copy of the struct alone shares every section, every list and every map it points at. A decoder writes
// a list into the array its target already holds, so a shared one means the rehearsal edits the original
// and a refused value leaves exactly the half-written configuration the copy exists to prevent.
//
// Walked over the type rather than field by field, so a section or a list added to the node's configuration
// is detached without this changing. A field it cannot detach is an error rather than a silent share, and
// the test beside this holds every reference in the type against that promise.
func detachSections(out, from *tmcfg.Config) error {
	if out == nil || from == nil {
		return fmt.Errorf("no configuration to detach")
	}
	return detachValue(reflect.ValueOf(out).Elem(), "")
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

// describe reads the value the node's configuration currently holds for each key, as text.
//
// Read through the same tags the decode writes through, so a key names the same field in both directions.
// Held as text because what a report needs is whether two values differ and what they are, and comparing
// the shapes a decode produced against the shapes a struct holds would answer a different question.
func describe(cfg *tmcfg.Config, keys []string) map[string]string {
	out := map[string]string{}
	if cfg == nil {
		return out
	}
	var nested map[string]any
	if err := mapstructure.Decode(cfg, &nested); err != nil {
		return out
	}
	flat := map[string]any{}
	flatten("", nested, flat)
	for _, key := range keys {
		if v, ok := flat[key]; ok {
			out[key] = fmt.Sprint(v)
		}
	}
	return out
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
		out[path] = value
	}
}

// referencePathsIn returns every path in a type that a copy has to detach, for the test that holds
// detachSections to the type it copies.
func referencePathsIn(t reflect.Type, path string, seen map[reflect.Type]bool) []string {
	if seen[t] {
		return nil
	}
	seen[t] = true
	defer delete(seen, t)

	var out []string
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		if path != "" {
			out = append(out, path)
		}
		if t.Kind() != reflect.Interface {
			out = append(out, referencePathsIn(t.Elem(), path, seen)...)
		}
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			out = append(out, referencePathsIn(f.Type, join(path, f.Name), seen)...)
		}
	}
	sort.Strings(out)
	return out
}

// samePath reports whether two paths name the same field, ignoring repeats a pointer produces.
func samePath(a, b string) bool { return strings.TrimSuffix(a, ".") == strings.TrimSuffix(b, ".") }
