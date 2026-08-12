package configtest

import (
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
)

// RecordingAppOpts wraps a configuration source and records every key asked of it.
//
// It answers which keys a run actually reads. A written list of that is a guess: a key reaches its
// reader as a dotted string built at the call site, so only running the code and watching finds the
// full set. Answers come from the wrapped source, so wrapping changes nothing the caller sees.
//
// Safe for concurrent use, because an application construction reads configuration from more than
// one goroutine
// and a recorder that raced would be a flaky test rather than a wrong answer.
type RecordingAppOpts struct {
	// Inner is the source that answers. A nil Inner answers nil for every key, which is a valid
	// source: a boot reading an absent key takes its own default.
	Inner interface{ Get(string) any }

	mu     sync.Mutex
	counts map[string]int
}

// Record wraps a source.
func Record(inner interface{ Get(string) any }) *RecordingAppOpts {
	return &RecordingAppOpts{Inner: inner}
}

// Get records the key and returns what the wrapped source answers.
func (r *RecordingAppOpts) Get(key string) any {
	r.mu.Lock()
	if r.counts == nil {
		r.counts = map[string]int{}
	}
	r.counts[key]++
	r.mu.Unlock()

	if r.Inner == nil {
		return nil
	}
	return r.Inner.Get(key)
}

// Keys returns every key that was asked for, sorted.
//
// Sorted rather than in the order they arrived, because the order depends on which goroutine got
// there first and a record that moved between runs could not be compared.
func (r *RecordingAppOpts) Keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.counts))
	for k := range r.counts {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Count is how many times a key was asked for.
//
// A key read more than once is worth seeing: each read is a separate chance for two call sites to
// disagree about the spelling or the cast, which is the shape of defect this suite exists to find.
func (r *RecordingAppOpts) Count(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[key]
}

// Total is how many reads happened, across all keys.
func (r *RecordingAppOpts) Total() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	total := 0
	for _, n := range r.counts {
		total += n
	}
	return total
}

// Under returns the recorded keys inside a dotted section, sorted.
//
// The section's own name is included when it was read directly, since a reader that asks for a whole
// table is as much a consumer of that section as one asking for a leaf.
func (r *RecordingAppOpts) Under(section string) []string {
	prefix := section + "."
	var out []string
	for _, k := range r.Keys() {
		if k == section || strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out
}

// CheckObservedKeys records the keys a run read, keyed by name.
//
// The record is the point. Reading the tree cannot find a key that reaches its reader only at run
// time, so this is the one place that holds the full set, and a key appearing or disappearing from
// it changes what the node reads. Read counts stay out: they move with unrelated refactoring, where
// the key set moves only when a reader arrives or goes.
func CheckObservedKeys(t testing.TB, name string, observed []string) {
	t.Helper()

	var b strings.Builder
	b.WriteString("# Keys read from configuration during the recorded run. Regenerate with -update.\n")
	b.WriteString("# One line per key, sorted, so adding a reader shows as one added line.\n")
	b.WriteString("#\n")
	b.WriteString("# Recorded by wrapping the source the run is given, because a key reaches a reader\n")
	b.WriteString("# through a string built at the call site and cannot be found by reading the tree.\n")
	b.WriteString("# How many times each key was read is left out: that moves with refactoring, where\n")
	b.WriteString("# this set moves only when a reader is added or removed.\n\n")

	sorted := make([]string, len(observed))
	copy(sorted, observed)
	sort.Strings(sorted)
	if len(sorted) == 0 {
		b.WriteString("(no keys were read, which means the run under test did not happen)\n")
	}
	for _, k := range sorted {
		b.WriteString(k + "\n")
	}

	got := strings.TrimRight(b.String(), "\n")
	path := goldenFilePath(t, name, ".observed.golden")

	if goldenUpdateRequested() {
		writeGolden(t, name, path, got)
		return
	}
	raw, err := os.ReadFile(path) //nolint:gosec // testdata/<name>.observed.golden; goldenFilePath validates the name
	if err != nil {
		t.Fatalf("%s: cannot read %s (%v).\nIf this record is new, create it by running this test "+
			"with -update and reviewing the recorded keys as part of the change.", name, path, err)
	}
	if want := recordText(raw); got != want {
		t.Fatalf("%s: the keys this run reads no longer match %s.\n%s\n"+
			"A new line is a reader that was added and a removed line is one that no longer runs. "+
			"Both change what an operator's file has to carry, so regenerate with -update and keep "+
			"the diff in the review.", name, path, goldenDiff(want, got))
	}
}
