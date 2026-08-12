package configcli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
)

// Difference is one key whose written value is not the baseline for a mode.
type Difference struct {
	// Key is the dotted key.
	Key string
	// Written is what the file holds.
	Written any
	// Baseline is what this binary would use for the mode if the key were absent.
	Baseline any
}

// Comparison is what the file and the binary disagree about.
type Comparison struct {
	// Mode is the node mode whose baselines the comparison ran against.
	Mode registry.Mode
	// Differing are the keys whose written value is not the baseline, sorted.
	Differing []Difference
	// Tracking are declared keys the file does not write, so they follow the baseline, sorted.
	Tracking []string
	// Undeclared are written keys no section declares, sorted.
	Undeclared []string
}

// Diff compares a written file against this binary's baselines.
//
// The mode comes from the file, which records the one its values resolve for. A caller may pass one
// to compare against a different mode, and a mismatch is an error rather than a silent choice of
// either: comparing an archive node's file against a validator's baselines reports differences that
// are not differences at all.
//
// The comparison is what an operator reads to answer two questions a file alone cannot: which of
// these values did somebody choose, and which are simply the binary's own. On a fully generated
// file every key is written, so nothing differs and nothing tracks, and that is itself the useful
// answer: the node is pinned to the values it was generated with.
//
// A written value that equals its baseline is still a written value. This reports it as agreeing
// rather than tracking, because the two behave differently the moment a release changes that
// baseline.
func Diff(file *seitoml.File, mode registry.Mode) (Comparison, error) {
	mode, err := reconcileMode(file, mode)
	if err != nil {
		return Comparison{}, err
	}
	resolved, err := registry.Resolve(mode)
	if err != nil {
		return Comparison{}, fmt.Errorf("resolve the baselines for mode %q: %w", mode, err)
	}
	written, err := file.Values()
	if err != nil {
		return Comparison{}, err
	}

	out := Comparison{Mode: mode}
	for key, res := range resolved.Keys {
		value, present := written[key]
		switch {
		case !present:
			out.Tracking = append(out.Tracking, key)
		case !sameValue(value, res.Value):
			out.Differing = append(out.Differing, Difference{Key: key, Written: value, Baseline: res.Value})
		}
	}
	for key := range written {
		if _, declared := resolved.Keys[key]; !declared {
			out.Undeclared = append(out.Undeclared, key)
		}
	}

	sort.Slice(out.Differing, func(i, j int) bool { return out.Differing[i].Key < out.Differing[j].Key })
	sort.Strings(out.Tracking)
	sort.Strings(out.Undeclared)
	return out, nil
}

// sameValue compares a written value against a baseline across the types the two arrive as.
//
// A file yields int64 for every whole number while a struct field may be an int or a uint, and a
// duration is written as text while its baseline is a time.Duration. Compared with == alone, every
// such key would be reported as differing on a file that agrees with the binary exactly.
func sameValue(written, baseline any) bool {
	if written == baseline {
		return true
	}
	if equal, bothNumbers := sameNumber(written, baseline); bothNumbers {
		return equal
	}
	if w, ok := written.(string); ok {
		if b, ok := baseline.(interface{ String() string }); ok {
			return w == b.String()
		}
	}
	if w, ok := written.([]any); ok {
		b, ok := baseline.([]string)
		if !ok || len(w) != len(b) {
			return false
		}
		for i := range w {
			if s, ok := w[i].(string); !ok || s != b[i] {
				return false
			}
		}
		return true
	}
	return false
}

// sameNumber compares two whole numbers, and reports whether both were whole numbers at all.
//
// This keeps signed and unsigned apart rather than casting both to one type. A file yields int64 for
// every whole number while a struct field may be unsigned, and casting an unsigned value above the
// signed maximum wraps it negative, so two different values compare equal.
func sameNumber(a, b any) (equal, bothNumbers bool) {
	ai, aSigned := asInt64(a)
	au, aUnsigned := asUint64(a)
	bi, bSigned := asInt64(b)
	bu, bUnsigned := asUint64(b)

	switch {
	case aSigned && bSigned:
		return ai == bi, true
	case aUnsigned && bUnsigned:
		return au == bu, true
	case aSigned && bUnsigned:
		return ai >= 0 && uint64(ai) == bu, true
	case aUnsigned && bSigned:
		return bi >= 0 && uint64(bi) == au, true
	default:
		return false, false
	}
}

// asInt64 reports a signed whole number.
func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	default:
		return 0, false
	}
}

// asUint64 reports an unsigned whole number.
func asUint64(v any) (uint64, bool) {
	switch n := v.(type) {
	case uint:
		return uint64(n), true
	case uint8:
		return uint64(n), true
	case uint16:
		return uint64(n), true
	case uint32:
		return uint64(n), true
	case uint64:
		return n, true
	default:
		return 0, false
	}
}

// Report renders a comparison for an operator.
func (c Comparison) Report() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Compared against the defaults for %q mode.\n", c.Mode))

	if len(c.Differing) > 0 {
		b.WriteString(fmt.Sprintf("\n%d key(s) differ from the default:\n", len(c.Differing)))
		for _, d := range c.Differing {
			b.WriteString(fmt.Sprintf("  %s = %v (default %v)\n", d.Key, d.Written, d.Baseline))
		}
	}
	if len(c.Undeclared) > 0 {
		b.WriteString(fmt.Sprintf("\n%d written key(s) this binary does not recognize:\n",
			len(c.Undeclared)))
		for _, k := range c.Undeclared {
			b.WriteString("  " + k + "\n")
		}
	}
	if len(c.Tracking) > 0 {
		b.WriteString(fmt.Sprintf("\n%d key(s) are not written and follow this binary's defaults.\n",
			len(c.Tracking)))
	}
	if len(c.Differing) == 0 && len(c.Undeclared) == 0 && len(c.Tracking) == 0 {
		b.WriteString("\nEvery declared key is written and matches this binary's defaults.\n")
	}
	return b.String()
}

// reconcileMode settles which mode a comparison runs against.
//
// The file's own mode wins when the caller names none. When the caller names one that disagrees, this
// refuses rather than picking: either answer produces a comparison the operator did not ask for, and
// the disagreement itself is what they need to see.
func reconcileMode(file *seitoml.File, asked registry.Mode) (registry.Mode, error) {
	recorded, err := file.Mode()
	if err != nil {
		return "", err
	}
	fromFile := registry.Mode(recorded)
	if err := knownMode(fromFile); err != nil {
		return "", fmt.Errorf("sei.toml records %s = %q: %w", seitoml.ModeKey, recorded, err)
	}
	if asked == "" || asked == fromFile {
		return fromFile, nil
	}
	if err := knownMode(asked); err != nil {
		return "", err
	}
	return "", fmt.Errorf("sei.toml holds values resolved for %q mode and this asks for %q. Comparing "+
		"against another mode's defaults reports differences that are not differences. Regenerate for "+
		"%q, or drop the flag to compare against the mode the file records", fromFile, asked, asked)
}
