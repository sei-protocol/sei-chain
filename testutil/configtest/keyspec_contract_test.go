package configtest

import (
	"testing"

	"github.com/spf13/cast"
)

// TestCastZeroMatchesTheUncheckedCastOfNil pins the invariant CheckKey's nil branch rests on.
//
// For an unguarded reader, `field = cast.ToX(opts.Get(k))` with a nil value writes whatever
// the unchecked cast returns for nil. CheckKey predicts that from CastKind.zero(), so the two
// have to agree exactly, including whether a slice comes back nil or empty: Dump renders
// []int(nil) as <nil-slice> and []int{} as <empty-slice>, and a mismatch would fail the row
// with a diff that points at the reader instead of at this table.
//
// It is asserted rather than commented because the agreement depends on spf13/cast's
// behavior, not on ours. cast v1.10.0 routes every slice conversion through one generic path
// that returns a nil slice for a nil input. A bump that changes that fails here, next to the
// assumption, rather than inside a section reader's row.
func TestCastZeroMatchesTheUncheckedCastOfNil(t *testing.T) {
	unchecked := map[CastKind]func(any) any{
		CastBool:        func(v any) any { return cast.ToBool(v) },
		CastString:      func(v any) any { return cast.ToString(v) },
		CastInt:         func(v any) any { return cast.ToInt(v) },
		CastInt64:       func(v any) any { return cast.ToInt64(v) },
		CastUint:        func(v any) any { return cast.ToUint(v) },
		CastUint32:      func(v any) any { return cast.ToUint32(v) },
		CastUint64:      func(v any) any { return cast.ToUint64(v) },
		CastFloat64:     func(v any) any { return cast.ToFloat64(v) },
		CastDuration:    func(v any) any { return cast.ToDuration(v) },
		CastStringSlice: func(v any) any { return cast.ToStringSlice(v) },
		CastIntSlice:    func(v any) any { return cast.ToIntSlice(v) },
	}

	for kind, apply := range unchecked {
		got := DumpAt("Field", apply(nil))
		want := DumpAt("Field", kind.zero())
		if got != want {
			t.Errorf("%s: cast.To%s(nil) renders as %q but zero() renders as %q. CheckKey's nil "+
				"branch predicts an unguarded reader's field from zero(), so the two must agree",
				kind, kind, got, want)
		}
	}

	// Bounded by castKindCount, not by len(unchecked): a kind added to the enum without a
	// matching entry here is still visited, so the missing entry is reported rather than
	// stepped over.
	for k := CastKind(0); k < castKindCount; k++ {
		if _, ok := unchecked[k]; !ok {
			t.Errorf("CastKind %s (%d) has no entry here. A kind added without a zero() case "+
				"silently falls through to nil, and every unguarded nil-value row for it would "+
				"then compare against <nil>, so add it here and to zero()", k, int(k))
		}
	}
}
