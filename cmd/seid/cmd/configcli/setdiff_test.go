package configcli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configcli"
	"github.com/sei-protocol/sei-chain/config/experimental"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
)

// typed is a section covering every type set has to read from a command line.
type typed struct {
	Enabled  bool          `mapstructure:"enabled"`
	Workers  int           `mapstructure:"workers"`
	Ratio    float64       `mapstructure:"ratio"`
	Endpoint string        `mapstructure:"endpoint"`
	Timeout  time.Duration `mapstructure:"timeout"`
	Peers    []string      `mapstructure:"peers"`
}

// registerTyped registers the section above with a baseline that varies by mode.
func registerTyped(t *testing.T) {
	t.Helper()
	registry.Reset()
	registry.RegisterSection("probe", &typed{}, func(m registry.Mode) any {
		return typed{
			Enabled:  m != registry.ModeArchive,
			Workers:  4,
			Ratio:    0.5,
			Endpoint: "localhost:8545",
			Timeout:  30 * time.Second,
			Peers:    []string{"a", "b"},
		}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering the probe section produced a defect: %v", d.Err)
	}
}

// seed writes a file on disk and returns its path.
func seed(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sei.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return path
}

// valuesAt reads a file's written keys back off disk.
func valuesAt(t *testing.T, path string) map[string]any {
	t.Helper()
	file, err := seitoml.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	v, err := file.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	return v
}

// TestSetWritesTheDeclaredTypeNotTheTypedText is the property set exists to hold.
//
// A value arrives from a command line as text. Written as text, the file parses and looks right to
// a reader while the node rejects it at the next boot, which is the worst place to find out.
func TestSetWritesTheDeclaredTypeNotTheTypedText(t *testing.T) {
	for _, tc := range []struct {
		key, raw string
		want     any
	}{
		{"probe.enabled", "false", false},
		{"probe.workers", "16", int64(16)},
		{"probe.ratio", "0.25", 0.25},
		{"probe.endpoint", "sei:8545", "sei:8545"},
		{"probe.timeout", "90s", "1m30s"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			registerTyped(t)
			path := seed(t, "schema_version = 1\nnode_mode = \"validator\"\n")

			change, err := configcli.Set(path, tc.key, tc.raw)
			if err != nil {
				t.Fatalf("Set(%s, %q): %v", tc.key, tc.raw, err)
			}

			if change.Had {
				t.Errorf("set reported the key was already written on an empty file: %+v", change)
			}
			got := valuesAt(t, path)[tc.key]
			if got != tc.want {
				t.Errorf("set %s to %q and the file holds %#v, want %#v. Written as the wrong type "+
					"the file still parses, and the node refuses the value at its next boot",
					tc.key, tc.raw, got, tc.want)
			}
		})
	}
}

// TestSetWritesAListAsAList covers the one type that is not a single token.
func TestSetWritesAListAsAList(t *testing.T) {
	registerTyped(t)
	path := seed(t, "schema_version = 1\nnode_mode = \"validator\"\n")

	if _, err := configcli.Set(path, "probe.peers", "x, y ,z"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, ok := valuesAt(t, path)["probe.peers"].([]any)
	if !ok {
		t.Fatalf("probe.peers is %#v, want a list", valuesAt(t, path)["probe.peers"])
	}
	if len(got) != 3 || got[0] != "x" || got[1] != "y" || got[2] != "z" {
		t.Errorf("the list reads %#v, want x, y and z with their surrounding spaces dropped", got)
	}
}

// TestSetRefusesTextThatIsNotTheDeclaredType is where the legacy coercions stop.
//
// The path a node reads today takes a blank as zero, a bool as 0 or 1, and a bare number as
// nanoseconds, discarding the error each time. Refusing at the point the operator types it is the
// only place the mistake is still cheap.
func TestSetRefusesTextThatIsNotTheDeclaredType(t *testing.T) {
	for _, tc := range []struct{ name, key, raw string }{
		{"a word for a number", "probe.workers", "lots"},
		{"a blank for a number", "probe.workers", ""},
		{"a fraction for a whole number", "probe.workers", "1.5"},
		{"a number for a bool", "probe.enabled", "1"},
		{"a blank for a bool", "probe.enabled", ""},
		{"a unit-less duration", "probe.timeout", "30"},
		{"a word for a duration", "probe.timeout", "soon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registerTyped(t)
			path := seed(t, "schema_version = 1\nnode_mode = \"validator\"\n")

			if _, err := configcli.Set(path, tc.key, tc.raw); err == nil {
				t.Errorf("set accepted %q for %s. The file would hold a value the node refuses, and "+
					"the operator finds out at the next boot", tc.raw, tc.key)
			}
			if v := valuesAt(t, path); len(v) != 0 {
				t.Errorf("a refused set still wrote to the file: %v", v)
			}
		})
	}
}

