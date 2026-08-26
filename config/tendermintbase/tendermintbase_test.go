package tendermintbase

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/seeds"
	"github.com/sei-protocol/sei-chain/config/registry"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/spf13/viper"
)

// whatVariesByNodeKind is every key these sections answer differently depending on the kind of node.
//
// Held by name and value rather than described, because two of these decide what a node exposes to the
// network. A rule that stopped varying would leave a validator binding the address a query-serving node
// binds, and a comment saying otherwise cannot fail.
var whatVariesByNodeKind = map[string]map[registry.Mode]string{
	"p2p.laddr": {
		registry.ModeValidator: "tcp://127.0.0.1:26656",
		registry.ModeSeed:      "tcp://127.0.0.1:26656",
		registry.ModeFull:      "tcp://0.0.0.0:26656",
		registry.ModeArchive:   "tcp://0.0.0.0:26656",
	},
	"rpc.laddr": {
		registry.ModeValidator: "tcp://127.0.0.1:26657",
		registry.ModeSeed:      "tcp://127.0.0.1:26657",
		registry.ModeFull:      "tcp://0.0.0.0:26657",
		registry.ModeArchive:   "tcp://0.0.0.0:26657",
	},
	"p2p.max-connections": {
		registry.ModeValidator: "100",
		registry.ModeSeed:      "1000",
		registry.ModeFull:      "100",
		registry.ModeArchive:   "100",
	},
	"p2p.allow-duplicate-ip": {
		registry.ModeValidator: "false",
		registry.ModeSeed:      "true",
		registry.ModeFull:      "false",
		registry.ModeArchive:   "false",
	},
	"tx-index.indexer": {
		registry.ModeValidator: "[null]",
		registry.ModeSeed:      "[null]",
		registry.ModeFull:      "[kv]",
		registry.ModeArchive:   "[kv]",
	},
}

// declaredSections are the sections this package registers, so a test walks the set rather than a list that
// has to be extended alongside it.
func declaredSections() []string { return SectionsRegisteredHere() }

// declaredHere are the keys the sections this package registers declare.
//
// Read out of the registry rather than matched against the section names as prefixes, because a section
// whose keys sit at the root of the file has no prefix for a match to find. The prefix form was blind to
// every one of those keys, so the claim that they answer the same for every node kind was measured by
// nothing. It also assumed a section's name and its prefix are the same string, which is a thing the
// registry keeps separate.
func declaredHere(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, name := range declaredSections() {
		registered, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("%s is not registered; Defects: %v", name, registry.Defects())
		}
		out = append(out, registered.Keys...)
	}
	return out
}

// TestWhatVariesByNodeKindIsTheRecordedSet measures the mode rules through the declared values.
//
// A key that starts varying fails and so does one that stops, which means changing a rule has to account
// for its row here. Two rows are the reason this is measured rather than stated: the listen addresses are
// what put a request surface on a node, and a validator holds a signing key.
func TestWhatVariesByNodeKindIsTheRecordedSet(t *testing.T) {
	byMode := map[registry.Mode]map[string]any{}
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err != nil {
			t.Fatalf("Resolve(%s): %v", mode, err)
		}
		byMode[mode] = resolved.Values
	}

	var measured []string
	for _, key := range declaredHere(t) {
		seen := map[string]bool{}
		for _, mode := range registry.Modes() {
			seen[fmt.Sprint(byMode[mode][key])] = true
		}
		if len(seen) == 1 {
			if _, recorded := whatVariesByNodeKind[key]; recorded {
				t.Errorf("%s is recorded as varying by node kind and answers the same for every mode. "+
					"Take it off the record, so the record stays the set of keys a generated file writes "+
					"differently per kind of node", key)
			}
			continue
		}
		measured = append(measured, key)
		want, recorded := whatVariesByNodeKind[key]
		if !recorded {
			t.Errorf("%s varies by node kind and nothing records it", key)
			continue
		}
		for _, mode := range registry.Modes() {
			if got := fmt.Sprint(byMode[mode][key]); got != want[mode] {
				t.Errorf("%s for %s is %q, recorded as %q", key, mode, got, want[mode])
			}
		}
	}

	sort.Strings(measured)
	if len(measured) != len(whatVariesByNodeKind) {
		t.Errorf("measured %d keys varying by node kind and %d are recorded: %v",
			len(measured), len(whatVariesByNodeKind), measured)
	}
}

// declaredAgainst pairs each section with the struct it declares against and how many of that struct's
// paths it leaves out.
//
// A section absent from it is a section whose key count nothing measures. The root section is not here
// because it declares against a local schema whose squashed group counts as one tagged field, so the
// arithmetic this table does would be wrong for it; a test of its own holds that section.
var declaredAgainst = []struct {
	section string
	proto   any
	exclude int
}{
	{P2PSectionName, &tmcfg.P2PConfig{}, 3},
	{RPCSectionName, &tmcfg.RPCConfig{}, 1},
	{ConsensusSectionName, &tmcfg.ConsensusConfig{}, len(removedSettings) + 1},
	{MempoolSectionName, &tmcfg.MempoolConfig{}, len(neverReachTheMempool) + 1},
	{StateSyncSectionName, &tmcfg.StateSyncConfig{}, 0},
	{TxIndexSectionName, &tmcfg.TxIndexConfig{}, 0},
	{InstrumentationSectionName, &tmcfg.InstrumentationConfig{}, 1},
	{PrivValidatorSectionName, &tmcfg.PrivValidatorConfig{}, 1},
	{SelfRemediationSectionName, &tmcfg.SelfRemediationConfig{}, 0},
}

