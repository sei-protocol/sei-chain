package configcli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sei-protocol/sei-chain/config/appopts"
	"github.com/sei-protocol/sei-chain/config/experimental"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
)

// Diagnosis is what doctor found in a written file.
type Diagnosis struct {
	// Unrecognized are written keys under no declared section, sorted. Each halts.
	Unrecognized []string
	// UnrecognizedExperimental are written keys under the experimental namespace that no
	// declaration matches, sorted. Each warns.
	UnrecognizedExperimental []string
	// Retired are experimental keys a tombstone covers, sorted. Each warns.
	Retired []string
	// Malformed are written keys whose value this binary cannot read as the key's declared type,
	// sorted by key. Each halts.
	Malformed []Malformation
	// Checked is how many written keys were examined, so a clean result is distinguishable from
	// one that examined nothing.
	Checked int
	// Mode is the node mode the file records, empty when it records none this binary knows.
	Mode string
	// ModeProblem says why the recorded mode is unusable, and is empty when it is fine.
	ModeProblem string
	// ModeConflict says how the recorded mode disagrees with the one Tendermint runs, and is empty
	// when they agree or when there was nothing to compare against.
	ModeConflict string
	// Refused are the sections that judged their own resolved values unusable, sorted. Each halts.
	Refused []registry.SectionError
	// Overridden are written keys whose value the environment supplies instead, sorted by key. Each
	// warns: the file is not what the node runs for that key, and nothing else says so.
	Overridden []Override
}

// Override is one written key the environment answers instead of the file.
type Override struct {
	// Key is the dotted key.
	Key string
	// Written is what the file holds.
	Written any
	// Applied is what resolves instead.
	Applied any
	// Variable is the environment variable supplying it.
	Variable string
}

// Malformation is one written value this binary cannot read.
type Malformation struct {
	// Key is the dotted key.
	Key string
	// Value is what the file holds.
	Value any
	// Want is the type the key's section declares.
	Want string
	// Reason says why the value cannot be read as that type.
	Reason string
}

// Healthy reports whether the file may be booted from.
func (d Diagnosis) Healthy() bool {
	return len(d.Unrecognized) == 0 && len(d.Malformed) == 0 && len(d.Refused) == 0 &&
		d.ModeProblem == "" && d.ModeConflict == ""
}

// Doctor checks every written key against what this binary declares.
//
// This treats the two namespaces differently, and that asymmetry is why the experimental namespace
// is worth having. A written stable key the binary does not recognize is a
// broken promise: the operator wrote a setting believing it would take effect, and it will not, so
// this refuses. An experimental key is offered with no such promise, so it warns and the node
// boots.
//
// A written value this binary cannot read as its key's declared type halts for the same reason. The
// key is recognized, so nothing reports it, and the node refuses the value at its next boot instead.
// set converts a value on the way in, but hand-editing the file is equally legitimate and reaches no
// such check, so this is where a hand-edited value is caught. The reading is shared with adoption,
// which faces the same question about a value it did not write.
//
// Each section is then asked whether it can use the values that resolve for it. The tags say what shape
// a value has and a section says which values are allowed, so an enum's members and a number's range are
// checked here and nowhere else could know them.
//
// The mode the file records is compared against the one Tendermint runs, where a caller knows it. Not
// for equality: an archive node is correctly set up with config.toml saying full, so the rule is that
// Tendermint runs whatever this node's mode implies.
//
// A key the file does not write is healthy by definition, because it resolves to the baseline. That
// is why this walks the written set rather than the declared one: checking the declared set would
// report every unwritten key on a file that is entirely correct.
func Doctor(file *seitoml.File, tendermintMode string) (Diagnosis, error) {
	written, err := file.Values()
	if err != nil {
		return Diagnosis{}, err
	}
	declared, err := declaredTypes()
	if err != nil {
		return Diagnosis{}, err
	}
	live, retired := experimentalNames()

	var d Diagnosis
	d.Mode, d.ModeProblem = diagnoseMode(file)
	// Compared only when both sides are known. An unreadable config.toml is not evidence about
	// sei.toml, and a mode this binary cannot use has already been reported on its own terms.
	if d.ModeProblem == "" && tendermintMode != "" {
		if err := appopts.ReconcileMode(d.Mode, tendermintMode); err != nil {
			d.ModeConflict = err.Error()
		}
	}

	for key, value := range written {
		d.Checked++
		name, isExperimental := experimentalName(key)
		switch {
		case !isExperimental:
			want, isDeclared := declared[key]
			if !isDeclared {
				d.Unrecognized = append(d.Unrecognized, key)
				continue
			}
			if _, err := coerce(value, want); err != nil {
				d.Malformed = append(d.Malformed, Malformation{
					Key: key, Value: value, Want: want.String(), Reason: err.Error(),
				})
			}
		case retired[name]:
			d.Retired = append(d.Retired, key)
		case !live[name]:
			d.UnrecognizedExperimental = append(d.UnrecognizedExperimental, key)
		}
	}
	sort.Strings(d.Unrecognized)
	sort.Strings(d.UnrecognizedExperimental)
	sort.Strings(d.Retired)
	sort.Slice(d.Malformed, func(i, j int) bool { return d.Malformed[i].Key < d.Malformed[j].Key })

	d.Refused, d.Overridden = askEachSection(d, written)
	return d, nil
}

