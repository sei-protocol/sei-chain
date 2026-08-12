package experimental

import (
	"sort"
	"strings"

	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
)

// The bounds Sweep applies. Each does a job none of the others can, which is why there are five
// numbers rather than one.
//
// MaxKeyBytes bounds what could ever match a declaration, and with it the resolution cost: depth
// cost is roughly quadratic, so raising this raises sweep cost superlinearly and must be
// re-measured, never extrapolated.
//
// MaxKeySegments bounds the environment pass, which is O(candidates x depth) because
// EnvShadowVars walks every proper prefix. The deepest dotted key in the tree is three
// (state-commit.flatkv.enable-read-write-metrics), so 16 has large headroom.
//
// MaxLoggedNameBytes bounds one name inside a record; MaxRecordBytes bounds the record. A name is
// resolvable up to MaxKeyBytes and readable up to MaxLoggedNameBytes, so a name between them is
// resolved and classified normally and rendered truncated. MaxRecordBytes is separate because the
// others cannot jointly guarantee it: quoting expands up to four times.
const (
	MaxKeyBytes        = 256
	MaxKeySegments     = 16
	MaxSweptCandidates = 512
	MaxLoggedNameBytes = 120
	MaxRecordBytes     = 8 << 10
)

// Source is the resolved configuration the sweep reads. A *viper.Viper satisfies it.
//
// AllKeys supplies candidate names only. Every classification comes from resolution, because a
// key can be enumerated and still resolve to nothing: a variable naming any proper prefix of a
// key's path collapses it while AllKeys keeps listing it.
type Source interface {
	servertypes.AppOptions // Get(string) any
	AllKeys() []string
}

// SweepInput is everything Sweep reads; nothing is ambient.
//
// Every field is load-bearing and a zero value silently loses a finding class: no Checkers and
// every declared key is reported unrecognized; no Tombstones and a promoted key is reported
// unrecognized, byte-identical to a typo; no Defects and no defect is ever reported at boot; no
// EnvPrefix or Environ and the environment pass does not run. Production calls SweepRegistry
// instead of building this by hand, and Findings.EnvPassRan reports the one omission a caller
// might still make.
type SweepInput struct {
	Source     Source
	Checkers   []Checker
	Tombstones []Tombstone
	Defects    []*DeclarationError
	EnvPrefix  string
	Environ    []string
}

// Findings is one sweep's result. Every field is advisory and every slice is sorted.
type Findings struct {
	Unrecognized  []Unrecognized  // resolved, in the namespace, claimed by no declaration
	Promoted      []PromotedKey   // resolved, matched by a tombstone
	Malformed     []*ValueError   // declared, resolved, value not usable
	Shadowed      []ShadowFinding // enumerated but resolving to nothing
	EnvDelivered  []string        // variables delivering a declared key's full path
	EnvShadow     []string        // variables collapsing a path prefix
	Defects       []*DeclarationError
	OversizeNames int  // candidates skipped for exceeding MaxKeyBytes or MaxKeySegments
	Truncated     int  // candidates skipped for exceeding MaxSweptCandidates
	EnvPassRan    bool // whether the environment pass ran at all
}

// Empty reports whether there is nothing to tell a node operator.
//
// It accounts for OversizeNames and Truncated as well as the slices: a section whose only anomaly
// is one over-long key would otherwise produce total silence, which is the class the bounds
// themselves would have created.
func (f Findings) Empty() bool {
	return len(f.Unrecognized) == 0 && len(f.Promoted) == 0 && len(f.Malformed) == 0 &&
		len(f.Shadowed) == 0 && len(f.EnvShadow) == 0 && len(f.Defects) == 0 &&
		f.OversizeNames == 0 && f.Truncated == 0
}

// Unrecognized names a key nothing claims, with the closest declared name when one is close
// enough to be worth showing.
type Unrecognized struct{ Key, Nearest string }