// TestEveryExclusionByDeprecationStillHasOne closes the direction the check beside it cannot see.
//
// That check walks declared keys, so it catches a declared key whose field becomes deprecated and never
// an excluded key whose field stops being one. The second is the expiry that matters: the node dropping
// the note and starting to honour a setting leaves the key absent from the space, an operator told it
// reaches nothing, and every other test here passing.
//
// Deprecation is the default justification, so an exclusion resting on something else has to say so. The
// four that do are each named by the constant that carries their reason, which is what keeps this from
// becoming a list somebody has to remember: a new exclusion is either marked deprecated or it fails here
// until its reason is written down.
func TestEveryExclusionByDeprecationStillHasOne(t *testing.T) {
	justifiedOtherwise := map[string]string{
		filledFromTheCommandLine:      "the command line carries it after the file is read",
		derivedFromTheConnectionLimit: "unset is the setting, and a default here would be invented",
		readByNothing:                 "nothing reads it and no generated file carries it",
		"max-batch-bytes":             "no code reads it, and its field carries no deprecation note",
	}

	for _, tc := range declaredAgainst {
		registered, ok := registry.Lookup(tc.section)
		if !ok {
			t.Errorf("%s is not registered; Defects: %v", tc.section, registry.Defects())
			continue
		}
		marked := deprecatedPaths(t, reflect.TypeOf(tc.proto).Elem())
		for _, key := range registered.Excluded {
			rel := strings.TrimPrefix(key, tc.section+".")
			if marked[rel] {
				continue
			}
			if _, stated := justifiedOtherwise[rel]; stated {
				continue
			}
			t.Errorf("%s is excluded and its field is not marked deprecated, so either the node now "+
				"honours the setting and the key belongs declared, or the reason for leaving it out "+
				"needs saying", key)
		}
	}
}

// TestNoDeclaredKeyNamesADeprecatedField holds every section against its struct's own marking.
//
// A field the node marks deprecated is one a written value cannot change, so declaring it offers a key
// that reads as a setting and is not one. The consensus section had this check from the start against a
// hand-kept list; this is the same rule for every section, which is what catches the next one rather than
// the one already found.
//
// Read from the source, because the node marks a field two ways and a tag walk sees only the prefix on
// the field name. The standard comment above a field is how most of them are marked.
func TestNoDeclaredKeyNamesADeprecatedField(t *testing.T) {
	for _, name := range declaredSections() {
		registered, ok := registry.Lookup(name)
		if !ok {
			t.Errorf("%s is not registered; Defects: %v", name, registry.Defects())
			continue
		}
		proto, named := markedIn[name]
		if !named {
			t.Errorf("%s declares keys and no struct is named as carrying their markings, so nothing "+
				"measures whether any of them is a setting the node removed", name)
			continue
		}
		marked := deprecatedPaths(t, reflect.TypeOf(proto).Elem())
		for _, key := range registered.Keys {
			rel := strings.TrimPrefix(key, name+".")
			if marked[rel] {
				t.Errorf("%s names a field the node marks deprecated, so it offers a setting a written "+
					"value cannot change", key)
			}
		}
	}
}

// markedIn pairs each section with the node's own struct whose deprecation markings apply to its keys.
//
// Named per section rather than taken from what the section declares against, because the root one
// declares against a local schema that squashes the node's base group. The markings that apply to those
// keys live on that group, and a schema this package wrote carries none of them.
//
// Walked from the registered set rather than from this map, so a section added without a row here fails
// instead of skipping the check.
var markedIn = map[string]any{
	P2PSectionName:             &tmcfg.P2PConfig{},
	RPCSectionName:             &tmcfg.RPCConfig{},
	ConsensusSectionName:       &tmcfg.ConsensusConfig{},
	MempoolSectionName:         &tmcfg.MempoolConfig{},
	StateSyncSectionName:       &tmcfg.StateSyncConfig{},
	TxIndexSectionName:         &tmcfg.TxIndexConfig{},
	InstrumentationSectionName: &tmcfg.InstrumentationConfig{},
	PrivValidatorSectionName:   &tmcfg.PrivValidatorConfig{},
	SelfRemediationSectionName: &tmcfg.SelfRemediationConfig{},
	RootSectionName:            &tmcfg.BaseConfig{},
}

