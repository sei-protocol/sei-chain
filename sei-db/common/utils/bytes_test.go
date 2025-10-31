package utils

import (
	"strings"
	"testing"
)

func TestClone(t *testing.T) {
	var cloneTests = [][]byte{
		[]byte(nil),
		Clone([]byte{}),
		[]byte(strings.Repeat("a", 42))[:0],
		[]byte(strings.Repeat("a", 42))[:0:0],
		[]byte("short"),
		[]byte(strings.Repeat("a", 42)),
	}
	for _, input := range cloneTests {
		clone := Clone(input)
		if !Equal(clone, input) {
			t.Errorf("Clone(%q) = %q; want %q", input, clone, input)
		}

		if input == nil && clone != nil {
			t.Errorf("Clone(%#v) return value should be equal to nil slice.", input)
		}

		if input != nil && clone == nil {
			t.Errorf("Clone(%#v) return value should not be equal to nil slice.", input)
		}
	}
}
