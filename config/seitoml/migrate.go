package seitoml

import (
	"fmt"
	"sort"
)

// Migration transforms a file from one schema version to the next.
//
// A migration that has shipped never changes. Every node that runs it must transform its
// configuration the same way, because a migration that behaved differently in a later release
// would leave two nodes with files that agree on their version and disagree on their contents.
// Fixture is what makes that enforceable rather than a request: it is recorded alongside the
// result of applying this migration to it, so editing Apply moves a recorded file and a reviewer
// sees exactly which shipped migration changed.
type Migration struct {
	// To is the schema version this produces. It is always one more than the version it accepts.
	To int
	// Summary is the one line an operator sees for this step.
	Summary string
	// Fixture is a file at version To-1 that exercises what this migration changes.
	Fixture string
	// Apply performs the transformation. It must not write the schema version; the caller does
	// that, so a migration cannot leave the file claiming a version its contents do not match.
	Apply func(*File) error
}

// Step is what one migration did.
type Step struct {
	// To is the version the file reached.
	To int
	// Summary is the migration's own description.
	Summary string
	// Changed names every key the step added, removed or altered, sorted.
	Changed []string
}

// migrations is the frozen chain, in ascending order, appended to and never edited.
//
// Empty because the schema is at its first version and nothing has needed transforming yet. The
// first entry added here must produce version 2 and must raise SchemaVersion in the same change,
// which a test enforces.
var migrations []Migration

// Migrations returns the frozen chain.
func Migrations() []Migration {
	out := make([]Migration, len(migrations))
	copy(out, migrations)
	return out
}

// Upgrade runs every migration the file at path still needs.
//
// One write per step, each atomic, so the file on disk only ever holds a version some migration
// produced. A crash part way through leaves a file at an earlier valid version that the next run
// carries forward, rather than one whose contents belong to no version at all.
//
// With dryRun set nothing is written and the returned steps are exactly what a real run would do,
// which is what makes a preview worth trusting.
func Upgrade(path string, chain []Migration, dryRun bool) ([]Step, error) {
	if err := ValidateChain(chain); err != nil {
		return nil, err
	}
	file, err := Load(path)
	if err != nil {
		return nil, err
	}
	from, err := file.Version()
	if err != nil {
		return nil, err
	}
	pending, err := Pending(from, chain)
	if err != nil {
		return nil, err
	}

	steps := make([]Step, 0, len(pending))
	for _, m := range pending {
		step, err := apply(file, m)
		if err != nil {
			return steps, fmt.Errorf("migrating to version %d (%s): %w", m.To, m.Summary, err)
		}
		if !dryRun {
			if err := file.Save(path); err != nil {
				return steps, fmt.Errorf("saving version %d: %w", m.To, err)
			}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// apply runs one migration and records which keys it moved.
//
// The version is written here rather than inside Apply, so a migration cannot leave the file
// claiming a version its contents do not match.
func apply(file *File, m Migration) (Step, error) {
	before, err := file.Values()
	if err != nil {
		return Step{}, err
	}
	if m.Apply == nil {
		return Step{}, fmt.Errorf("migration to version %d has no Apply", m.To)
	}
	if err := m.Apply(file); err != nil {
		return Step{}, err
	}
	if err := file.setVersion(m.To); err != nil {
		return Step{}, err
	}
	after, err := file.Values()
	if err != nil {
		return Step{}, err
	}
	return Step{To: m.To, Summary: m.Summary, Changed: changedKeys(before, after)}, nil
}

// changedKeys names every key that differs between two readings, sorted.
func changedKeys(before, after map[string]any) []string {
	seen := map[string]bool{}
	for k, v := range before {
		if av, present := after[k]; !present || !sameValue(v, av) {
			seen[k] = true
		}
	}
	for k := range after {
		if _, present := before[k]; !present {
			seen[k] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sameValue compares two read values, including lists.
func sameValue(a, b any) bool {
	as, aok := a.([]any)
	bs, bok := b.([]any)
	if aok || bok {
		if !aok || !bok || len(as) != len(bs) {
			return false
		}
		for i := range as {
			if !sameValue(as[i], bs[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// Pending returns the migrations a file at version from still needs.
//
// A file already current needs none. A file from a newer binary is refused rather than reported
// current: its keys were written against a schema this binary does not know, so treating it as
// finished would boot a node against a file it cannot fully read while reporting success.
func Pending(from int, chain []Migration) ([]Migration, error) {
	current := currentVersion(chain)
	switch {
	case from > current:
		return nil, fmt.Errorf("sei.toml is at schema version %d and this binary knows version %d. "+
			"It was written by a newer seid, so its keys were written against a schema this binary "+
			"does not have. Run the newer binary, or regenerate the file with this one", from, current)
	case from < 1:
		return nil, fmt.Errorf("sei.toml is at schema version %d, which no release produced", from)
	}

	var pending []Migration
	for _, m := range chain {
		if m.To > from {
			pending = append(pending, m)
		}
	}
	return pending, nil
}

// ValidateChain refuses a chain that could not have been produced by appending to it.
//
// Every migration raises the version by exactly one, starting at 2. A gap would leave a file at a
// version nothing can move forward, and a repeat would mean two migrations claim to produce the
// same version so the order between them decides the result.
func ValidateChain(chain []Migration) error {
	for i, m := range chain {
		want := i + 2
		if m.To != want {
			return fmt.Errorf("migration %d of %d produces version %d, want %d: the chain is built by "+
				"appending, so each step raises the version by exactly one starting at 2",
				i+1, len(chain), m.To, want)
		}
		if m.Summary == "" {
			return fmt.Errorf("the migration to version %d has no summary, so an operator running "+
				"upgrade is shown a step with no description", m.To)
		}
		if m.Apply == nil {
			return fmt.Errorf("the migration to version %d has no Apply", m.To)
		}
		if m.Fixture == "" {
			return fmt.Errorf("the migration to version %d has no fixture. Without one nothing records "+
				"what it does, and editing a shipped migration would change how every node "+
				"transforms its configuration with no test failing", m.To)
		}
	}
	return nil
}

// currentVersion is the version a chain produces.
func currentVersion(chain []Migration) int {
	if len(chain) == 0 {
		return 1
	}
	return chain[len(chain)-1].To
}

// CurrentVersion is the version the frozen chain produces.
func CurrentVersion() int { return currentVersion(migrations) }