// TestTheDeclaredKeysAreTheOnesTheReaderDecodes holds the declaration to the struct the node decodes into.
//
// Derived from that struct's own tags, so this asserts the count rather than the spelling: a renamed tag
// moves the reader and the declaration together, and there is no third statement to drift from.
func TestTheDeclaredKeysAreTheOnesTheReaderDecodes(t *testing.T) {
	// Walked against the registered set first, so a section added without a row fails here rather than
	// having its key count go unmeasured. The root section is the one absence, because it declares
	// against a schema whose squashed group counts as a single tagged field and the arithmetic below
	// would be wrong for it; a test of its own holds that section.
	counted := map[string]bool{RootSectionName: true}
	for _, tc := range declaredAgainst {
		counted[tc.section] = true
	}
	for _, name := range declaredSections() {
		if !counted[name] {
			t.Errorf("%s is registered and no row states how many of its struct's paths it leaves out, "+
				"so its key count is unmeasured", name)
		}
	}

	for _, tc := range declaredAgainst {
		registered, ok := registry.Lookup(tc.section)
		if !ok {
			t.Errorf("%s is not registered; Defects: %v", tc.section, registry.Defects())
			continue
		}
		tagged := taggedFields(reflect.TypeOf(tc.proto).Elem())
		if want := tagged - tc.exclude; len(registered.Keys) != want {
			t.Errorf("%s declares %d keys and the struct carries %d tagged fields with %d excluded, "+
				"so %d were expected", tc.section, len(registered.Keys), tagged, tc.exclude, want)
		}
		if len(registered.Excluded) != tc.exclude {
			t.Errorf("%s excludes %v and %d exclusions were expected",
				tc.section, registered.Excluded, tc.exclude)
		}
	}
}

// TestEachPeerExclusionStillHasItsReason holds every path this section leaves out to the fact that
// justified leaving it out.
//
// Every one has a different reason, so each is checked against the thing that would expire it rather
// than against the list. An exclusion whose reason has gone is a setting an operator can use that the
// key space refuses, and it fails silently: the key simply is not there.
//
// No count here. Two rounds of review found this doc naming a number the list beneath it had outgrown,
// so the list is the only statement of how many there are.
func TestEachPeerExclusionStillHasItsReason(t *testing.T) {
	registered, ok := registry.Lookup(P2PSectionName)
	if !ok {
		t.Fatalf("%s is not registered; Defects: %v", P2PSectionName, registry.Defects())
	}
	for _, rel := range []string{filledFromTheCommandLine, derivedFromTheConnectionLimit, readByNothing} {
		if !slices.Contains(registered.Excluded, P2PSectionName+"."+rel) {
			t.Errorf("excluded is %v and does not name %s", registered.Excluded, rel)
		}
	}

	// The root directory: excluded because the command line carries it after the file is read, so the
	// file never states it. Measured, because a doc claiming each reason is checked and then checking
	// two of three is how the last one of these went stale.
	if generatedFileCarries(t, filledFromTheCommandLine) {
		t.Errorf("%s is excluded because no file states it and a generated file now does, so an "+
			"operator writes it and it belongs declared", filledFromTheCommandLine)
	}

	// The outbound ceiling: a pointer the node's defaults leave unset, where unset is the setting,
	// because the node derives a ceiling from the total limit instead. A default here would be invented.
	if got := tmcfg.DefaultP2PConfig().MaxOutboundConnections; got != nil {
		t.Errorf("the node now defaults the outbound ceiling to %v, so it states a value and belongs "+
			"declared rather than excluded", *got)
	}

	// The two a cluster patches in are declared, not excluded, because a node run by hand sets both and
	// its configuration file accepts both. Held here so a later round does not quietly refuse them: the
	// symptom would be an operator told that the address their node advertises reaches nothing.
	declared := map[string]bool{}
	for _, key := range registered.Keys {
		declared[key] = true
	}
	for _, rel := range []string{"external-address", "persistent-peers"} {
		if !declared[P2PSectionName+"."+rel] {
			t.Errorf("%s.%s is a setting an operator running a node by hand writes and the section does "+
				"not declare it", P2PSectionName, rel)
		}
	}

	// The dial hook: excluded because nothing reads it and no generated file carries it. The second
	// half is the measurable one, and it is the half that would expire first, since the node wiring the
	// field up would start writing it and the key would then be one an operator's file holds and this
	// space refuses.
	if generatedFileCarries(t, readByNothing) {
		t.Errorf("%s is excluded and a generated file now carries it, so it is a setting an operator "+
			"writes and belongs declared", readByNothing)
	}
}

