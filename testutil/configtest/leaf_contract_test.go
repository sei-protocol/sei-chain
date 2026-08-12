package configtest

import (
	"strings"
	"testing"
)

// This file tests the machinery every row assertion in the tree flows through.
//
// isLeafLine decides which rendered lines belong to a field, and leafAt, spliceLeaf and dropLeaf all
// scope themselves with it. None of them had a test of its own, which left the suite's central
// comparison resting on an untested predicate: widening isLeafLine to a bare prefix match reddens one
// package, incidentally, and gutting the comparison in assertResolvedView reddens nothing at all.
//
// The shapes below are Dump's, so a change to how a field renders shows up here rather than as a row
// assertion that quietly stops scoping itself correctly.

// TestIsLeafLineMatchesTheThreeShapesOfAField pins what the predicate's own documentation claims: a
// field is its scalar line, the indexed lines a slice or map renders as, and the dotted lines a
// struct-valued field renders as.
func TestIsLeafLineMatchesTheThreeShapesOfAField(t *testing.T) {
	for _, c := range []struct {
		what  string
		line  string
		path  string
		match bool
	}{
		{"its own scalar line", "Enable = bool(true)", "Enable", true},
		{"the explicit nil line", "Enable = <nil>", "Enable", true},
		{"an indexed line from a slice", "Peers[0] = string(\"a\")", "Peers", true},
		{"an indexed line from a map", "Labels[\"k\"] = string(\"v\")", "Labels", true},
		{"a dotted line from a sub-struct", "FlatKV.Fsync = bool(false)", "FlatKV", true},
		{"the sub-struct's own line", "FlatKV = <nil>", "FlatKV", true},

		{"an unrelated field", "Backend = string(\"pebbledb\")", "Enable", false},
		{"a sibling sharing a prefix", "EnableMetrics = bool(true)", "Enable", false},
		{"a sibling sharing a prefix, indexed", "EnableList[0] = int(1)", "Enable", false},
		{"a parent when the path is the child", "FlatKV = <nil>", "FlatKV.Fsync", false},
	} {
		t.Run(c.what, func(t *testing.T) {
			if got := isLeafLine(c.line, c.path); got != c.match {
				t.Errorf("isLeafLine(%q, %q) = %v, want %v", c.line, c.path, got, c.match)
			}
		})
	}
}

// TestIsLeafLineDoesNotSwallowASibling is the case the predicate's documentation calls out by name, and
// the one a bare prefix match gets wrong.
//
// Held separately from the table because it is the failure that motivated the shape of the predicate:
// with `strings.HasPrefix(line, path)` alone, a path of Enable claims EnableMetrics, and every operation
// scoped by it then reads, splices or drops a field the row never named.
func TestIsLeafLineDoesNotSwallowASibling(t *testing.T) {
	const dump = "Enable = bool(true)\nEnableMetrics = bool(false)\nEnableReadWriteMetrics = bool(false)"

	leaf, ok := leafAt(dump, "Enable")
	if !ok {
		t.Fatal("leafAt could not find Enable")
	}
	// The whole rendered line, not the bare value: leafAt, spliceLeaf and dropLeaf all deal in lines,
	// and leafOf is the one that renders a value without its path. Pinned here because the distinction
	// is not obvious from the names and a caller that assumed otherwise would compare a line against a
	// value and always disagree.
	if leaf != "Enable = bool(true)" {
		t.Errorf("leafAt(Enable) = %q, want the whole line — a sibling was swallowed", leaf)
	}

	// dropLeaf scopes itself the same way, so the siblings have to survive it.
	remaining := dropLeaf(dump, "Enable")
	for _, sibling := range []string{"EnableMetrics", "EnableReadWriteMetrics"} {
		if !strings.Contains(remaining, sibling) {
			t.Errorf("dropLeaf(Enable) removed %s:\n%s", sibling, remaining)
		}
	}
	if strings.Contains(remaining, "Enable = ") {
		t.Errorf("dropLeaf(Enable) left the field it was asked to drop:\n%s", remaining)
	}
}

