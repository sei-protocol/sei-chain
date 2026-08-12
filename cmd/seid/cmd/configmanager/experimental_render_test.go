package configmanager

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/experimental"
)

// Key names reach the log through one helper, and truncation is visible when it happens.
//
// One helper rather than one render per call site, so no record can be the one that forgot. A key
// name comes from an operator's file, so it can carry anything a TOML key can carry.

// captured is a logging that records what it was handed, so a test asserts on the arguments a
// record was built from rather than on a formatted line.
type captured struct {
	warns  []record
	errors []record
}

type record struct {
	msg  string
	args []any
}

func (c *captured) Warn(msg string, args ...any)  { c.warns = append(c.warns, record{msg, args}) }
func (c *captured) Error(msg string, args ...any) { c.errors = append(c.errors, record{msg, args}) }

// arg returns the value that followed key in a record's arguments.
func (r record) arg(key string) (any, bool) {
	for i := 0; i+1 < len(r.args); i += 2 {
		if k, ok := r.args[i].(string); ok && k == key {
			return r.args[i+1], true
		}
	}
	return nil, false
}

// TestANameCannotForgeALogLine is the property a key name makes possible.
//
// A name arrives from an operator's file, so it can hold a newline, a CRLF, an ANSI escape or a NUL.
// Rendered raw, any of those lets a key name inject a line that reads as the node's own output.
// QuoteToASCII is what makes that impossible, and it is applied in one place.
func TestANameCannotForgeALogLine(t *testing.T) {
	for _, tc := range []struct {
		name, raw, mustNotContain string
	}{
		{"newline", "experimental.a\nlevel=INFO msg=\"forged\"", "\n"},
		{"carriage return", "experimental.a\r\nforged", "\r"},
		{"ansi escape", "experimental.a\x1b[31mred", "\x1b"},
		{"nul", "experimental.a\x00b", "\x00"},
		{"non-ascii", "experimental.aéb", "é"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := logName(tc.raw)

			if strings.Contains(got, tc.mustNotContain) {
				t.Errorf("logName(%q) = %s, which still carries the raw byte. A key name comes from an "+
					"operator's file, so rendered raw it can inject a line that reads as the node's own "+
					"output", tc.raw, got)
			}
			if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
				t.Errorf("logName(%q) = %s, which is not quoted; an unquoted name has no boundary a "+
					"reader can trust", tc.raw, got)
			}
		})
	}
}

// TestTruncationIsVisibleAndAscii holds that a shortened name says so, in plain ASCII.
//
// A name is resolvable up to MaxKeyBytes and readable up to MaxLoggedNameBytes, so a name between
// them is classified normally and rendered short. That is only safe if the reader can tell.
func TestTruncationIsVisibleAndAscii(t *testing.T) {
	long := "experimental." + strings.Repeat("a", experimental.MaxLoggedNameBytes)

	got := logName(long)

	if !strings.Contains(got, "...") {
		t.Errorf("logName on an over-long name produced %s with no truncation mark, so a reader takes "+
			"a shortened name for the whole one", got)
	}
	if strings.Contains(got, `\u`) {
		t.Errorf("logName produced %s, which carries an escape sequence as its mark. A Unicode "+
			"ellipsis renders as \\u2026 through QuoteToASCII and reads as part of the name", got)
	}
	// Inside the quotes, so the mark cannot be mistaken for the surrounding format's own syntax.
	if trimmed := strings.TrimSuffix(got, `"`); !strings.HasSuffix(trimmed, "...") {
		t.Errorf("the truncation mark in %s is outside the quotes", got)
	}
	if short := logName("experimental.a.b"); strings.Contains(short, "...") {
		t.Errorf("a name within the limit was marked truncated: %s", short)
	}
}

// TestATruncatedRecordCarriesItsLimits holds the two fields a shortened name owes a reader.
//
// Without them a reader cannot tell how much was cut, and Nearest is dropped because it is computed
// on the full name and would otherwise sit beside a token describing a different string.
func TestATruncatedRecordCarriesItsLimits(t *testing.T) {
	long := "experimental." + strings.Repeat("b", experimental.MaxLoggedNameBytes)
	var c captured

	logFindings(&c, experimental.Findings{
		Unrecognized: []experimental.Unrecognized{
			{Key: long, Nearest: "experimental.something.close"},
			{Key: "experimental.short.key", Nearest: "experimental.short.keys"},
		},
	})

	var rec record
	for _, w := range c.warns {
		if strings.Contains(w.msg, "unrecognized") {
			rec = w
		}
	}
	if rec.msg == "" {
		t.Fatal("no unrecognized record was emitted")
	}
	for _, field := range []string{"truncated", "maxbytes"} {
		if _, ok := rec.arg(field); !ok {
			t.Errorf("the record omits %q, so a reader cannot tell that a rendered name is shorter "+
				"than what their file holds. args=%v", field, rec.args)
		}
	}
	nearest, _ := rec.arg("nearest")
	list, ok := nearest.([]string)
	if !ok || len(list) != 2 {
		t.Fatalf("nearest rendered as %#v, want two entries", nearest)
	}
	if list[0] != "" {
		t.Errorf("the truncated name carries nearest=%q. It is computed on the full name, so beside a "+
			"shortened one it describes a different string", list[0])
	}
	if list[1] == "" {
		t.Error("the name within the limit lost its nearest, so the assertion above would hold for a " +
			"renderer that dropped every suggestion")
	}
}