// generatedFileCarries reports whether the file the node writes for itself holds a key.
//
// Rendered through the node's own writer rather than matched against the template's source, so what is
// measured is what an operator's file actually contains.
func generatedFileCarries(t *testing.T, rel string) bool {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("make a home to render into: %v", err)
	}
	if err := tmcfg.WriteConfigFile(home, tmcfg.DefaultConfig()); err != nil {
		t.Fatalf("render a node configuration file: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	if err != nil {
		t.Fatalf("read the rendered file: %v", err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if name, _, found := strings.Cut(line, "="); found && strings.TrimSpace(name) == rel {
			return true
		}
	}
	return false
}

// taggedFields counts the fields of a struct that carry a mapstructure name, following the same rules the
// registry derives keys by: a squashed field contributes its own, and a dash or a remaining field none.
func taggedFields(t reflect.Type) int {
	n := 0
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag, ok := f.Tag.Lookup("mapstructure")
		if !ok || f.PkgPath != "" {
			continue
		}
		parts := strings.Split(tag, ",")
		opts := parts[1:]
		if hasOpt(opts, "remain") || parts[0] == "-" {
			continue
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if hasOpt(opts, "squash") {
			n += taggedFields(ft)
			continue
		}
		if ft.Kind() == reflect.Struct && ft.String() != "time.Time" && ft.String() != "big.Int" {
			n += taggedFields(ft)
			continue
		}
		n++
	}
	return n
}

func hasOpt(opts []string, want string) bool {
	for _, o := range opts {
		if o == want {
			return true
		}
	}
	return false
}

// warningCannotName are the removed settings the reader's own deprecation check does not report.
//
// Seven are durations or booleans, where a written zero and an unwritten field hold the same value, so
// the check has nothing to test. The eighth is a pointer the check could name and does not. Recorded so
// that making the check complete fails here rather than leaving a sentence quietly stale.
var warningCannotName = map[string]bool{
	"unsafe-overrides-enabled":              true,
	"unsafe-propose-timeout-override":       true,
	"unsafe-propose-timeout-delta-override": true,
	"unsafe-vote-timeout-override":          true,
	"unsafe-vote-timeout-delta-override":    true,
	"unsafe-commit-timeout-override":        true,
	"unsafe-bypass-commit-timeout-override": true,
	"stateless-leader-election":             true,
}

// TestTheExcludedConsensusPathsAreTheRemovedOnes ties the exclusion list to the struct's own marking.
//
// Each excluded path has to name a field the struct itself marks as deprecated, and no declared path may.
// The struct is the authority rather than the deprecation warning, because that warning is incomplete: it
// names eight of these sixteen.
//
// Nothing in the binary calls the warning either, so an operator who still has one of these in their file
// gets no error and no warning and the value is quietly ignored. That is what the exclusion is carrying. It
// keeps the key out of the new format rather than relying on a diagnostic that never runs.
func TestTheExcludedConsensusPathsAreTheRemovedOnes(t *testing.T) {
	registered, ok := registry.Lookup(ConsensusSectionName)
	if !ok {
		t.Fatalf("%s is not registered; Defects: %v", ConsensusSectionName, registry.Defects())
	}
	marked := deprecatedPaths(t, reflect.TypeOf(tmcfg.ConsensusConfig{}))

	excluded := map[string]bool{}
	for _, key := range registered.Excluded {
		excluded[key] = true
	}
	if want := len(removedSettings) + 1; len(excluded) != want {
		t.Errorf("the section excludes %d paths and %d were expected, being the removed settings and the "+
			"root directory", len(excluded), want)
	}
	for _, rel := range removedSettings {
		key := ConsensusSectionName + "." + rel
		if !excluded[key] {
			t.Errorf("%s is listed as removed and the section does not exclude it", key)
		}
		if !marked[rel] {
			t.Errorf("%s is excluded as a removed setting and the struct does not mark its field "+
				"deprecated, so it is a setting an operator can use and belongs declared", key)
		}
		delete(marked, rel)
	}
	for rel := range marked {
		t.Errorf("%s.%s names a field the struct marks deprecated and the section declares it",
			ConsensusSectionName, rel)
	}

	for _, key := range registered.Keys {
		rel := strings.TrimPrefix(key, ConsensusSectionName+".")
		if err := writtenThenChecked(t, rel); err != nil {
			t.Errorf("%s is declared and the deprecation warning names it: %v", key, err)
		}
	}
}

// TestTheDeprecationWarningReachesTheRecordedSubset measures the gap in the reader's own check.
//
// Eight of the sixteen removed settings make the warning name them and eight cannot, so an operator who
// wrote one of those seven would get nothing back even from a caller that ran the check. Held so that
// making the check complete shows up as a failure rather than as a sentence going quietly stale.
func TestTheDeprecationWarningReachesTheRecordedSubset(t *testing.T) {
	for _, rel := range removedSettings {
		err := writtenThenChecked(t, rel)
		switch {
		case warningCannotName[rel] && err != nil:
			t.Errorf("the warning now names %s, so it reaches one more removed setting and the row "+
				"should go", rel)
		case !warningCannotName[rel] && err == nil:
			t.Errorf("the warning no longer names %s, so one more removed setting is now silent", rel)
		}
	}
}

// deprecatedPaths returns the mapstructure names of the fields a struct marks deprecated.
func deprecatedPaths(t *testing.T, typ reflect.Type) map[string]bool {
	t.Helper()
	marked, ok := deprecatedFields(t)[typ.Name()]
	if !ok {
		t.Fatalf("%s was not found in the node's configuration source, so nothing measures which of "+
			"its fields are deprecated", typ.Name())
	}
	// Copied, because the source is parsed once for the whole package and one caller strikes rows off
	// what it is handed as it checks them. Sharing the map left a later caller reading an emptied set,
	// so the deprecation checks passed or failed on the order they ran in.
	out := make(map[string]bool, len(marked))
	for key := range marked {
		out[key] = true
	}
	return out
}

// deprecatedFields reports, per struct name, the mapstructure key of every field the node marks
// deprecated.
//
// Read from the source rather than through reflection, because the node marks a field two ways and
// reflection can only see one of them. Fifteen consensus fields carry a Deprecated prefix on the name,
// which a tag walk finds. Seven more across four structs are marked the standard way, by a comment
// above the field, which no tag carries. A predicate that saw only the names reported those
// seven as live settings, so each was declared as a key an operator could set and none of them does
// anything.
//
// Parsed once and cached, because every section that asks pays for the whole package otherwise.
func deprecatedFields(t *testing.T) map[string]map[string]bool {
	t.Helper()
	deprecatedOnce.Do(func() {
		deprecatedByStruct, deprecatedErr = parseDeprecatedFields(nodeConfigSourceDir)
	})
	if deprecatedErr != nil {
		t.Fatalf("read the node's configuration source: %v", deprecatedErr)
	}
	return deprecatedByStruct
}

// nodeConfigSourceDir is the package whose structs these sections declare against.
//
// A relative path because it is the same module, so it is checked out beside this one and moving either
// is a change to both.
const nodeConfigSourceDir = "../../sei-tendermint/config"

var (
	deprecatedOnce     sync.Once
	deprecatedByStruct map[string]map[string]bool
	deprecatedErr      error
)

// parseDeprecatedFields walks a package's source for struct fields marked deprecated.
//
// A field counts as marked if its name carries the prefix the consensus settings use, or if the comment
// above it opens the standard deprecation note. Only a field with a mapstructure tag is reported, since a
// field with no tag declares no key for a section to exclude.
//
// Three rules keep the answer the same on every run. Only the directory's own package is read, because a
// test package sits in the same directory and a struct there may share a name with a real one. Only a
// declaration at the top level of a file is read, because a struct declared inside a function may share a
// name too. And a name arriving twice is an error rather than an overwrite, because the two would
// otherwise race and whichever landed last would decide what the section is allowed to declare. Without
// these the same input gave four different answers across forty calls, and the failure was a check that
// passed while reading an empty set.
func parseDeprecatedFields(dir string) (map[string]map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	out := map[string]map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(file.Name.Name, "_test") {
			continue
		}
		for _, decl := range file.Decls {
			generic, ok := decl.(*ast.GenDecl)
			if !ok || generic.Tok != token.TYPE {
				continue
			}
			for _, spec := range generic.Specs {
				typed, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structType, ok := typed.Type.(*ast.StructType)
				if !ok {
					continue
				}
				if _, seen := out[typed.Name.Name]; seen {
					return nil, fmt.Errorf("%s declares a struct named %s that another file in %s also "+
						"declares, so which one answers depends on the order they are read",
						name, typed.Name.Name, dir)
				}
				out[typed.Name.Name] = markedFields(structType)
			}
		}
	}
	return out, nil
}