// TestPathScopedOperationsAgreeOnWhichLinesBelong pins the invariant isLeafLine exists to provide, which
// its documentation states in prose and nothing asserted.
//
// Reading, splicing and dropping a field must claim the same lines. If they disagree, a row's assertion
// compares a document spliced one way against a document read another way, and the mismatch reports as a
// reader defect rather than as the harness disagreeing with itself.
func TestPathScopedOperationsAgreeOnWhichLinesBelong(t *testing.T) {
	const dump = "Enable = bool(true)\n" +
		"FlatKV.Fsync = bool(false)\n" +
		"FlatKV.Dir = string(\"/x\")\n" +
		"FlatKVLegacy = bool(true)\n" +
		"Peers[0] = string(\"a\")\n" +
		"Peers[1] = string(\"b\")\n" +
		"PeersCount = int(2)"

	for _, path := range []string{"Enable", "FlatKV", "Peers"} {
		t.Run(path, func(t *testing.T) {
			claimed := 0
			for _, line := range strings.Split(dump, "\n") {
				if isLeafLine(line, path) {
					claimed++
				}
			}
			if claimed == 0 {
				t.Fatalf("isLeafLine claims no line for %q, so the case tests nothing", path)
			}

			// dropLeaf must remove exactly the claimed lines.
			left := strings.Count(dropLeaf(dump, path), "\n") + 1
			if want := strings.Count(dump, "\n") + 1 - claimed; left != want {
				t.Errorf("dropLeaf(%q) left %d lines, want %d: it does not remove the same lines "+
					"isLeafLine claims", path, left, want)
			}

			// spliceLeaf must find the field isLeafLine claims.
			if _, ok := spliceLeaf(dump, path, "REPLACED"); !ok {
				t.Errorf("spliceLeaf(%q) reported the path absent, but isLeafLine claims %d line(s) "+
					"for it", path, claimed)
			}

			// leafAt must too, for the shapes that have a single readable leaf.
			if _, ok := leafAt(dump, path); !ok && path == "Enable" {
				t.Errorf("leafAt(%q) reported the path absent", path)
			}
		})
	}
}

// TestSpliceLeafReportsAnAbsentPath pins the second return value.
//
// Its callers treat false as "the row names a field the resolved view does not contain" and fail with
// that diagnosis, so a splice that silently reported success would turn a mis-specified row into a
// passing one.
func TestSpliceLeafReportsAnAbsentPath(t *testing.T) {
	const dump = "Enable = bool(true)"

	if _, ok := spliceLeaf(dump, "Missing", "X"); ok {
		t.Error("spliceLeaf reported success for a path the dump does not contain")
	}

	// The replacement is a whole line, which is why every caller builds it as path + " = " + value.
	spliced, ok := spliceLeaf(dump, "Enable", "Enable = bool(false)")
	if !ok {
		t.Fatal("spliceLeaf reported failure for a path the dump does contain")
	}
	if spliced != "Enable = bool(false)" {
		t.Errorf("spliceLeaf produced %q", spliced)
	}
}

// TestAssertResolvedViewFailsWhenAnUndeclaredFieldMoves is the comparison gap 01 named.
//
// The check compares the whole resolved document, so a reader that lands its own key correctly and also
// perturbs a field the row never declared fails. Gutting that comparison left every package in the tree
// green, because nothing exercised it: the assertion was the only thing asserting it, and it had no test.
func TestAssertResolvedViewFailsWhenAnUndeclaredFieldMoves(t *testing.T) {
	type config struct {
		Named      bool
		Undeclared int
	}
	spec := KeySpec{Key: "section.named", Path: "Named", Cast: CastBool}

	t.Run("a reader that moves only its own field passes", func(t *testing.T) {
		got := capture(t, func(tb testing.TB) {
			assertResolvedView(tb, "section", spec,
				config{Named: false, Undeclared: 7},
				config{Named: true, Undeclared: 7},
				"Named = bool(true)")
		})
		if len(got.failures) != 0 {
			t.Errorf("a faithful reader was reported as failing: %v", got.failures)
		}
	})

	t.Run("a reader that also moves an undeclared field fails", func(t *testing.T) {
		got := capture(t, func(tb testing.TB) {
			assertResolvedView(tb, "section", spec,
				config{Named: false, Undeclared: 7},
				config{Named: true, Undeclared: 99},
				"Named = bool(true)")
		})
		if len(got.failures) == 0 {
			t.Fatal("a reader that perturbed a field the row does not declare was accepted, so the " +
				"comparison asserts nothing")
		}
		if !strings.Contains(got.failures[0], "does not declare") {
			t.Errorf("failed, but not with the undeclared-field diagnosis: %q", got.failures[0])
		}
	})

	t.Run("an undeclared field named in AlsoWrites is exempt", func(t *testing.T) {
		declared := spec
		declared.AlsoWrites = []string{"Undeclared"}
		got := capture(t, func(tb testing.TB) {
			assertResolvedView(tb, "section", declared,
				config{Named: false, Undeclared: 7},
				config{Named: true, Undeclared: 99},
				"Named = bool(true)")
		})
		if len(got.failures) != 0 {
			t.Errorf("a field the row declares in AlsoWrites was still reported: %v", got.failures)
		}
	})
}
