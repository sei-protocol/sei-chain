package configcli_test

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configcli"
	"github.com/sei-protocol/sei-chain/config/experimental"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
)

// registerGiga registers the giga executor the way its own package would.
//
// The struct under test is the real one, so what generate writes is measured against the keys the
// live reader actually resolves rather than against a copy of them.
func registerGiga(t *testing.T) {
	t.Helper()
	registry.Reset()
	registry.RegisterSection("giga_executor", &gigaconfig.Config{}, func(m registry.Mode) any {
		return gigaconfig.Config{Enabled: true, OCCEnabled: m != registry.ModeArchive}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering giga_executor produced a defect: %v", d.Err)
	}
}

func render(t *testing.T, f *seitoml.File) string {
	t.Helper()
	raw, err := f.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return string(raw)
}

// TestGenerateWritesEveryDeclaredKeyAtTheModesBaseline is the verb an external node operator
// reaches for, and the only one they should need.
//
// Completeness is the point: a key generate skipped is a setting the operator cannot discover from
// the file, so this compares against the whole declared set rather than a sample of it.
func TestGenerateWritesEveryDeclaredKeyAtTheModesBaseline(t *testing.T) {
	registerGiga(t)

	file, err := configcli.Generate(registry.ModeArchive)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	written, err := file.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	for _, key := range registry.Keys() {
		if _, present := written[key]; !present {
			t.Errorf("generate did not write %q. A key absent from the file is one an operator "+
				"cannot discover, and they have no way to know it exists", key)
		}
	}
	if len(written) != len(registry.Keys()) {
		t.Errorf("generate wrote %d keys and the registry declares %d: %v. An extra key is one no "+
			"section owns, and doctor would refuse the file this verb just produced",
			len(written), len(registry.Keys()), written)
	}
	// The values are the mode's, not the zero value of each type.
	if written["giga_executor.occ_enabled"] != false {
		t.Errorf("occ_enabled is %#v on an archive node, want false from that mode's baseline",
			written["giga_executor.occ_enabled"])
	}
	if written["giga_executor.enabled"] != true {
		t.Errorf("enabled is %#v, want true", written["giga_executor.enabled"])
	}
}

// TestGenerateFollowsTheModeItWasGiven holds that the mode reaches the file.
//
// Without it the test above would pass for a generate that ignored its argument and wrote one
// baseline everywhere, which is exactly the bug that would put an archive node's settings on a
// validator.
func TestGenerateFollowsTheModeItWasGiven(t *testing.T) {
	registerGiga(t)

	archive, err := configcli.Generate(registry.ModeArchive)
	if err != nil {
		t.Fatalf("Generate(archive): %v", err)
	}
	validator, err := configcli.Generate(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Generate(validator): %v", err)
	}

	a, _ := archive.Values()
	v, _ := validator.Values()
	if a["giga_executor.occ_enabled"] == v["giga_executor.occ_enabled"] {
		t.Errorf("both modes wrote %#v for a key whose baseline varies by mode, so the mode never "+
			"reached the file", a["giga_executor.occ_enabled"])
	}
}

// TestGenerateIsByteStable is what makes two generated files comparable.
//
// Map iteration order is not stable in Go, so a generate that walked a map directly would produce
// a different file on every run. Two nodes could then have identical configuration and a diff that
// showed every line as changed.
func TestGenerateIsByteStable(t *testing.T) {
	registerGiga(t)

	first, err := configcli.Generate(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	second, err := configcli.Generate(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if a, b := render(t, first), render(t, second); a != b {
		t.Errorf("two runs of one binary produced different files:\n%s\n---\n%s", a, b)
	}
}

// TestGenerateWritesKeysInSortedOrder is the deterministic half of the property above.
//
// Comparing two runs only catches unstable ordering when the iteration happens to differ, which
// for a handful of keys is a coin toss and would make the test flake rather than fail. Asserting
// the order directly is what makes it reliable, so the section here declares its keys out of
// alphabetical order on purpose.
func TestGenerateWritesKeysInSortedOrder(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &struct {
		Zulu    bool `mapstructure:"zulu"`
		Alpha   bool `mapstructure:"alpha"`
		Mike    bool `mapstructure:"mike"`
		Bravo   bool `mapstructure:"bravo"`
		Yankee  bool `mapstructure:"yankee"`
		Charlie bool `mapstructure:"charlie"`
	}{}, func(registry.Mode) any {
		return struct {
			Zulu    bool `mapstructure:"zulu"`
			Alpha   bool `mapstructure:"alpha"`
			Mike    bool `mapstructure:"mike"`
			Bravo   bool `mapstructure:"bravo"`
			Yankee  bool `mapstructure:"yankee"`
			Charlie bool `mapstructure:"charlie"`
		}{}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering the probe section produced a defect: %v", d.Err)
	}

	file, err := configcli.Generate(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var got []string
	for _, line := range strings.Split(render(t, file), "\n") {
		if name, _, ok := strings.Cut(strings.TrimSpace(line), " = "); ok && !strings.HasPrefix(name, "#") {
			got = append(got, name)
		}
	}
	// The two header keys come first, then the section's keys in order. schema_version and node_mode
	// describe the file rather than configuring the node, so they sit outside the sorted run.
	want := []string{"schema_version", "node_mode", "alpha", "bravo", "charlie", "mike", "yankee", "zulu"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the file's keys are in the order %v, want %v. Written in map order, two nodes with "+
			"identical configuration would produce a diff showing every line as changed", got, want)
	}
}

// TestAGeneratedFileSaysItsValuesAreCommitments records the consequence of writing every key.
//
// Generate fills in a value for every declared key, and this binary treats a written value as the
// operator's decision and never rewrites it. So a generated node keeps these values across an
// upgrade even where a later release ships a different default, and only regenerating moves it
// forward.
//
// Pinned here so the property is a stated one rather than something discovered later against a
// node that quietly stopped tracking its defaults. If generate ever stops materializing unchosen
// keys, this is the test that should change with it.
func TestAGeneratedFileSaysItsValuesAreCommitments(t *testing.T) {
	registerGiga(t)

	file, err := configcli.Generate(registry.ModeArchive)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	body := render(t, file)
	for _, must := range []string{"archive", "never rewrites", "across an upgrade"} {
		if !strings.Contains(body, must) {
			t.Errorf("the generated file does not tell its reader %q. Every value in it was filled "+
				"in from a default, and an operator cannot otherwise tell those apart from the ones "+
				"they chose:\n%s", must, body)
		}
	}
	// The preamble is comments, so nothing it says becomes a key the node has to recognize.
	written, err := file.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	for key := range written {
		if !strings.HasPrefix(key, "giga_executor.") {
			t.Errorf("the file carries %q, which no section declares. The preamble must be comments, "+
				"not configuration", key)
		}
	}
}

// TestRegeneratingDoesNotStackPreambles holds the second run of the verb.
//
// Regenerating is the documented way to move a node onto current defaults, so it happens more than
// once on the same node. A preamble appended each time would grow the file's header without bound.
func TestRegeneratingDoesNotStackPreambles(t *testing.T) {
	registerGiga(t)
	file, err := configcli.Generate(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	reread, err := seitoml.Parse(strings.NewReader(render(t, file)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	reread.SetPreamble([]string{" replaced"})

	body := render(t, reread)
	if got := strings.Count(body, "never rewrites"); got != 0 {
		t.Errorf("the previous preamble survived %d time(s) alongside the new one:\n%s", got, body)
	}
	if !strings.Contains(body, "# replaced") {
		t.Errorf("the new preamble was not written:\n%s", body)
	}
}

// TestGenerateRefusesAModeNoNodeRuns keeps a plausible file for a nonexistent mode from existing.
//
// A section's baseline function is free to answer for any mode its switch does not name, so
// resolving an unknown one produces a complete and entirely wrong file rather than an error.
func TestGenerateRefusesAModeNoNodeRuns(t *testing.T) {
	registerGiga(t)

	if _, err := configcli.Generate(registry.Mode("archival")); err == nil {
		t.Error("generate accepted a mode no node runs, so it would write a complete file whose " +
			"every value came from a baseline function's default branch")
	}
}

// TestGenerateRefusesAnEmptyRegistry keeps an empty file from reading as a configured node.
//
// A file with a schema version and no keys is valid and says nothing. Written to a node's config
// directory it looks like a successful generate, and every setting silently tracks a baseline the
// operator never saw.
func TestGenerateRefusesAnEmptyRegistry(t *testing.T) {
	registry.Reset()

	if _, err := configcli.Generate(registry.ModeValidator); err == nil {
		t.Error("generate produced a file from an empty registry, which reads as a node with " +
			"nothing to configure")
	}
}

// TestDoctorRefusesAnUnrecognizedStableKeyAndPermitsExperimental is the asymmetry that makes the
// experimental namespace worth having.
//
// A written stable key the binary does not recognize is a broken promise: the operator wrote it
// believing it would take effect and it will not. An experimental key carries no such promise, so
// it warns and the node boots. All three directions are asserted, because checking only the refusal
// would pass for a doctor that refused everything.
func TestDoctorRefusesAnUnrecognizedStableKeyAndPermitsExperimental(t *testing.T) {
	registerGiga(t)
	experimental.Reset()
	experimental.Int(experimental.Decl[int]{
		Name: "probe.workers", Default: 8, Owner: "configtest", Since: "v6.6.0",
	})

	file := parseFile(t, `schema_version = 1
node_mode = "validator"

[giga_executor]
enabled = true

[experimental]
probe.workers = 16
probe.unknown = 1
`)

	d, err := configcli.Doctor(file)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !d.Healthy() {
		t.Errorf("a file whose only stable key is declared was refused: %+v", d)
	}
	if len(d.UnrecognizedExperimental) != 1 || d.UnrecognizedExperimental[0] != "experimental.probe.unknown" {
		t.Errorf("undeclared experimental keys are %v, want the one this binary does not declare. "+
			"Warning on it is what the experimental namespace offers instead of a refusal",
			d.UnrecognizedExperimental)
	}
	if d.Checked != 3 {
		t.Errorf("doctor examined %d keys, want 3. A count that does not match the file means a "+
			"clean result cannot be told apart from one that examined nothing", d.Checked)
	}

	// The refusal direction, on the same declared set.
	broken := parseFile(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[giga_executor]\nnot_a_key = true\n")
	d, err = configcli.Doctor(broken)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if d.Healthy() {
		t.Error("a written stable key no section declares was accepted. The operator wrote a " +
			"setting expecting it to take effect, and nothing tells them it does not")
	}
	if len(d.Unrecognized) != 1 || d.Unrecognized[0] != "giga_executor.not_a_key" {
		t.Errorf("unrecognized keys are %v, want the one undeclared key", d.Unrecognized)
	}
}

// TestDoctorPassesAFileGenerateJustWrote is the pair the two verbs owe each other.
//
// Generate writes every declared key and doctor refuses any key that is not declared. If they
// disagree, the tool refuses the file it just produced, on a fresh node, before anything else has
// happened.
func TestDoctorPassesAFileGenerateJustWrote(t *testing.T) {
	registerGiga(t)
	experimental.Reset()

	file, err := configcli.Generate(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	reread := parseFile(t, render(t, file))

	d, err := configcli.Doctor(reread)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !d.Healthy() {
		t.Errorf("doctor refused the file generate just wrote: %s\nThat is a fresh node refusing "+
			"its own configuration before an operator has touched it", d.Report())
	}
	if d.Checked != len(registry.Keys()) {
		t.Errorf("doctor examined %d of the %d keys generate wrote", d.Checked, len(registry.Keys()))
	}
}

// TestDoctorReportsARetiredExperimentalKeySeparately keeps the two experimental cases apart.
//
// A retired key was real once and an operator's file may carry it from an earlier release, so the
// advice is to look up what replaced it. An undeclared key was never real, and the advice is that
// it does nothing. Reported together, neither operator gets the right instruction.
func TestDoctorReportsARetiredExperimentalKeySeparately(t *testing.T) {
	registerGiga(t)
	experimental.Reset()
	experimental.Retired(experimental.Tombstone{
		Name: "probe.old", Type: "int", Owner: "configtest", Since: "v6.5.0", RetiredIn: "v6.7.0",
	})

	file := parseFile(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[experimental]\nprobe.old = 1\n")

	d, err := configcli.Doctor(file)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if len(d.Retired) != 1 || d.Retired[0] != "experimental.probe.old" {
		t.Errorf("retired keys are %v and undeclared ones are %v; a retired key belongs in the "+
			"first, because the operator needs to be told what replaced it rather than that it "+
			"never existed", d.Retired, d.UnrecognizedExperimental)
	}
	if !d.Healthy() {
		t.Error("a retired experimental key stopped the node. Retiring one is a change we made, " +
			"not a mistake the operator made")
	}
}

// TestDoctorIgnoresKeysTheFileDoesNotWrite is why it walks the written set.
//
// An unwritten key resolves to the baseline, which is healthy by definition. A doctor that walked
// the declared set instead would report every unwritten key, so a correct file would produce a
// report as long as the registry.
func TestDoctorIgnoresKeysTheFileDoesNotWrite(t *testing.T) {
	registerGiga(t)
	experimental.Reset()

	d, err := configcli.Doctor(parseFile(t, "schema_version = 1\nnode_mode = \"validator\"\n"))
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !d.Healthy() || d.Checked != 0 {
		t.Errorf("an empty file produced %+v, want a healthy diagnosis over zero keys. Every key it "+
			"omits resolves to the baseline, which is what an unwritten key means", d)
	}
	if got := d.Report(); !strings.Contains(got, "0 written key(s)") {
		t.Errorf("the report for an empty file reads %q, which does not say that nothing was "+
			"checked. A clean report over no keys reads as a clean file", got)
	}
}

// parseFile parses a file body.
func parseFile(t *testing.T, body string) *seitoml.File {
	t.Helper()
	f, err := seitoml.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}