// markedFields returns the mapstructure keys of the fields a struct marks deprecated.
func markedFields(structType *ast.StructType) map[string]bool {
	marked := map[string]bool{}
	for _, field := range structType.Fields.List {
		if len(field.Names) != 1 || field.Tag == nil {
			continue
		}
		if !strings.HasPrefix(field.Names[0].Name, "Deprecated") && !isDeprecationNote(field.Doc) {
			continue
		}
		tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
		if key, ok := tag.Lookup("mapstructure"); ok {
			marked[strings.Split(key, ",")[0]] = true
		}
	}
	return marked
}

// isDeprecationNote reports whether a field's comment opens the standard deprecation note.
//
// The note has to begin a line, which is the convention Go states and tools rely on. Matched anywhere in
// the comment, a field whose documentation merely points at another field's deprecation came back marked,
// and the cheapest way to silence that failure is to stop declaring a live key.
func isDeprecationNote(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, line := range doc.List {
		if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(line.Text, "//")), "Deprecated:") {
			return true
		}
	}
	return false
}

// writtenThenChecked writes one consensus path into a configuration and returns what the reader's
// deprecation warning says about it.
//
// Written and decoded rather than assigned, because a removed setting is detected by its field being
// non-nil after a decode, and assigning it directly would skip the step under test.
func writtenThenChecked(t *testing.T, rel string) error {
	t.Helper()
	conf := tmcfg.DefaultConfig()
	v := viper.New()
	v.SetConfigType("toml")
	body := "[consensus]\n" + rel + " = " + probeValueFor(t, reflect.TypeOf(tmcfg.ConsensusConfig{}), rel) + "\n"
	if err := v.ReadConfig(strings.NewReader(body)); err != nil {
		t.Fatalf("compose a file setting %s: %v", rel, err)
	}
	// Fatal rather than skipped. The probe value comes from the field's own type, so a refusal here means
	// the derivation is wrong, and a skip inside this loop ends every remaining row while reporting a
	// pass.
	if err := v.Unmarshal(conf); err != nil {
		t.Fatalf("%s does not decode from a value derived from its own field: %v", rel, err)
	}
	return conf.DeprecatedFieldWarning()
}

