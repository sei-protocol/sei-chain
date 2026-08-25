package configmanager

import (
	"reflect"
	"testing"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// TestTheCopyShareNothingWithWhatItCopied is the property the rollback rests on.
//
// The delivery decodes into a copy and publishes by replacing, so a refused value leaves the node's
// configuration untouched. That holds only if the copy shares nothing the decode can write through, and a
// decoder writes a list into the array its target already holds. One shared section, list or map and the
// rehearsal edits the original.
//
// Walked over the whole type rather than the fields anyone thought of, so a reference added to the node's
// configuration fails here rather than quietly sharing.
func TestTheCopyShareNothingWithWhatItCopied(t *testing.T) {
	from := tmcfg.DefaultConfig()
	// Give every list something in it, so a shared backing array is observable.
	from.RPC.CORSAllowedOrigins = []string{"a", "b", "c"}
	from.StateSync.RPCServers = []string{"one:1", "two:2"}
	from.TxIndex.Indexer = []string{"kv"}
	from.Other = map[string]any{"left": "over"}

	out, err := copyNodeConfig(from)
	if err != nil {
		t.Fatalf("copyNodeConfig: %v", err)
	}

	for _, path := range referencePathsIn(reflect.TypeOf(tmcfg.Config{}), "", map[reflect.Type]bool{}) {
		a, okA := fieldByPath(reflect.ValueOf(from).Elem(), path)
		b, okB := fieldByPath(reflect.ValueOf(out).Elem(), path)
		if !okA || !okB {
			continue
		}
		if shares(a, b) {
			t.Errorf("%s is shared between the node's configuration and the copy, so a decode into the "+
				"copy writes through to the node and a refused value cannot be rolled back", path)
		}
	}
}

// TestTheCopyHoldsWhatItCopied is the other half: detaching must not lose a value.
//
// A copy that shares nothing and holds nothing would pass the test above and deliver a configuration of
// zeroes over a running node.
func TestTheCopyHoldsWhatItCopied(t *testing.T) {
	from := tmcfg.DefaultConfig()
	from.RPC.CORSAllowedOrigins = []string{"a", "b", "c"}
	from.Mempool.Size = 4321
	from.Instrumentation.Prometheus = true
	from.Other = map[string]any{"left": "over"}

	out, err := copyNodeConfig(from)
	if err != nil {
		t.Fatalf("copyNodeConfig: %v", err)
	}
	if !reflect.DeepEqual(from, out) {
		t.Error("the copy does not hold what it copied; a delivery would publish a configuration that " +
			"differs from the node's in ways nobody wrote")
	}
}

// fieldByPath walks a dotted field path, following pointers.
func fieldByPath(v reflect.Value, path string) (reflect.Value, bool) {
	for _, name := range splitPath(path) {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		f := v.FieldByName(name)
		if !f.IsValid() {
			return reflect.Value{}, false
		}
		v = f
	}
	return v, true
}

// splitPath breaks a dotted field path into its names.
func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			out = append(out, path[start:i])
			start = i + 1
		}
	}
	return append(out, path[start:])
}

// shares reports whether two values point at the same memory.
func shares(a, b reflect.Value) bool {
	if a.Kind() != b.Kind() {
		return false
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Map:
		return !a.IsNil() && !b.IsNil() && a.Pointer() == b.Pointer()
	case reflect.Slice:
		return a.Len() > 0 && b.Len() > 0 && a.Pointer() == b.Pointer()
	}
	return false
}
