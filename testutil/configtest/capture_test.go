package configtest

import (
	"flag"
	"fmt"
	"strings"
	"testing"
)

// This file lets the harness's own checks be tested the way the suites they guard are
// tested: by provoking a failure and reading it.
//
// A check that reports through testing.TB cannot be asserted on directly, because the
// report fails the test doing the asserting. So the check runs against a stand-in TB that
// collects what it is told, on a goroutine of its own so that a Fatalf can end the call the
// way the real one does. What comes back is the text a failing suite would print, which is
// the artifact worth pinning: these messages are read by someone who has just broken
// something and does not yet know what.

// captureTB collects a check's failures instead of failing the test that provoked them.
//
// It embeds *testing.T for the rest of the testing.TB surface, including the unexported
// method that makes the interface unimplementable from outside. Anything not overridden
// therefore still reaches the real T: a check that logs, logs, and a check that reaches a
// method this does not model does so visibly rather than silently.
type captureTB struct {
	*testing.T
	failures []string
	// fatal records that the check gave up rather than reporting and carrying on. Which of the
	// two a check uses is part of its contract — CheckKeyNames reports so that a package's later
	// sections are still checked, goldenFilePath gives up because a path it cannot confine is not
	// something to continue past — so a test of one asserts which it got.
	fatal bool
}

// Errorf records a non-fatal failure.
func (c *captureTB) Errorf(format string, args ...any) {
	c.failures = append(c.failures, fmt.Sprintf(format, args...))
}

// Fatalf records a failure and ends the goroutine, which is what the real Fatalf does and
// what a check written around it assumes. Recording without ending would run the rest of a
// check that had already given up.
func (c *captureTB) Fatalf(format string, args ...any) {
	c.Errorf(format, args...)
	c.fatal = true
	panic(captureFatal{})
}

// captureFatal is the value Fatalf panics with. A panic rather than runtime.Goexit,
// because Goexit from a goroutine the testing package did not start reports as
// "test executed panic(nil) or runtime.Goexit" and would take the whole run down; this is
// recovered by capture and never escapes it.
type captureFatal struct{}

// capture runs check against a stand-in TB and returns everything it reported.
func capture(t *testing.T, check func(testing.TB)) *captureTB {
	t.Helper()
	c := &captureTB{T: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(captureFatal); !ok {
					panic(r)
				}
			}
		}()
		check(c)
	}()
	<-done
	return c
}

// only returns the single failure a check was expected to report, failing the test when
// the count is anything else. A check that reports twice where one report was expected is
// a check whose messages a reader has to reconcile, so the count is asserted rather than
// indexed past.
func (c *captureTB) only(t *testing.T) string {
	t.Helper()
	if len(c.failures) != 1 {
		t.Fatalf("expected exactly one failure, got %d:\n%s", len(c.failures),
			strings.Join(c.failures, "\n---\n"))
	}
	return c.failures[0]
}

// mentioning returns the failures containing want, so a test can assert on the report for
// one row out of several.
func (c *captureTB) mentioning(want string) []string {
	var out []string
	for _, f := range c.failures {
		if strings.Contains(f, want) {
			out = append(out, f)
		}
	}
	return out
}

// withUpdateFlag turns -update on for one test and restores it afterwards.
//
// The flag is process-global, which is why the harness guide says to pass it per package.
// Flipping it here is contained by two facts about this package: nothing in it records a
// golden of its own, and none of its tests call t.Parallel, so no other test observes the
// window in which it is on.
// It also sets allowRecordWriteUnderCI, because the tests that call this helper have to observe a
// write actually happening, and CI is where they run. This is the only assignment of that variable
// in the tree: it is unexported, so nothing outside package configtest can reach it, and it is set
// through this helper rather than per test so there is one place to look for callers.
func withUpdateFlag(t *testing.T) {
	t.Helper()
	f := flag.Lookup("update")
	if f == nil {
		t.Fatal("the -update flag is not registered, so the golden helpers cannot be exercised")
	}
	previous := f.Value.String()
	if err := f.Value.Set("true"); err != nil {
		t.Fatalf("set -update: %v", err)
	}
	previouslyAllowed := allowRecordWriteUnderCI
	allowRecordWriteUnderCI = true
	t.Cleanup(func() {
		allowRecordWriteUnderCI = previouslyAllowed
		if err := f.Value.Set(previous); err != nil {
			t.Errorf("restore -update to %q: %v", previous, err)
		}
	})
}