// probeValueFor renders a value the field behind a key accepts, taken from that field's own type.
//
// Derived rather than matched on the name, because a name says nothing about a shape. The double-sign
// height is an integer whose name begins like the boolean overrides do, and it decoded only because the
// decoder accepts a boolean where an integer belongs. A shape the decoder refuses used to end the loop
// through a skip, so every row after it went unmeasured and the run still read as a pass.
func probeValueFor(t *testing.T, typ reflect.Type, rel string) string {
	t.Helper()
	field, ok := fieldTagged(typ, rel)
	if !ok {
		t.Fatalf("%s names no field of %s, so no value can be derived for it", rel, typ.Name())
	}
	ft := field.Type
	for ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	if ft == reflect.TypeOf(time.Duration(0)) {
		return "\"1s\""
	}
	switch ft.Kind() {
	case reflect.Interface:
		// The removed settings are each a pointer to an empty interface, which is how the reader tells a
		// written value from an absent one. Any shape decodes, so the value only has to be present.
		return "\"1s\""
	case reflect.Bool:
		return "true"
	case reflect.String:
		return "\"probe\""
	case reflect.Float32, reflect.Float64:
		return "1.0"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "1"
	case reflect.Slice:
		return "[]"
	default:
		t.Fatalf("%s is a %s and no probe value is derived for that shape", rel, ft.Kind())
		return ""
	}
}

