package configcli

import (
	"fmt"
	"sort"
	"strings"

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
	return len(d.Unrecognized) == 0 && len(d.Malformed) == 0 && d.ModeProblem == ""
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
// A key the file does not write is healthy by definition, because it resolves to the baseline. That
// is why this walks the written set rather than the declared one: checking the declared set would
// report every unwritten key on a file that is entirely correct.
func Doctor(file *seitoml.File) (Diagnosis, error) {
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
	return d, nil
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