// TestARecordWithNothingTruncatedOmitsTheFields is the other direction.
//
// A field present on every record teaches a reader to ignore it, so it appears only when something
// was actually cut.
func TestARecordWithNothingTruncatedOmitsTheFields(t *testing.T) {
	var c captured

	logFindings(&c, experimental.Findings{
		Unrecognized: []experimental.Unrecognized{{Key: "experimental.a.b"}},
	})

	for _, w := range c.warns {
		if !strings.Contains(w.msg, "unrecognized") {
			continue
		}
		if _, ok := w.arg("truncated"); ok {
			t.Errorf("a record with nothing truncated carries the field anyway: %v. A field on every "+
				"record is one a reader learns to skip", w.args)
		}
	}
}

// TestTheKeyListIsCappedAndSaysSo bounds one record.
//
// A rollback can make a whole feature's key set unrecognized at once. An unbounded record is
// dropped by some log shippers and split by others, so the count is what an operator acts on and
// the list is what they grep.
func TestTheKeyListIsCappedAndSaysSo(t *testing.T) {
	var us []experimental.Unrecognized
	for i := 0; i < maxReportedKeys*3; i++ {
		us = append(us, experimental.Unrecognized{Key: fmt.Sprintf("experimental.bulk.k%02d", i)})
	}
	var c captured

	logFindings(&c, experimental.Findings{Unrecognized: us})

	for _, w := range c.warns {
		if !strings.Contains(w.msg, "unrecognized") {
			continue
		}
		count, _ := w.arg("count")
		if count != len(us) {
			t.Errorf("the record reports count=%v for %d keys; the full count is what an operator "+
				"alerts on even when the list is cut", count, len(us))
		}
		omitted, _ := w.arg("omitted")
		if omitted != len(us)-maxReportedKeys {
			t.Errorf("the record reports omitted=%v, want %d", omitted, len(us)-maxReportedKeys)
		}
		keys, _ := w.arg("keys")
		if list, ok := keys.([]string); !ok || len(list) != maxReportedKeys {
			t.Errorf("the record rendered %d keys with a cap of %d", len(list), maxReportedKeys)
		}
		return
	}
	t.Fatal("no unrecognized record was emitted")
}

// TestLevelsAreFixed holds which class lands at which level.
//
// One summary at error level gives an operator a single line to alert on. A shadow is also error,
// because the operator's value is silently gone: the key is written, it resolves to nothing, and
// the declared default is what runs. Everything else is warn.
func TestLevelsAreFixed(t *testing.T) {
	var c captured

	logFindings(&c, experimental.Findings{
		Unrecognized: []experimental.Unrecognized{{Key: "experimental.a.b"}},
		Shadowed:     []experimental.ShadowFinding{{Key: "experimental.c.d", Cause: "SEID_EXPERIMENTAL_C"}},
		Promoted:     []experimental.PromotedKey{{Key: "experimental.e.f", PromotedTo: "e.f", RetiredIn: "v6.7"}},
		// The env pass is reported separately when it did not run, and this test's subject is which
		// class lands at which level, so the fixture says it ran.
		EnvPassRan: true,
	})

	if len(c.errors) != 2 {
		t.Errorf("got %d error records, want 2: the summary and the shadow. Levels are fixed so an "+
			"alert can match on them", len(c.errors))
		for _, e := range c.errors {
			t.Logf("  error: %s", e.msg)
		}
	}
	if len(c.warns) != 2 {
		t.Errorf("got %d warn records, want 2: the unrecognized list and the promoted key", len(c.warns))
	}
}

// TestNothingIsEmittedWhenClean holds silence at the reporter rather than through a whole binary.
//
// Total silence when clean is what makes a node with no experimental keys byte-identical on every
// command, and checking it here means reading the records built rather than diffing output.
func TestNothingIsEmittedWhenClean(t *testing.T) {
	var c captured

	logFindings(&c, experimental.Findings{})

	if len(c.warns)+len(c.errors) != 0 {
		t.Errorf("a clean sweep emitted %d records; every node with a well-formed section would then "+
			"get output on every application command", len(c.warns)+len(c.errors))
	}
}