// TestSetRefusesAKeyNoSectionDeclares keeps the tool from writing a file it will not accept.
//
// Doctor refuses an undeclared written key, so a set that wrote one would produce a file the same
// tool rejects. The message names the closest declared keys, because a typo is only correctable if
// the operator can see what they meant.
func TestSetRefusesAKeyNoSectionDeclares(t *testing.T) {
	registerTyped(t)
	path := seed(t, "schema_version = 1\nnode_mode = \"validator\"\n")

	_, err := configcli.Set(path, "probe.worker", "16")
	if err == nil {
		t.Fatal("set accepted a key no section declares, so doctor would refuse the file set just wrote")
	}
	if !strings.Contains(err.Error(), "probe.workers") {
		t.Errorf("the message does not name the closest declared key: %v", err)
	}
}

// TestSetRefusesToWriteTheExperimentalTable holds a boundary the file format depends on.
//
// The experimental table is written by an operator and never by seid, which is what makes a value in
// it a value somebody chose. A table this command created would be indistinguishable from one an
// operator wrote, and a freshly generated home does not carry the table for the same reason.
func TestSetRefusesToWriteTheExperimentalTable(t *testing.T) {
	registerTyped(t)
	path := seed(t, "schema_version = 1\nnode_mode = \"validator\"\n")

	// Asserted on what the message says, not merely that one appeared. An experimental key is real
	// and hand-written, so refusing it as a key nothing declares is wrong advice: the operator would
	// go looking for a typo in a key that exists.
	_, err := configcli.Set(path, "experimental.probe.workers", "16")
	if err == nil {
		t.Fatal("set wrote into the experimental table. A section the binary created reads as a value " +
			"an operator chose")
	}
	if !strings.Contains(err.Error(), experimental.Namespace) || !strings.Contains(err.Error(), "by hand") {
		t.Errorf("set refused the key with %q, which does not tell the operator the table is written "+
			"by hand. Refused as an unknown key, they would go hunting for a typo in a key that "+
			"exists", err)
	}

	_, err = configcli.Set(path, "schema_version", "9")
	if err == nil {
		t.Fatal("set wrote the schema version, which would make the file claim a shape its contents " +
			"do not have")
	}
	if !strings.Contains(err.Error(), "upgrade") {
		t.Errorf("set refused the schema version with %q, which does not point at the verb that does "+
			"move a file forward", err)
	}
	if v := valuesAt(t, path); len(v) != 0 {
		t.Errorf("a refused set still wrote to the file: %v", v)
	}
}

