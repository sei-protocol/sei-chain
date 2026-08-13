package seitoml_test

import (
	"reflect"
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
