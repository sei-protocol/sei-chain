package gate

import (
	"fmt"
	"strings"
)

// Exit codes. Distinguished because they mean different things to whoever is reading them: a gate that
// ran and disagreed with the record is a finding to act on, while a gate that could not run is a
// broken instrument and says nothing about the suite.
const (
	// ExitOK means every row behaved as recorded.
	ExitOK = 0
	// ExitFailures means the gate ran and at least one row disagreed with its record.
	ExitFailures = 1
	// ExitAborted means the gate could not run.
	ExitAborted = 2
)

// ExitCode maps a result to the code the process exits with.
func (r Result) ExitCode() int {
	switch {
	case r.Aborted:
		return ExitAborted
	case len(r.Failures) > 0:
		return ExitFailures
	default:
		return ExitOK
	}
}

// Summary is the account of what the run observed.
//
// Built as a string rather than written straight out, so the same text is what a test asserts on and
// what a reader sees. A summary assembled by a dozen writes can only be checked by capturing a writer,
// which tests the plumbing as much as the wording.
//
// The counts appear on success as well as on failure, because "OK" over an unstated number of
// observations is the claim this gate exists to refuse to make. A reader should see that it observed
// something without having to trust that it would have said otherwise.
func (r Result) Summary() string {
	var b strings.Builder

	fmt.Fprintf(&b, "\nrows read: %d   mutations observed: %d   not observable: %d\n",
		r.RowsRead, r.Observed, r.Unobservable)

	for _, failure := range r.Failures {
		fmt.Fprintf(&b, "\nGATE FAILURE: %s\n", indentContinuation(failure))
	}

	switch {
	case r.Aborted:
		b.WriteString("\nMUTATION GATE: ABORTED\n")
	case len(r.Failures) > 0:
		fmt.Fprintf(&b, "\nMUTATION GATE: %d FAILURE(S)\n", len(r.Failures))
	default:
		fmt.Fprintf(&b, "\nMUTATION GATE: OK (%d mutation(s) observed)\n", r.Observed)
	}
	return b.String()
}

// indentContinuation lines up a multi-line failure under its first line, so a failure that explains
// itself over several sentences reads as one item rather than as several.
func indentContinuation(s string) string {
	return strings.ReplaceAll(s, "\n", "\n              ")
}