// TestSetReportsWhatItReplaced is what an operator sees after changing a value.
//
// Without the previous value the report cannot distinguish a change from a no-op, and an operator
// has no record of what they overwrote.
func TestSetReportsWhatItReplaced(t *testing.T) {
	registerTyped(t)
	path := seed(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nworkers = 4\n")

	change, err := configcli.Set(path, "probe.workers", "16")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	if !change.Had || change.From != int64(4) {
		t.Errorf("set reported %+v, want the previous 4. Without it a change cannot be told from a "+
			"no-op", change)
	}
	if change.To != int64(16) {
		t.Errorf("set reported writing %#v, want 16", change.To)
	}
}

// TestUnsetRemovesTheKeyAndReportsWhetherItWasThere holds both directions of the verb.
//
// Unsetting an absent key is not an error, but it must say it did nothing, or an operator cannot
// tell a successful fallback from a mistyped key.
func TestUnsetRemovesTheKeyAndReportsWhetherItWasThere(t *testing.T) {
	registerTyped(t)
	path := seed(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nworkers = 4\nenabled = true\n")

	change, err := configcli.Unset(path, "probe.workers")
	if err != nil {
		t.Fatalf("Unset: %v", err)
	}
	if !change.Removed || !change.Had || change.From != int64(4) {
		t.Errorf("unset reported %+v, want the removal of a key holding 4", change)
	}
	values := valuesAt(t, path)
	if _, present := values["probe.workers"]; present {
		t.Errorf("the key is still written: %v", values)
	}
	if values["probe.enabled"] != true {
		t.Errorf("unsetting one key disturbed another: %v", values)
	}

	again, err := configcli.Unset(path, "probe.workers")
	if err != nil {
		t.Fatalf("Unset on an absent key: %v", err)
	}
	if again.Removed || again.Had {
		t.Errorf("unset reported %+v for a key already gone. An operator cannot tell a successful "+
			"fallback from a mistyped key", again)
	}
}

// TestSetAndUnsetRoundTripThroughTheFile is the pair's own property.
//
// Held on a key whose baseline differs from the value that was set, since otherwise the state
// before and after are indistinguishable and the test would pass for an unset that did nothing.
func TestSetAndUnsetRoundTripThroughTheFile(t *testing.T) {
	registerTyped(t)
	path := seed(t, "schema_version = 1\nnode_mode = \"validator\"\n")

	if _, err := configcli.Set(path, "probe.workers", "16"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := valuesAt(t, path)["probe.workers"]; got != int64(16) {
		t.Fatalf("after set the file holds %#v, want 16", got)
	}

	if _, err := configcli.Unset(path, "probe.workers"); err != nil {
		t.Fatalf("Unset: %v", err)
	}

	file, err := seitoml.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	values, _ := file.Values()
	if _, present := values["probe.workers"]; present {
		t.Errorf("the key survives the round trip: %v", values)
	}
	// And it now resolves to the baseline, which is what removing it was for.
	comparison, err := configcli.Diff(file, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	var tracks bool
	for _, k := range comparison.Tracking {
		if k == "probe.workers" {
			tracks = true
		}
	}
	if !tracks {
		t.Errorf("after unset the key does not track the baseline: %+v", comparison)
	}
}

// TestSetPreservesTheCommentOnTheKeyItChanges is the reason the file is edited rather than rewritten.
//
// Held through the verb rather than at the file layer, because it is the verb an operator runs and
// a rewrite anywhere in that path loses the same comments.
func TestSetPreservesTheCommentOnTheKeyItChanges(t *testing.T) {
	registerTyped(t)
	const note = "# Raised during the March load test."
	path := seed(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\n"+note+"\nworkers = 4\n")

	if _, err := configcli.Set(path, "probe.workers", "16"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	raw, err := os.ReadFile(path) //nolint:gosec // a path this test created under t.TempDir
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), note) {
		t.Errorf("set dropped the comment explaining the value it changed. The file reads:\n%s", raw)
	}
}

// TestAKeysTypeDoesNotVaryByMode is what lets set read a type without being told a mode.
//
// The type comes from a resolved baseline, and only one mode is resolved to get it. That is only
// sound while every mode agrees, so a section whose baseline returned a different type per mode
// would silently make set coerce to whichever mode happened to be first.
func TestAKeysTypeDoesNotVaryByMode(t *testing.T) {
	registerTyped(t)

	types := map[string]map[string]string{}
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", mode, err)
		}
		types[string(mode)] = map[string]string{}
		for key, res := range resolved.Keys {
			types[string(mode)][key] = reflect.TypeOf(res.Value).String()
		}
	}

	first := string(registry.Modes()[0])
	for mode, got := range types {
		for key, typ := range got {
			if want := types[first][key]; typ != want {
				t.Errorf("%s is a %s in %q mode and a %s in %q mode. set reads a key's type from one "+
					"mode, so it would coerce an operator's value to the wrong type", key, typ, mode,
					want, first)
			}
		}
	}
}

// TestDiffSeparatesChosenValuesFromTrackedOnes is what the verb answers.
//
// A file alone cannot say which of its values somebody decided on. Written keys that match the
// baseline are reported as agreeing rather than as tracking, because the two behave differently the
// moment a release changes that baseline.
func TestDiffSeparatesChosenValuesFromTrackedOnes(t *testing.T) {
	registerTyped(t)
	file := parseFile(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nworkers = 16\nratio = 0.5\nstray = 1\n")

	got, err := configcli.Diff(file, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	if len(got.Differing) != 1 || got.Differing[0].Key != "probe.workers" {
		t.Errorf("differing keys are %+v, want only probe.workers, whose written 16 is not the "+
			"baseline 4", got.Differing)
	}
	if d := got.Differing[0]; d.Written != int64(16) || d.Baseline != 4 {
		t.Errorf("the difference reads %+v, want written 16 against baseline 4", d)
	}
	// ratio is written and equals its baseline, so it is neither differing nor tracking.
	for _, k := range got.Tracking {
		if k == "probe.ratio" {
			t.Error("a written key that matches its baseline was reported as tracking it. The two " +
				"behave differently the moment a release changes that baseline")
		}
	}
	if len(got.Undeclared) != 1 || got.Undeclared[0] != "probe.stray" {
		t.Errorf("undeclared keys are %v, want the one no section declares", got.Undeclared)
	}
	// Six keys are declared and the file writes two of them, so four track the baseline.
	if len(got.Tracking) != 4 {
		t.Errorf("tracking keys are %v, want the 4 declared keys the file does not write", got.Tracking)
	}
}

// TestDiffDoesNotReportEqualValuesAsDifferentBecauseOfTheirTypes is the trap this comparison has.
//
// A file yields int64 for every whole number while a struct field may be an int or a uint, and a
// duration is written as text while its baseline is a time.Duration. Compared naively, a file that
// agrees with the binary exactly would report every one of those keys as changed, and the verb would
// be useless on precisely the file generate produces.
func TestDiffDoesNotReportEqualValuesAsDifferentBecauseOfTheirTypes(t *testing.T) {
	registerTyped(t)
	generated, err := configcli.Generate(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	reread := parseFile(t, render(t, generated))

	got, err := configcli.Diff(reread, registry.ModeValidator)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	if len(got.Differing) != 0 {
		t.Errorf("a freshly generated file differs from the baselines it was generated from: %+v\n\n"+
			"Each of these is the same value read back at a different Go type", got.Differing)
	}
	if len(got.Tracking) != 0 || len(got.Undeclared) != 0 {
		t.Errorf("a generated file reports %d tracking and %d undeclared keys, want none of either",
			len(got.Tracking), len(got.Undeclared))
	}
	if !strings.Contains(got.Report(), "matches this binary's defaults") {
		t.Errorf("the report for a generated file reads %q", got.Report())
	}
}

// TestDiffFollowsTheModeTheFileRecords holds that the recorded mode reaches the comparison.
//
// Two files carrying the same value and different modes must compare differently, or the mode never
// reached the baselines and an archive node would be measured against a validator's defaults.
func TestDiffFollowsTheModeTheFileRecords(t *testing.T) {
	registerTyped(t)
	const body = "schema_version = 1\nnode_mode = %q\n\n[probe]\nenabled = true\n"

	validator, err := configcli.Diff(parseFile(t, fmt.Sprintf(body, "validator")), "")
	if err != nil {
		t.Fatalf("Diff(validator file): %v", err)
	}
	archive, err := configcli.Diff(parseFile(t, fmt.Sprintf(body, "archive")), "")
	if err != nil {
		t.Fatalf("Diff(archive file): %v", err)
	}

	if validator.Mode != registry.ModeValidator || archive.Mode != registry.ModeArchive {
		t.Errorf("the comparisons ran against %q and %q, want the modes the files record",
			validator.Mode, archive.Mode)
	}
	if len(validator.Differing) != 0 {
		t.Errorf("enabled = true differs from the validator baseline, which is also true: %+v",
			validator.Differing)
	}
	if len(archive.Differing) != 1 {
		t.Errorf("enabled = true does not differ from the archive baseline, which is false: %+v.\n"+
			"A comparison that ignored the recorded mode would measure an archive node against a "+
			"validator's defaults", archive.Differing)
	}
}

// TestDiffRefusesAModeThatDisagreesWithTheFile is why the recorded mode is not merely a default.
//
// Silently preferring either one produces a comparison the operator did not ask for. The
// disagreement is the thing they need to see, so it is the answer.
func TestDiffRefusesAModeThatDisagreesWithTheFile(t *testing.T) {
	registerTyped(t)
	file := parseFile(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nenabled = true\n")

	_, err := configcli.Diff(file, registry.ModeArchive)
	if err == nil {
		t.Fatal("a comparison against a mode the file does not record was accepted, so an operator " +
			"would read differences that are not differences")
	}
	for _, want := range []string{"validator", "archive"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q, so the operator cannot see which two disagree: %v",
				want, err)
		}
	}
	// Naming the same mode the file records is not a disagreement.
	if _, err := configcli.Diff(file, registry.ModeValidator); err != nil {
		t.Errorf("naming the mode the file already records was refused: %v", err)
	}
	if _, err := configcli.Diff(file, registry.Mode("archival")); err == nil {
		t.Error("Diff accepted a mode no node runs")
	}
}

// TestDiffRefusesAFileWithNoUsableMode keeps a comparison from guessing which defaults to use.
//
// A file that records no mode, or one no release produced, cannot be compared against anything:
// picking a mode chooses whose defaults the operator is shown against, and either choice is wrong
// often enough to be useless.
func TestDiffRefusesAFileWithNoUsableMode(t *testing.T) {
	registerTyped(t)

	for _, tc := range []struct{ name, body string }{
		{"absent", "schema_version = 1\n\n[probe]\nenabled = true\n"},
		{"unknown", "schema_version = 1\nnode_mode = \"archival\"\n"},
		{"empty", "schema_version = 1\nnode_mode = \"\"\n"},
		{"not text", "schema_version = 1\nnode_mode = 3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := configcli.Diff(parseFile(t, tc.body), ""); err == nil {
				t.Errorf("a file whose mode is %s was compared anyway, against whichever defaults the "+
					"code happened to pick", tc.name)
			}
		})
	}
}

// TestDoctorRefusesAFileWhoseModeItCannotUse holds the check that makes every other answer mean
// something.
//
// A mode this binary does not know leaves nothing able to resolve the defaults the file's values were
// chosen against, so no comparison can run and the node cannot be told whether its settings still
// mean what they meant. Reporting the keys as fine while the mode is unusable would read as a clean
// bill of health.
func TestDoctorRefusesAFileWhoseModeItCannotUse(t *testing.T) {
	registerTyped(t)

	for _, tc := range []struct{ name, body string }{
		{"absent", "schema_version = 1\n\n[probe]\nenabled = true\n"},
		{"unknown", "schema_version = 1\nnode_mode = \"archival\"\n\n[probe]\nenabled = true\n"},
		{"not text", "schema_version = 1\nnode_mode = 3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := configcli.Doctor(parseFile(t, tc.body))
			if err != nil {
				t.Fatalf("Doctor: %v", err)
			}

			if d.Healthy() {
				t.Errorf("a file whose mode is %s was reported healthy. Nothing can resolve the defaults "+
					"its values were chosen against, so a clean report is a false one", tc.name)
			}
			if d.ModeProblem == "" {
				t.Errorf("the diagnosis carries no mode problem: %+v. An operator cannot fix what the "+
					"report does not name", d)
			}
			if !strings.Contains(d.Report(), "node mode") {
				t.Errorf("the report does not mention the mode:\n%s", d.Report())
			}
		})
	}

	// A usable mode leaves the diagnosis clean and names the mode it checked against, or the
	// assertions above would hold for a doctor that refused every file.
	good := parseFile(t, "schema_version = 1\nnode_mode = \"archive\"\n\n[probe]\nenabled = true\n")
	d, err := configcli.Doctor(good)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if !d.Healthy() || d.ModeProblem != "" {
		t.Errorf("a file recording a real mode was refused: %+v", d)
	}
	if d.Mode != "archive" {
		t.Errorf("the diagnosis reports mode %q, want archive", d.Mode)
	}
	if !strings.Contains(d.Report(), "archive") {
		t.Errorf("the clean report does not say which mode it checked against:\n%s", d.Report())
	}
}

// TestDoctorRefusesAValueItCannotRead closes the gap between what set checks and what a hand-edited
// file can hold.
//
// set converts a value on the way in, so a value typed at the command line can never be the wrong
// type. Hand-editing the file is equally legitimate and reaches no such check, so without this a
// file could hold a value the node refuses at its next start while doctor called it healthy. That is
// the worst place to find out.
func TestDoctorRefusesAValueItCannotRead(t *testing.T) {
	registerTyped(t)
	file := parseFile(t, `schema_version = 1
node_mode = "validator"

[probe]
workers = "banana"
enabled = 42
timeout = 30
ratio = "half"
endpoint = "sei:8545"
`)

	d, err := configcli.Doctor(file)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if d.Healthy() {
		t.Errorf("a file holding values this binary cannot read was called healthy: %s", d.Report())
	}
	malformed := map[string]bool{}
	for _, m := range d.Malformed {
		malformed[m.Key] = true
		if m.Reason == "" || m.Want == "" {
			t.Errorf("a malformation carries no reason or no expected type: %+v. An operator cannot "+
				"correct a value the report does not describe", m)
		}
	}
	for _, key := range []string{"probe.workers", "probe.enabled", "probe.timeout", "probe.ratio"} {
		if !malformed[key] {
			t.Errorf("%q holds a value of the wrong type and doctor did not report it: %+v", key,
				d.Malformed)
		}
	}
	// The one value that is the right type is not reported, or this would hold for a doctor that
	// refused every value.
	if malformed["probe.endpoint"] {
		t.Errorf("probe.endpoint holds a valid string and was reported malformed: %+v", d.Malformed)
	}
	// A malformed key is recognized, so it must not also be reported as unrecognized.
	if len(d.Unrecognized) != 0 {
		t.Errorf("malformed keys were also reported as unrecognized: %v. They are declared; it is "+
			"their values that are wrong, and the two need different fixes", d.Unrecognized)
	}
	if !strings.Contains(d.Report(), "cannot read") {
		t.Errorf("the report does not flag the unreadable values:\n%s", d.Report())
	}
}

// TestDoctorAcceptsEveryTypeAFileLegitimatelyHolds is the other direction.
//
// A configuration file writes a duration as text and every whole number as an integer, so a check
// that demanded the exact Go type would report a correct file as broken. This drives one valid value
// per declared type through the same path.
func TestDoctorAcceptsEveryTypeAFileLegitimatelyHolds(t *testing.T) {
	registerTyped(t)
	generated, err := configcli.Generate(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	d, err := configcli.Doctor(parseFile(t, render(t, generated)))
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !d.Healthy() {
		t.Errorf("doctor refused the file generate just wrote: %s\nEvery value in it came from the "+
			"binary's own baseline, so a check that rejects one is checking the wrong thing", d.Report())
	}
	if len(d.Malformed) != 0 {
		t.Errorf("a generated file reported %d malformed values: %+v", len(d.Malformed), d.Malformed)
	}
}

// TestDoctorExitsNonZeroOnAValueItCannotRead is what lets a deploy gate on the check.
func TestDoctorExitsNonZeroOnAValueItCannotRead(t *testing.T) {
	registerTyped(t)
	home := newHome(t)
	if err := os.WriteFile(configcli.Path(home),
		[]byte("schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nworkers = \"banana\"\n"),
		0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	out, err := invoke(t, home, "doctor")
	if err == nil {
		t.Errorf("doctor exited zero on a file holding a value it cannot read, so nothing automated "+
			"can gate on it:\n%s", out)
	}
	if !strings.Contains(out, "probe.workers") {
		t.Errorf("doctor did not name the value it cannot read:\n%s", out)
	}
}