// askEachSection resolves what the node would run and asks every section whether it can use it.
//
// Resolved rather than written, because a section's rule can span two keys and one handed only the
// written ones would see a zero for every key the operator left alone. What each section judges is
// therefore the configuration the node would actually run, which makes a clean report mean the boot
// will be content with this file.
//
// The environment is resolved along with the file, because it beats the file: judging the file alone
// would judge a configuration no node runs, and report a value as usable while the node used another.
// Command-line flags are not resolved here and cannot be, since they belong to the start command and
// this verb is a different one. So a clean report means the boot is content with this file and this
// environment, and a flag typed at start time still wins over both.
//
// Skipped when the mode is unusable or a value is unreadable. Neither can produce a resolution worth
// judging, and both are already reported on their own terms, so asking anyway would turn one fault
// into two findings.
func askEachSection(d Diagnosis, written map[string]any) ([]registry.SectionError, []Override) {
	if d.ModeProblem != "" || len(d.Malformed) > 0 {
		return nil, nil
	}
	resolved, err := registry.Resolve(registry.Mode(d.Mode),
		registry.FileLayer(written), registry.EnvLayer(os.LookupEnv))
	if err != nil {
		return []registry.SectionError{{Section: "", Err: err}}, nil
	}
	return registry.ValidateResolved(resolved), environmentOverrides(resolved, written)
}

// environmentOverrides lists the written keys the environment answers instead of the file.
//
// Resolving without the environment would judge a configuration no node runs, since the environment
// beats the file, so this verb has to include it. Having included it, an operator needs telling: a file
// they wrote and a value the node uses are two different things for these keys, and nothing else reports
// that. The legacy path cannot report it at all, because its layers merge inside one source before
// anything observes them.
func environmentOverrides(resolved registry.Resolved, written map[string]any) []Override {
	var out []Override
	for key, value := range written {
		resolution, declared := resolved.From(key)
		if !declared || resolution.From != "env" {
			continue
		}
		out = append(out, Override{
			Key:      key,
			Written:  value,
			Applied:  resolution.Value,
			Variable: registry.EnvName(key),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// diagnoseMode reads the recorded node mode and says what is wrong with it, if anything.
//
// A mode this binary does not know makes every other answer meaningless: nothing can resolve the
// baselines the file's values were chosen against, so a comparison cannot run and the node cannot be
// told whether its settings still mean what they meant.
func diagnoseMode(file *seitoml.File) (mode, problem string) {
	recorded, err := file.Mode()
	if err != nil {
		return "", err.Error()
	}
	if err := knownMode(registry.Mode(recorded)); err != nil {
		return recorded, err.Error()
	}
	return recorded, ""
}

// experimentalName reports whether a written key sits in the experimental namespace, and returns
// the declared name with the namespace prefix removed.
func experimentalName(key string) (string, bool) {
	prefix := experimental.Namespace + "."
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	return strings.TrimPrefix(key, prefix), true
}

// experimentalNames returns the live declarations and the tombstoned ones.
func experimentalNames() (live, retired map[string]bool) {
	live, retired = map[string]bool{}, map[string]bool{}
	for _, decl := range experimental.Declarations() {
		live[decl.Name] = true
	}
	for _, tomb := range experimental.Tombstones() {
		retired[tomb.Name] = true
	}
	return live, retired
}

// Report renders a diagnosis for an operator, most severe first.
func (d Diagnosis) Report() string {
	var b strings.Builder
	if d.ModeProblem != "" {
		b.WriteString("the node mode this file records is unusable: " + d.ModeProblem + "\n" +
			"Nothing can resolve the defaults its values were chosen against until that is fixed.\n")
	}
	if d.ModeConflict != "" {
		b.WriteString("this node's two configuration files disagree about what kind of node it is: " +
			d.ModeConflict + "\n")
	}
	if len(d.Refused) > 0 {
		b.WriteString(fmt.Sprintf("%d section(s) refused the values this file resolves to. The node "+
			"will refuse them too:\n", len(d.Refused)))
		for _, refusal := range d.Refused {
			b.WriteString("  " + refusal.Error() + "\n")
		}
	}
	if len(d.Unrecognized) > 0 {
		b.WriteString(fmt.Sprintf("%d written key(s) this binary does not recognize. Each was "+
			"written expecting it to take effect, and none of them does:\n", len(d.Unrecognized)))
		for _, k := range d.Unrecognized {
			b.WriteString("  " + k + "\n")
		}
	}
	if len(d.Malformed) > 0 {
		b.WriteString(fmt.Sprintf("%d written value(s) this binary cannot read. The node will refuse "+
			"each of them at its next start:\n", len(d.Malformed)))
		for _, m := range d.Malformed {
			b.WriteString(fmt.Sprintf("  %s = %#v: %s (expected %s)\n", m.Key, m.Value, m.Reason, m.Want))
		}
	}
	if len(d.Overridden) > 0 {
		b.WriteString(fmt.Sprintf("%d written key(s) the environment answers instead of this file. The "+
			"node uses the environment's value, not the one written here:\n", len(d.Overridden)))
		for _, o := range d.Overridden {
			b.WriteString(fmt.Sprintf("  %s: this file says %#v, %s says %#v\n",
				o.Key, o.Written, o.Variable, o.Applied))
		}
	}
	for _, group := range []struct {
		keys []string
		head string
	}{
		{d.Retired, "experimental key(s) that have been retired. Each is ignored; check the release " +
			"notes for what replaced it"},
		{d.UnrecognizedExperimental, "experimental key(s) this binary does not declare. Each is " +
			"ignored, which is what the experimental namespace offers"},
	} {
		if len(group.keys) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("%d %s:\n", len(group.keys), group.head))
		for _, k := range group.keys {
			b.WriteString("  " + k + "\n")
		}
	}
	if b.Len() == 0 {
		return fmt.Sprintf("%d written key(s) checked against %q mode, all recognized.\n",
			d.Checked, d.Mode)
	}
	return b.String()
}
