package configcli_test

import (
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configcli"
	"github.com/sei-protocol/sei-chain/config/registry"
)

// existing is a node's current configuration, controlled key by key.
//
// A fake rather than a real viper, so an assertion here is about what adoption does with a value
// rather than about what viper would have returned for it.
type existing map[string]any

func (e existing) Get(key string) any { return e[key] }

func (e existing) AllKeys() []string {
	out := make([]string, 0, len(e))
	for k := range e {
		out = append(out, k)
	}
	return out
}

// noEnv is an environment with nothing set.
func noEnv(string) (string, bool) { return "", false }

// TestAdoptionKeepsWhatTheNodeWasRunning is the property that separates adopting from generating.
//
// A node that has been running has values somebody chose. Building the file from this binary's
// baselines instead would move it onto defaults silently, which is the one outcome adoption exists to
// prevent.
func TestAdoptionKeepsWhatTheNodeWasRunning(t *testing.T) {
	registerTyped(t)
	// The baseline for validator mode is workers=4, enabled=true, timeout=30s.
	current := existing{
		"probe.workers": 64,
		"probe.enabled": false,
		"probe.timeout": "5m",
	}

	got, err := configcli.Adopt(current, noEnv, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	written, err := got.File.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	for key, want := range map[string]any{
		"probe.workers": int64(64),
		"probe.enabled": false,
		"probe.timeout": "5m0s",
	} {
		if written[key] != want {
			t.Errorf("%s adopted as %#v, want the %#v this node was running. Taking the baseline "+
				"instead moves a running node onto a default nobody chose", key, written[key], want)
		}
	}
	if len(got.Carried) != 3 {
		t.Errorf("reported %v as carried over, want the three the existing configuration held",
			got.Carried)
	}
	assertEveryKeyAccountedFor(t, got)
}

// TestAdoptionStillWritesTheKeysTheOldConfigurationLacked keeps the file complete.
//
// A key the old configuration never set has no value to carry, and leaving it out would produce a
// file that is not the complete picture the other verbs assume. It takes the baseline and is reported
// separately, so an operator can see which values were not theirs.
func TestAdoptionStillWritesTheKeysTheOldConfigurationLacked(t *testing.T) {
	registerTyped(t)

	got, err := configcli.Adopt(existing{"probe.workers": 64}, noEnv, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	written, err := got.File.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if len(written) != len(registry.Keys()) {
		t.Errorf("adopted %d keys and the registry declares %d: %v", len(written),
			len(registry.Keys()), written)
	}
	if written["probe.endpoint"] != "localhost:8545" {
		t.Errorf("a key the old configuration lacked adopted as %#v, want the baseline",
			written["probe.endpoint"])
	}
	if len(got.Carried) != 1 || got.Carried[0] != "probe.workers" {
		t.Errorf("carried %v, want only the one key the old configuration held", got.Carried)
	}
	if len(got.Baselined) != len(registry.Keys())-1 {
		t.Errorf("reported %d baselined keys, want %d", len(got.Baselined), len(registry.Keys())-1)
	}
	assertEveryKeyAccountedFor(t, got)
	// Doctor accepts it, or adoption would hand an operator a file the same tool refuses.
	if d, err := configcli.Doctor(got.File); err != nil || !d.Healthy() {
		t.Errorf("doctor refused an adopted file: %v %s", err, d.Report())
	}
}

// TestAnEnvironmentSuppliedValueIsReportedAndNotWritten holds the one channel adoption must not fold
// in.
//
// An environment variable sits above the file, so writing its value in changes nothing while it is
// still set, and silently changes how the node runs the day somebody unsets it. Reporting it is the
// only honest option.
func TestAnEnvironmentSuppliedValueIsReportedAndNotWritten(t *testing.T) {
	registerTyped(t)
	delivered := registry.EnvName("probe.workers")
	lookup := func(name string) (string, bool) {
		if name == delivered {
			return "99", true
		}
		return "", false
	}

	got, err := configcli.Adopt(existing{"probe.endpoint": "sei:8545"}, lookup, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	if len(got.Environment) != 1 || got.Environment[0] != "probe.workers" {
		t.Errorf("reported %v as environment-supplied, want probe.workers", got.Environment)
	}
	// The key is reported as environment-supplied and as taking the baseline, never as carried: no
	// value for it came out of the existing files. Environment may legitimately overlap with carried
	// when a variable and a file both hold a key, which is why the invariant below is the one that
	// has to hold rather than a rule about Environment alone.
	if holdsKey(got.Carried, "probe.workers") {
		t.Errorf("probe.workers is reported as carried over, but its value came from a variable and "+
			"was not written. The counts an operator reads would then include a value that is not in "+
			"the file. Carried=%v Baselined=%v", got.Carried, got.Baselined)
	}
	assertEveryKeyAccountedFor(t, got)
	written, err := got.File.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if written["probe.workers"] != 4 && written["probe.workers"] != int64(4) {
		t.Errorf("probe.workers was written as %#v. The environment's 99 must not be folded in: it "+
			"overrides the file, so writing it changes nothing now and changes the node's behaviour "+
			"the day it is unset", written["probe.workers"])
	}
	// And the report names the variable, since that is what an operator has to go and look at.
	if !strings.Contains(got.Report(), delivered) {
		t.Errorf("the report does not name %s, so an operator cannot find what is overriding their "+
			"file:\n%s", delivered, got.Report())
	}
	// The file says so too, because the report is gone once the terminal scrolls.
	raw, err := got.File.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !strings.Contains(string(raw), "environment") {
		t.Errorf("the adopted file does not mention that a setting is supplied by the environment:\n%s",
			raw)
	}
}

// TestAValueThatCannotBeReadKeepsTheBaselineAndIsReported holds the middle path between two bad ones.
//
// Writing it anyway produces a file the node refuses at its next boot. Dropping it quietly moves the
// node onto a default nobody chose. Keeping the baseline and saying so is the only option that leaves
// the operator able to act.
func TestAValueThatCannotBeReadKeepsTheBaselineAndIsReported(t *testing.T) {
	registerTyped(t)
	current := existing{
		"probe.workers": "lots",      // not a number
		"probe.timeout": "30",        // a duration with no unit
		"probe.enabled": "yes",       // not a bool
		"probe.ratio":   "very",      // not a number
		"probe.peers":   []any{1, 2}, // a list that is not text
	}

	got, err := configcli.Adopt(current, noEnv, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	if len(got.Unconvertible) != 5 {
		t.Errorf("reported %d unreadable values, want 5: %+v", len(got.Unconvertible), got.Unconvertible)
	}
	written, err := got.File.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if written["probe.workers"] != int64(4) {
		t.Errorf("probe.workers adopted as %#v from an unreadable value, want the baseline 4",
			written["probe.workers"])
	}
	if written["probe.timeout"] != "30s" {
		t.Errorf("probe.timeout adopted as %#v, want the baseline 30s. A unit-less 30 read as "+
			"nanoseconds is the coercion this refuses", written["probe.timeout"])
	}
	for _, r := range got.Unconvertible {
		if r.Reason == "" || r.Value == nil {
			t.Errorf("a rejection carries no reason or no value: %+v. An operator cannot correct a "+
				"value the report does not quote back", r)
		}
	}
	if !strings.Contains(got.Report(), "could not be read") {
		t.Errorf("the report does not flag the unreadable values:\n%s", got.Report())
	}
}

// TestAdoptionReadsAValueWhoseTypeTheOldFileGotRight is the other direction of coercion.
//
// The refusals above would hold for an adoption that refused everything, so this drives the values a
// configuration file legitimately produces: numbers as text, whole numbers as floats, lists as text.
func TestAdoptionReadsAValueWhoseTypeTheOldFileGotRight(t *testing.T) {
	registerTyped(t)
	current := existing{
		"probe.workers":  "64",            // a file or flag binding yields text
		"probe.ratio":    float64(0.25),   //
		"probe.endpoint": "sei:8545",      //
		"probe.timeout":  "1m30s",         //
		"probe.peers":    []any{"a", "b"}, // viper yields []any for a TOML array
		"probe.enabled":  false,           //
	}

	got, err := configcli.Adopt(current, noEnv, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	if len(got.Unconvertible) != 0 {
		t.Fatalf("values a configuration file legitimately produces were refused: %+v",
			got.Unconvertible)
	}
	written, err := got.File.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	for key, want := range map[string]any{
		"probe.workers":  int64(64),
		"probe.ratio":    0.25,
		"probe.endpoint": "sei:8545",
		"probe.timeout":  "1m30s",
		"probe.enabled":  false,
	} {
		if written[key] != want {
			t.Errorf("%s adopted as %#v, want %#v", key, written[key], want)
		}
	}
	peers, ok := written["probe.peers"].([]any)
	if !ok || len(peers) != 2 || peers[0] != "a" {
		t.Errorf("probe.peers adopted as %#v, want a list of a and b", written["probe.peers"])
	}
}

// TestAdoptionFollowsTheModeForTheKeysItCannotCarry holds that the mode still reaches the baselines.
//
// Adoption uses baselines only where the old configuration is silent, so a mode that never reached
// them would put one mode's defaults on another mode's node for exactly those keys.
func TestAdoptionFollowsTheModeForTheKeysItCannotCarry(t *testing.T) {
	registerTyped(t)
	current := existing{"probe.workers": 64} // enabled is left to the baseline, and it varies by mode

	validator, err := configcli.Adopt(current, noEnv, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Adopt(validator): %v", err)
	}
	archive, err := configcli.Adopt(current, noEnv, registry.ModeArchive)
	if err != nil {
		t.Fatalf("Adopt(archive): %v", err)
	}

	v, _ := validator.File.Values()
	a, _ := archive.File.Values()
	if v["probe.enabled"] == a["probe.enabled"] {
		t.Errorf("both modes baselined probe.enabled to %#v for a key whose baseline varies by mode, "+
			"so the mode never reached the baselines", v["probe.enabled"])
	}
	if _, err := configcli.Adopt(current, noEnv, registry.Mode("archival")); err == nil {
		t.Error("Adopt accepted a mode no node runs")
	}
}

// TestAdoptionRefusesWithNothingToAdoptFrom keeps an empty result from reading as a successful one.
func TestAdoptionRefusesWithNothingToAdoptFrom(t *testing.T) {
	registerTyped(t)

	if _, err := configcli.Adopt(nil, noEnv, registry.ModeValidator); err == nil {
		t.Error("Adopt accepted a nil source, which would produce a file of pure baselines while " +
			"reporting that a node's configuration had been carried over")
	}

	registry.Reset()
	if _, err := configcli.Adopt(existing{}, noEnv, registry.ModeValidator); err == nil {
		t.Error("Adopt produced a file from an empty registry")
	}
}

// TestAdoptionIsByteStable keeps two adopted files comparable.
func TestAdoptionIsByteStable(t *testing.T) {
	registerTyped(t)
	current := existing{"probe.workers": 64, "probe.endpoint": "sei:8545"}

	first, err := configcli.Adopt(current, noEnv, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	second, err := configcli.Adopt(current, noEnv, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	a, err := first.File.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	b, err := second.File.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("two adoptions of one configuration produced different files:\n%s\n---\n%s", a, b)
	}
	// The value is Duration, not time.Duration's zero, so the fixture is exercising what it claims.
	if !strings.Contains(string(a), "workers = 64") {
		t.Errorf("the adopted file does not carry the value it was given:\n%s", a)
	}
}

// TestAdoptedAndGeneratedFilesDifferWhereTheNodeDoes is the pair's own property.
//
// If adopting and generating produced the same file, adoption would be doing nothing, and the two
// tests above could both pass against an adoption that ignored its source entirely.
func TestAdoptedAndGeneratedFilesDifferWhereTheNodeDoes(t *testing.T) {
	registerTyped(t)
	current := existing{"probe.workers": 64}

	adopted, err := configcli.Adopt(current, noEnv, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	generated, err := configcli.Generate(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	a, _ := adopted.File.Values()
	g, _ := generated.Values()
	if a["probe.workers"] == g["probe.workers"] {
		t.Errorf("adopting and generating both produced workers=%#v, so adoption ignored the value "+
			"the node was running", a["probe.workers"])
	}
	// Everything the old configuration did not set matches what generate would have written.
	for key, want := range g {
		if key == "probe.workers" {
			continue
		}
		if !sameRead(a[key], want) {
			t.Errorf("%s adopted as %#v and generates as %#v; a key the old configuration did not "+
				"set should match the baseline", key, a[key], want)
		}
	}
}

// sameRead compares two values read back off a file, including lists.
//
// Two lists are never equal under ==, and comparing them with it panics rather than returning false,
// so a test that reached a list key would fail for the wrong reason.
func sameRead(a, b any) bool {
	as, aok := a.([]any)
	bs, bok := b.([]any)
	if aok || bok {
		if !aok || !bok || len(as) != len(bs) {
			return false
		}
		for i := range as {
			if !sameRead(as[i], bs[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// TestAdoptionCarriesADurationTheOldFileWroteAsText covers the type the legacy path mishandles.
//
// A duration reaches a configuration file as text, and reading a bare number as nanoseconds is the
// coercion that turns an intended thirty seconds into thirty billionths of one. Adoption is the last
// point at which that value can still be corrected cheaply.
func TestAdoptionCarriesADurationTheOldFileWroteAsText(t *testing.T) {
	registerTyped(t)

	got, err := configcli.Adopt(existing{"probe.timeout": "2h45m"}, noEnv, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	written, _ := got.File.Values()
	if written["probe.timeout"] != "2h45m0s" {
		t.Errorf("the duration adopted as %#v, want 2h45m0s", written["probe.timeout"])
	}
	// And the same value as a bare number is refused rather than read as nanoseconds.
	refused, err := configcli.Adopt(existing{"probe.timeout": 9900}, noEnv, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(refused.Unconvertible) != 1 {
		t.Errorf("a bare number was accepted as a duration: %+v. Read as nanoseconds it is under a "+
			"hundredth of a millisecond, not the two and three quarter hours it looks like",
			refused.Unconvertible)
	}
	if w, _ := refused.File.Values(); w["probe.timeout"] != (30 * time.Second).String() {
		t.Errorf("the refused duration adopted as %#v, want the baseline", w["probe.timeout"])
	}
}

// assertEveryKeyAccountedFor holds the invariant the reported counts depend on.
//
// Every declared key is written exactly once, so it either carried a value over or took the
// baseline, never both and never neither. A key in both lists inflates what an operator is told was
// preserved; a key in neither hides a value that changed.
func assertEveryKeyAccountedFor(t *testing.T, got configcli.Adoption) {
	t.Helper()

	seen := map[string]int{}
	for _, k := range got.Carried {
		seen[k]++
	}
	for _, k := range got.Baselined {
		seen[k]++
	}
	for _, key := range registry.Keys() {
		switch seen[key] {
		case 1:
		case 0:
			t.Errorf("%s is reported as neither carried nor baselined, so a value that changed is "+
				"invisible in the report", key)
		default:
			t.Errorf("%s is reported %d times across carried and baselined, which inflates what an "+
				"operator is told was preserved", key, seen[key])
		}
		delete(seen, key)
	}
	for key := range seen {
		t.Errorf("%s is reported but no section declares it", key)
	}
}

// holdsKey reports whether keys contains want.
func holdsKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}