// fieldTagged returns the field of a struct whose mapstructure tag names a key.
func fieldTagged(typ reflect.Type, rel string) (reflect.StructField, bool) {
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if tag, ok := f.Tag.Lookup("mapstructure"); ok && strings.Split(tag, ",")[0] == rel {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// TestTheStateSyncKeysAreDeclaredAsASet holds the section to the set an operator fills together.
//
// Turning state sync on means writing every one of these, so a key this space refuses is one an operator
// writes and is told nothing reads. The snapshot servers were left out on the reasoning that a key with
// no default cannot be declared, and three siblings in the same section disprove it: the trust height,
// the trust hash and the scratch directory each state a zero value and are declared.
//
// The generated file writes four of the five. It leaves the scratch directory out and says so in words,
// naming it among the keys it omits on purpose while still parsing them if set, which is the whole of the
// evidence that an operator writes these by hand.
//
// Both halves of that reasoning are measured below rather than stated: that each key the operator
// supplies states nothing, and that the template writes four of the five.
func TestTheStateSyncKeysAreDeclaredAsASet(t *testing.T) {
	registered, ok := registry.Lookup(StateSyncSectionName)
	if !ok {
		t.Fatalf("%s is not registered; Defects: %v", StateSyncSectionName, registry.Defects())
	}
	if len(registered.Excluded) != 0 {
		t.Errorf("the section excludes %v, and every path it carries is one an operator writes",
			registered.Excluded)
	}
	declared := map[string]bool{}
	for _, key := range registered.Keys {
		declared[key] = true
	}
	for _, rel := range []string{"rpc-servers", "trust-height", "trust-hash", "temp-dir", "use-p2p"} {
		if !declared[StateSyncSectionName+"."+rel] {
			t.Errorf("%s.%s is one of the keys an operator fills to turn state sync on and the section "+
				"does not declare it", StateSyncSectionName, rel)
		}
	}

	// Each of the four the operator supplies states nothing, which is what makes the set coherent: none
	// of these is a value the binary can know. Asserted per key rather than for the servers alone,
	// because a default arriving for any of them would leave this section declaring a value it invented
	// while the sentence above still called them the operator's own.
	live := tmcfg.DefaultStateSyncConfig()
	for name, stated := range map[string]bool{
		"rpc-servers":  len(live.RPCServers) != 0,
		"trust-height": live.TrustHeight != 0,
		"trust-hash":   live.TrustHash != "",
		"temp-dir":     live.TempDir != "",
	} {
		if stated {
			t.Errorf("the node now ships a value for %s.%s, so it is no longer a setting only an "+
				"operator can supply and the reasoning for declaring the set has changed",
				StateSyncSectionName, name)
		}
	}

	// Four of the five reach a generated file. The scratch directory does not, and the template names it
	// among the keys it leaves out on purpose while still parsing them if set, which is the whole of the
	// evidence that these are written by hand. Measured, because that split is what the paragraph above
	// rests on.
	for rel, written := range map[string]bool{
		"enable": true, "use-p2p": true, "rpc-servers": true, "trust-height": true, "trust-hash": true,
		"temp-dir": false,
	} {
		if got := generatedFileCarries(t, rel); got != written {
			t.Errorf("a generated file carries %s.%s = %v and %v was expected, so the split between "+
				"what the template writes and what an operator adds has moved",
				StateSyncSectionName, rel, got, written)
		}
	}
}

// TestEverySectionThisPackageRegistersIsUsable is the check no single section here can make.
//
// A registration the registry cannot use is recorded rather than panicked, so a section that failed to
// register is absent rather than loud, and two of the refusals depend on what else has registered. Nothing
// is enumerated beyond the section names this package owns, so adding one is covered by adding it there.
func TestEverySectionThisPackageRegistersIsUsable(t *testing.T) {
	for _, name := range declaredSections() {
		registered, ok := registry.Lookup(name)
		if !ok {
			t.Errorf("%s is not registered; Defects: %v", name, registry.Defects())
			continue
		}
		if len(registered.Keys) == 0 {
			t.Errorf("%s registered and declares no key", name)
		}
	}
	for _, d := range registry.Defects() {
		t.Errorf("the registry refused %s: %v", d.Section, d.Err)
	}
}

// TestTheRootSchemaCarriesWhatTheNodesOwnTypeCarries closes the one place a spelling is restated.
//
// The root section declares against a schema rather than the node's top-level type, because that type
// carries the nine tables as well and declaring against it would declare their keys twice. The schema
// squashes the same base group, so fourteen spellings still derive from the node's own tags, and it
// restates two fields by hand. Nine of those spellings become keys, the rest being left out. This holds those two to the type they came from: name, tag and type each, and the count of
// non-table fields, so a third one appearing there fails here rather than going undeclared.
func TestTheRootSchemaCarriesWhatTheNodesOwnTypeCarries(t *testing.T) {
	live := reflect.TypeOf(tmcfg.Config{})
	schema := reflect.TypeOf(nodeRootSchema{})

	var restated int
	for i := 0; i < live.NumField(); i++ {
		f := live.Field(i)
		tag, ok := f.Tag.Lookup("mapstructure")
		if !ok {
			continue
		}
		// The squashed base group and the nine tables are not restated; everything else is.
		if strings.Contains(tag, "squash") {
			continue
		}
		if f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct {
			continue
		}
		restated++

		got, found := schema.FieldByName(f.Name)
		if !found {
			t.Errorf("the node's type carries %s (%s) at its root and the schema does not, so the key is "+
				"not declared at all", f.Name, tag)
			continue
		}
		if want := got.Tag.Get("mapstructure"); want != tag {
			t.Errorf("%s is tagged %q on the node's type and %q here, so the declared key is not the one "+
				"the reader decodes", f.Name, tag, want)
		}
		if got.Type != f.Type {
			t.Errorf("%s is a %s on the node's type and a %s here, so the declared value has a shape the "+
				"reader does not read", f.Name, f.Type, got.Type)
		}
	}

	// The schema holds the squashed group plus exactly the restated fields, so a field added here that the
	// node's type does not carry fails too.
	if want := restated + 1; schema.NumField() != want {
		t.Errorf("the schema carries %d fields and the node's type has %d root fields beside the squashed "+
			"group, so %d were expected", schema.NumField(), restated, want)
	}
}

// TestTheRootPathsLeftOutAreTheOnesTheFileAlreadyStates names why two root paths are not declared.
//
// The home directory is where this file is found, so a value inside it would be the file naming its own
// location, and the command line already carries it. The node mode is the fact the file states at the top
// under its own name, and a second spelling would let the two disagree: the resolution answers for one and
// the node reads the other.
//
// Written out here rather than read from the list the registration uses. Comparing that list against itself
// agrees however it changes, so a path dropped from it would leave this passing while the key became
// declared.
func TestTheRootPathsLeftOutAreTheOnesTheFileAlreadyStates(t *testing.T) {
	registered, ok := registry.Lookup(RootSectionName)
	if !ok {
		t.Fatalf("%s is not registered; Defects: %v", RootSectionName, registry.Defects())
	}
	declared := map[string]bool{}
	for _, key := range registered.Keys {
		declared[key] = true
	}
	for key, why := range map[string]string{
		"home":         "the file's own location, which the command line carries",
		"mode":         "the fact the file states at the top under its own name",
		"proxy-app":    "the address of an out-of-process application the node no longer runs",
		"abci":         "the transport to that application",
		"filter-peers": "peer filtering through that application",
	} {
		if declared[key] {
			t.Errorf("%q is declared at the root and it is %s, so the key space offers a setting that "+
				"is either already settled or reaches nothing", key, why)
		}
	}
	if want := len(notWritableInThisFile) + len(removedFromTheNode); len(registered.Excluded) != want {
		t.Errorf("the root section excludes %v and %d paths were expected, being the two the file settles "+
			"elsewhere and the three the node removed", registered.Excluded, want)
	}
}

// TestNoSectionDeclaresTheRootDirectory covers a field six of these sections carry.
//
// Each holds a root directory tagged the same as the key at the top of the file, and the node fills every
// one from the command line after the file is read. So each states the empty string, and a delivery that
// wrote a declared value would blank the root a running node found its data, its genesis file and its
// signing key under.
//
// Checked across every registered section rather than the five, so a section added later that carries the
// same field fails here instead of shipping the same hole.
func TestNoSectionDeclaresTheRootDirectory(t *testing.T) {
	for _, s := range registry.Sections() {
		for _, key := range s.Keys {
			if key == "home" || strings.HasSuffix(key, ".home") {
				t.Errorf("%s declares %q, and the node fills that field from the command line after the "+
					"file is read, so what this section states for it is the empty string", s.Name, key)
			}
		}
	}
}

// TestWhatTheGeneratorFillsIsNotWhatTheDeclarationStates names the writer for each key on the list.
//
// A declared value is what a generated file carries, and these are the keys where that is not true
// because the init command sets them after the pipeline forMode mirrors.
//
// Each listed key is checked both ways: it is declared, so it is not quietly excluded, and what makes
// its declared value wrong still makes it wrong. A key that stops diverging fails, so a row cannot go
// stale while reading as a live exception.
//
// What this does not measure is the other direction. Nothing enumerates the values that command sets
// after the pipeline, so a key joining this class and never added to the list is invisible here.
// Closing that needs a reader over the command itself, which is a different mechanism from this one;
// until it exists the list is maintained by review and this holds only what is on it.
func TestWhatTheGeneratorFillsIsNotWhatTheDeclarationStates(t *testing.T) {
	// What makes each declaration something other than what a generated file carries, checked rather
	// than described. A row that stops holding means the key no longer belongs on the list.
	wrongBecause := map[string]func(*testing.T, any){
		P2PSectionName + ".bootstrap-peers": func(t *testing.T, declared any) {
			// The command fills these from the chain identifier, after the pipeline forMode mirrors.
			// Read from the generator's own source, so a change there moves this.
			supplied := seeds.BootstrapPeers("pacific-1")
			if supplied == "" {
				t.Error("the generator ships no seeds for a public network, so nothing fills this key " +
					"and the declared empty value is what a file carries after all")
				return
			}
			if fmt.Sprint(declared) == supplied {
				t.Errorf("declared as %q, which is what the generator supplies, so this no longer "+
					"diverges", declared)
			}
		},
		"moniker": func(t *testing.T, declared any) {
			// The command takes the node name as a required argument, so a generated file always
			// carries an operator's own. What the declaration answers instead is a fact about the
			// machine that asked, which is why no value keyed on mode could be right.
			host, err := os.Hostname()
			if err != nil {
				t.Skipf("this host has no name to compare against: %v", err)
			}
			if fmt.Sprint(declared) != host {
				t.Errorf("declared as %q where this host is %q, so the declaration no longer answers a "+
					"host fact and this key may belong off the list", declared, host)
			}
		},
	}

	resolved, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, key := range filledByTheGenerator {
		check, named := wrongBecause[key]
		if !named {
			t.Errorf("%s is on the list and nothing says what makes its declared value wrong, so the "+
				"row records no divergence", key)
			continue
		}
		declared, declares := resolved.Values[key]
		if !declares {
			t.Errorf("%s is on the list and no section declares it; a key the generated file carries "+
				"belongs in the key space", key)
			continue
		}
		t.Run(key, func(t *testing.T) { check(t, declared) })
	}
	if len(wrongBecause) != len(filledByTheGenerator) {
		t.Errorf("%d reasons are stated and %d keys are listed", len(wrongBecause), len(filledByTheGenerator))
	}
}

// TestTheExcludedMempoolPathsReachNothing holds the reason those three are left out.
//
// Each is excluded because no code reads it, which is a claim about the whole binary rather than about a
// marking, so no check on how a field is documented can reach it. One of the three is not marked
// deprecated at all and never will be caught that way.
//
// What this drives is one of the two paths a written value takes: the conversion into the mempool's own
// configuration. The other is the transaction reactor, which reads several settings straight off this
// struct, and it cannot be reached from here because it is an internal package. So a value routed into
// the channel the reactor sizes would leave the conversion untouched and this test passing, which is the
// gap to know about: the criterion is that no code reads these, and this holds the larger half of it.
//
// Measured by setting each to a value nothing would choose and converting: a field the conversion started
// carrying would change the result, and the exclusion would have become a key an operator writes whose
// value stops arriving, with nothing failing. Compared against a conversion of the defaults rather than
// against a written-out expectation, so a field added to the conversion moves both sides and only these
// three decide the outcome.
func TestTheExcludedMempoolPathsReachNothing(t *testing.T) {
	for _, rel := range neverReachTheMempool {
		t.Run(rel, func(t *testing.T) {
			untouched := tmcfg.DefaultMempoolConfig().ToMempoolConfig()

			written := tmcfg.DefaultMempoolConfig()
			field, ok := fieldTagged(reflect.TypeOf(tmcfg.MempoolConfig{}), rel)
			if !ok {
				t.Fatalf("%s names no field of the mempool configuration", rel)
			}
			set := reflect.ValueOf(written).Elem().FieldByName(field.Name)
			switch set.Kind() {
			case reflect.Int, reflect.Int64:
				set.SetInt(set.Int() + 9999)
			default:
				t.Fatalf("%s is a %s and this does not know how to write one", rel, set.Kind())
			}

			if got := written.ToMempoolConfig(); !reflect.DeepEqual(got, untouched) {
				t.Errorf("writing %s changed what the running mempool receives, so the conversion now "+
					"carries it and the key belongs declared rather than excluded", rel)
			}

		})
	}
}