// PromotedKey names a key that has left the experimental surface: promoted when PromotedTo is
// set, removed outright when it is empty. The record distinguishes the two, because telling an
// operator a deleted knob was promoted is its own wrong diagnostic.
type PromotedKey struct{ Key, PromotedTo, RetiredIn string }

// ShadowFinding names a key that resolves to nothing and, when one was found, the variable
// responsible. An empty Cause means no environment cause was found, which is the case that
// deserves the most attention: nothing in this design explains it.
type ShadowFinding struct{ Key, Cause string }

// SweepRegistry sweeps src against this binary's registry.
//
// It fills Checkers, Tombstones and Defects from the package accessors and delegates to Sweep. It
// exists because a hand-built SweepInput that omits a field compiles and sweeps clean. The
// remaining arguments stay explicit so nothing is read from the process ambiently.
func SweepRegistry(src Source, envPrefix string, environ []string) Findings {
	return Sweep(SweepInput{
		Source:     src,
		Checkers:   Checkers(),
		Tombstones: Tombstones(),
		Defects:    Defects(),
		EnvPrefix:  envPrefix,
		Environ:    environ,
	})
}

// Sweep reports what the resolved configuration holds under the namespace that this binary does
// not recognize, which declared keys hold a value it will not use, and which keys resolve to
// nothing at all.
//
// It reads: it modifies no configuration, logs nothing, returns no error, and sorts every slice so
// repeated sweeps of one input agree. Candidates are bounded before the first resolution.
func Sweep(in SweepInput) Findings {
	var f Findings
	f.Defects = in.Defects

	if in.Source == nil {
		return f
	}

	declared := indexCheckers(in.Checkers)
	retired := indexTombstones(in.Tombstones)
	candidates, oversize, truncated := candidatesOf(in.Source)
	f.OversizeNames, f.Truncated = oversize, truncated

	for _, path := range candidates {
		// Classification comes from resolution, not from enumeration. A key AllKeys lists can
		// still resolve to nothing, and reporting that as unrecognized would send an operator
		// hunting a missing declaration when a variable ate their value.
		raw := in.Source.Get(path)
		if raw == nil {
			f.Shadowed = append(f.Shadowed, ShadowFinding{
				Key:   path,
				Cause: shadowCause(in, path),
			})
			continue
		}

		if c, ok := declared[path]; ok {
			if ve, usable := c.Reject(raw); !usable {
				f.Malformed = append(f.Malformed, ve)
			}
			continue
		}
		if t, ok := retired[path]; ok {
			f.Promoted = append(f.Promoted, PromotedKey{
				Key: path, PromotedTo: t.PromotedTo, RetiredIn: t.RetiredIn,
			})
			continue
		}
		f.Unrecognized = append(f.Unrecognized, Unrecognized{
			Key:     path,
			Nearest: nearest(path, declared),
		})
	}

	f.EnvDelivered, f.EnvShadow, f.EnvPassRan = envPass(in, declared)
	f.sort()
	return f
}

// sort orders every slice so one input yields one result across runs.
func (f *Findings) sort() {
	sort.Slice(f.Unrecognized, func(i, j int) bool { return f.Unrecognized[i].Key < f.Unrecognized[j].Key })
	sort.Slice(f.Promoted, func(i, j int) bool { return f.Promoted[i].Key < f.Promoted[j].Key })
	sort.Slice(f.Malformed, func(i, j int) bool { return f.Malformed[i].Key < f.Malformed[j].Key })
	sort.Slice(f.Shadowed, func(i, j int) bool { return f.Shadowed[i].Key < f.Shadowed[j].Key })
	sort.Strings(f.EnvDelivered)
	sort.Strings(f.EnvShadow)
}

