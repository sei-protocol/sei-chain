package seitoml_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/seitoml"
)

// TestAListSurvivesBeingReadAndWrittenAgain closes the asymmetry between the reader and the writer.
//
// Reading an array back produces a list of untyped values, and the writer used to render only a list of
// strings. So anything that read a list-valued key and wrote it again failed on a value this package had
// just handed it, and a migration renaming such a key is exactly that.
func TestAListSurvivesBeingReadAndWrittenAgain(t *testing.T) {
	f, err := seitoml.New("full", "seid test")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("index-events", []string{"tx.height", "message.action"}); err != nil {
		t.Fatalf("write the list: %v", err)
	}

	value, present, err := f.Get("index-events")
	if err != nil || !present {
		t.Fatalf("read the list back: present=%v err=%v", present, err)
	}
	if err := f.Set("renamed-events", value); err != nil {
		t.Fatalf("write what the read returned: %v.\n\nA value this package produced has to be one it "+
			"accepts, or nothing can read a setting and write it somewhere else", err)
	}

	got, present, err := f.Get("renamed-events")
	if err != nil || !present {
		t.Fatalf("read the written list: present=%v err=%v", present, err)
	}
	if !reflect.DeepEqual(got, value) {
		t.Errorf("the list read back as %#v, want %#v", got, value)
	}
}

// TestAListThisCannotRenderStillFails keeps the case above from widening what the writer accepts.
func TestAListThisCannotRenderStillFails(t *testing.T) {
	f, err := seitoml.New("full", "seid test")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("probe", []any{"fine", struct{ Nope int }{}}); err == nil {
		t.Error("a list holding a value the writer cannot render was accepted. The writer must not take " +
			"more than the reader can produce, or an unrenderable value becomes a plausible-looking line")
	}
}

// TestAListOfPairsCanBeWrittenAndReadBack is the shape a metric label set has.
//
// Written as untyped rows, which is both what a file reads back as and the only shape the metric settings
// reader accepts. Its own struct declares a list of string pairs and the reader refuses that, so nothing
// here needs to render it.
func TestAListOfPairsCanBeWrittenAndReadBack(t *testing.T) {
	f, err := seitoml.New("full", "seid test")
	if err != nil {
		t.Fatal(err)
	}
	want := []any{[]any{"chain_id", "pacific-1"}, []any{"region", "euw1"}}
	if err := f.Set("telemetry.global-labels", want); err != nil {
		t.Fatalf("write a list of pairs: %v", err)
	}

	raw, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	reread, err := seitoml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("the written file does not parse: %v\n\n%s", err, raw)
	}
	got, present, err := reread.Get("telemetry.global-labels")
	if err != nil || !present {
		t.Fatalf("read back: present=%v err=%v\n\n%s", present, err, raw)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("read back as %#v, want %#v", got, want)
	}
}

// TestAnEmptyLabelSetIsWritable is the default the telemetry section carries.
func TestAnEmptyLabelSetIsWritable(t *testing.T) {
	f, err := seitoml.New("full", "seid test")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("telemetry.global-labels", []any{}); err != nil {
		t.Fatalf("the default value cannot be written, so generate would fail on this section: %v", err)
	}
	raw, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "global-labels = []") {
		t.Errorf("an absent label set rendered as something other than an empty list:\n\n%s", raw)
	}
}
