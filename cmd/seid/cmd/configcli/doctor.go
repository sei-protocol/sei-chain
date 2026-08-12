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
	// Checked is how many written keys were examined, so a clean result is distinguishable from
	// one that examined nothing.
	Checked int
}

// Healthy reports whether the file may be booted from.
func (d Diagnosis) Healthy() bool { return len(d.Unrecognized) == 0 }

// Doctor checks every written key against what this binary declares.
//
// The two namespaces are treated differently on purpose, and that asymmetry is the reason the
// experimental namespace is worth having. A written stable key the binary does not recognize is a
// broken promise: the operator wrote a setting believing it would take effect, and it will not, so
// this refuses. An experimental key is offered with no such promise, so it warns and the node
// boots.
//
// A key the file does not write is healthy by definition, because it resolves to the baseline. That
// is why this walks the written set rather than the declared one: checking the declared set would
// report every unwritten key on a file that is entirely correct.
func Doctor(file *seitoml.File) (Diagnosis, error) {
	written, err := file.Values()
	if err != nil {
		return Diagnosis{}, err
	}
	declared := declaredStableKeys()
	live, retired := experimentalNames()

	var d Diagnosis
	for key := range written {
		d.Checked++
		name, isExperimental := experimentalName(key)
		switch {
		case !isExperimental:
			if !declared[key] {
				d.Unrecognized = append(d.Unrecognized, key)
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
	return d, nil
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

// declaredStableKeys is the set of keys a section declares.
func declaredStableKeys() map[string]bool {
	keys := registry.Keys()
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
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
	if len(d.Unrecognized) > 0 {
		b.WriteString(fmt.Sprintf("%d written key(s) this binary does not recognize. Each was "+
			"written expecting it to take effect, and none of them does:\n", len(d.Unrecognized)))
		for _, k := range d.Unrecognized {
			b.WriteString("  " + k + "\n")
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
		return fmt.Sprintf("%d written key(s) checked, all recognized.\n", d.Checked)
	}
	return b.String()
}