// candidatesOf returns the namespace's keys, bounded before any resolution.
//
// Bounded first because resolution is the expensive step and its cost grows with depth, so an
// unbounded candidate set is where a pathological file would land.
func candidatesOf(src Source) (keys []string, oversize, truncated int) {
	prefix := Namespace + "."
	for _, path := range src.AllKeys() {
		lowered := strings.ToLower(path)
		if !strings.HasPrefix(lowered, prefix) && lowered != Namespace {
			continue
		}
		if len(lowered) > MaxKeyBytes || len(strings.Split(lowered, ".")) > MaxKeySegments {
			oversize++
			continue
		}
		if len(keys) >= MaxSweptCandidates {
			truncated++
			continue
		}
		keys = append(keys, lowered)
	}
	sort.Strings(keys)
	return keys, oversize, truncated
}

// indexCheckers keys the declared set by full path.
func indexCheckers(cs []Checker) map[string]Checker {
	out := make(map[string]Checker, len(cs))
	for _, c := range cs {
		out[c.Path()] = c
	}
	return out
}

// indexTombstones keys the retired set by full path.
func indexTombstones(ts []Tombstone) map[string]Tombstone {
	out := make(map[string]Tombstone, len(ts))
	for _, t := range ts {
		out[Namespace+"."+t.Name] = t
	}
	return out
}

// shadowCause names the variable collapsing a path, or empty when none was found.
func shadowCause(in SweepInput, path string) string {
	if in.EnvPrefix == "" || len(in.Environ) == 0 {
		return ""
	}
	set := setVars(in.Environ)
	for _, v := range EnvShadowVars(in.EnvPrefix, path) {
		if set[v] {
			return v
		}
	}
	return ""
}

// envPass reports which variables deliver a declared key and which collapse one.
//
// It runs only with both a prefix and an environment, and reports whether it ran, because a caller
// that omitted either would otherwise get a clean sweep that had simply not looked.
func envPass(in SweepInput, declared map[string]Checker) (delivered, shadow []string, ran bool) {
	if in.EnvPrefix == "" || len(in.Environ) == 0 {
		return nil, nil, false
	}
	set := setVars(in.Environ)

	seen := map[string]bool{}
	for path := range declared {
		if v := in.EnvPrefix + "_" + envify(path); set[v] {
			delivered = append(delivered, v)
		}
		for _, v := range EnvShadowVars(in.EnvPrefix, path) {
			if set[v] && !seen[v] {
				seen[v] = true
				shadow = append(shadow, v)
			}
		}
	}
	return delivered, shadow, true
}

// setVars returns the names of every variable that is set and non-empty.
//
// Empty is excluded deliberately: an empty variable does not collapse a key, and reporting one as
// a cause would send an operator to a variable that is not the problem.
func setVars(environ []string) map[string]bool {
	out := make(map[string]bool, len(environ))
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if ok && value != "" {
			out[name] = true
		}
	}
	return out
}

// EnvShadowVars returns the variables that would collapse keyPath, one per proper prefix.
//
// Derived the way the server viper derives them: the replacer runs over the whole prefixed name,
// so a caller building a name by hand gets it wrong and sees a false negative. This is the single
// production derivation.
func EnvShadowVars(prefix, keyPath string) []string {
	segs := strings.Split(keyPath, ".")
	out := make([]string, 0, len(segs)-1)
	for i := 1; i < len(segs); i++ {
		out = append(out, prefix+"_"+envify(strings.Join(segs[:i], ".")))
	}
	return out
}

// envify renders a dotted path the way the boot's replacer does: dots and hyphens both become
// underscores, then the whole name is upper-cased.
func envify(path string) string {
	return strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(path))
}

// nearest returns the declared path closest to key, or empty when none is close enough.
//
// Close enough means within a small edit distance, so a typo gets a suggestion and an unrelated
// key does not get a misleading one.
func nearest(key string, declared map[string]Checker) string {
	const maxDistance = 3
	best, bestD := "", maxDistance+1
	for path := range declared {
		if d := editDistance(key, path); d < bestD {
			best, bestD = path, d
		}
	}
	if bestD > maxDistance {
		return ""
	}
	return best
}

// editDistance returns the Levenshtein distance between two strings.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
