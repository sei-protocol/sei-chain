package registry_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
)

// The tests here hold this package against config/seitoml. Two packages decide what a configuration key
// may be: this one decides what a section may declare, and seitoml decides what the file every source
// reads may carry. They reach that decision from separate rules with separate reasons, so agreement is
// checked rather than shared. A key this package declares and that file cannot hold is a key an operator
// can neither read nor write, and the failure lands when something tries to write it on a node rather
// than when the section is added.
//
// The import runs one way and only in a test, so no production edge is created. seitoml is a file format
// and knows nothing about a declared key; teaching it the tree's configuration types is the dependency
// this split exists to avoid.

// everyShape declares one field per Go type the node's own configuration structs use.
type everyShape struct {
	Text       string        `mapstructure:"text"`
	Flag       bool          `mapstructure:"flag"`
	Count      int           `mapstructure:"count"`
	Wide       int64         `mapstructure:"wide"`
	Narrow     int32         `mapstructure:"narrow"`
	Unsigned   uint          `mapstructure:"unsigned"`
	Unsigned32 uint32        `mapstructure:"unsigned32"`
	Unsigned64 uint64        `mapstructure:"unsigned64"`
	Ratio      float64       `mapstructure:"ratio"`
	Wait       time.Duration `mapstructure:"wait"`
	Names      []string      `mapstructure:"names"`
	Pairs      [][]string    `mapstructure:"pairs"`
	Named      namedText     `mapstructure:"named"`
}

type namedText string

func everyShapeDefaults(registry.Mode) any {
	return &everyShape{
		Text: "tcp://0.0.0.0:1", Flag: true, Count: 4, Wide: 1 << 40, Narrow: 7,
		Unsigned: 8, Unsigned32: 9, Unsigned64: 1 << 40, Ratio: 1.5,
		Wait: 90 * time.Second, Names: []string{"a", "b"}, Pairs: [][]string{{"a", "b"}},
		Named: "memiavl_only",
	}
}

// TestEveryDeclaredKeyCanBeWrittenToTheFile is the property the two rules exist to satisfy.
//
// It is one-directional on purpose. seitoml being stricter than this package is the defect; stating it as
// "the two rules match" would invite closing a failure by loosening the file, which is the wrong end.
func TestEveryDeclaredKeyCanBeWrittenToTheFile(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &everyShape{}, everyShapeDefaults)
	for _, d := range registry.Defects() {
		t.Fatalf("registering every declared shape produced a defect: %v", d.Err)
	}

	resolved, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
	if err != nil {
		t.Fatalf("resolving every declared shape: %v", err)
	}

	f, err := seitoml.New("validator")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, key := range registry.Keys() {
		value := resolved.Values[key]
		if err := f.Set(key, value); err != nil {
			t.Errorf("%s resolves to a %T and the file refuses it: %v\nA declared key the file cannot "+
				"carry is one an operator can neither read nor write, and nothing says so until "+
				"something tries", key, value, err)
		}
	}
}

// TestAValueFromTheFileHasTheTypeTheDefaultHas closes the loop the other way.
//
// A caller reads a value without knowing which source supplied it. If the two disagree on type, code that
// asserts one works on a node running defaults and fails on a node whose operator wrote that key.
func TestAValueFromTheFileHasTheTypeTheDefaultHas(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &everyShape{}, everyShapeDefaults)
	for _, d := range registry.Defects() {
		t.Fatalf("registering every declared shape produced a defect: %v", d.Err)
	}

	fromDefaults, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	// Round-tripped through the file rather than hand-written, so the types are the ones a reader
	// actually produces rather than the ones this test believes it produces.
	f, err := seitoml.New("validator")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for key, value := range fromDefaults.Values {
		if err := f.Set(key, value); err != nil {
			t.Fatalf("writing %s: %v", key, err)
		}
	}
	raw, err := f.Bytes()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	reread, err := seitoml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("reading back what was written: %v", err)
	}
	written, err := reread.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}

	fromFile, err := registry.Resolve(registry.ModeValidator, registry.Sources{File: written})
	if err != nil {
		t.Fatalf("resolving over the file: %v", err)
	}

	for _, key := range registry.Keys() {
		d, o := fromDefaults.Values[key], fromFile.Values[key]
		if reflect.TypeOf(d) != reflect.TypeOf(o) {
			t.Errorf("%s is %T from this node's defaults and %T from its sei.toml. A caller cannot "+
				"assert either type, and whichever it asserts fails on one of the two", key, d, o)
		}
		if !reflect.DeepEqual(d, o) {
			t.Errorf("%s is %#v from the defaults and %#v after a round trip through the file, so the "+
				"same setting reads as two values", key, d, o)
		}
	}
}
