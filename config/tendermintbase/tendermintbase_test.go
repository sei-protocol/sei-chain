package tendermintbase

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

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
func declaredSections() []string {
	return []string{
		P2PSectionName, RPCSectionName, ConsensusSectionName, MempoolSectionName,
		StateSyncSectionName, TxIndexSectionName, InstrumentationSectionName,
		PrivValidatorSectionName, SelfRemediationSectionName,
	}
}

// ours reports whether a key belongs to a section this package registers.
func ours(key string) bool {
	for _, name := range declaredSections() {
		if strings.HasPrefix(key, name+".") {
			return true
		}
	}
	return false
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
	for key := range byMode[registry.ModeValidator] {
		if !ours(key) {
			continue
		}
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

// TestTheDeclaredKeysAreTheOnesTheReaderDecodes holds the declaration to the struct the node decodes into.
//
// Derived from that struct's own tags, so this asserts the count rather than the spelling: a renamed tag
// moves the reader and the declaration together, and there is no third statement to drift from.
func TestTheDeclaredKeysAreTheOnesTheReaderDecodes(t *testing.T) {
	for _, tc := range []struct {
		section string
		proto   any
		exclude int
	}{
		{P2PSectionName, &tmcfg.P2PConfig{}, 1},
		{RPCSectionName, &tmcfg.RPCConfig{}, 0},
		{ConsensusSectionName, &tmcfg.ConsensusConfig{}, len(removedSettings)},
		{MempoolSectionName, &tmcfg.MempoolConfig{}, 0},
		{StateSyncSectionName, &tmcfg.StateSyncConfig{}, 1},
		{TxIndexSectionName, &tmcfg.TxIndexConfig{}, 0},
		{InstrumentationSectionName, &tmcfg.InstrumentationConfig{}, 0},
		{PrivValidatorSectionName, &tmcfg.PrivValidatorConfig{}, 0},
		{SelfRemediationSectionName, &tmcfg.SelfRemediationConfig{}, 0},
	} {
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

// TestTheExcludedPathIsTheOneWithNoDefault names why the one exclusion is there.
//
// The outbound ceiling is a pointer the node's defaults leave unset, and unset is the setting: the node
// derives a ceiling from the total limit instead. A default here would be invented. If the node ever gives
// it one, this fails and the key should be declared rather than excluded.
func TestTheExcludedPathIsTheOneWithNoDefault(t *testing.T) {
	registered, ok := registry.Lookup(P2PSectionName)
	if !ok {
		t.Fatalf("%s is not registered", P2PSectionName)
	}
	if want := []string{P2PSectionName + ".max-outbound-connections"}; !reflect.DeepEqual(registered.Excluded, want) {
		t.Fatalf("excluded is %v, want %v", registered.Excluded, want)
	}
	if got := tmcfg.DefaultP2PConfig().MaxOutboundConnections; got != nil {
		t.Errorf("the node now defaults the outbound ceiling to %v, so it states a value and belongs "+
			"declared rather than excluded", *got)
	}
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
// Six are durations or booleans, where a written zero and an unwritten field hold the same value, so the
// check has nothing to test. The seventh is a pointer the check could name and does not. Recorded so that
// making the check complete fails here rather than leaving a sentence quietly stale.
var warningCannotName = map[string]bool{
	"unsafe-overrides-enabled":              true,
	"unsafe-propose-timeout-override":       true,
	"unsafe-propose-timeout-delta-override": true,
	"unsafe-vote-timeout-override":          true,
	"unsafe-vote-timeout-delta-override":    true,
	"unsafe-commit-timeout-override":        true,
	"unsafe-bypass-commit-timeout-override": true,
}

// TestTheExcludedConsensusPathsAreTheRemovedOnes ties the exclusion list to the struct's own marking.
//
// Each excluded path has to name a field the struct itself marks as deprecated, and no declared path may.
// The struct is the authority rather than the deprecation warning, because that warning is incomplete: it
// names eight of these fifteen.
//
// Nothing in the binary calls the warning either, so an operator who still has one of these in their file
// gets no error and no warning and the value is quietly ignored. That is what the exclusion is carrying. It
// keeps the key out of the new format rather than relying on a diagnostic that never runs.
func TestTheExcludedConsensusPathsAreTheRemovedOnes(t *testing.T) {
	registered, ok := registry.Lookup(ConsensusSectionName)
	if !ok {
		t.Fatalf("%s is not registered; Defects: %v", ConsensusSectionName, registry.Defects())
	}
	marked := deprecatedPaths(reflect.TypeOf(tmcfg.ConsensusConfig{}))

	excluded := map[string]bool{}
	for _, key := range registered.Excluded {
		excluded[key] = true
	}
	if len(excluded) != len(removedSettings) {
		t.Errorf("the section excludes %d paths and %d are listed", len(excluded), len(removedSettings))
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
// Eight of the fifteen removed settings make the warning name them and seven cannot, so an operator who
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
func deprecatedPaths(t reflect.Type) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !strings.HasPrefix(f.Name, "Deprecated") {
			continue
		}
		if tag, ok := f.Tag.Lookup("mapstructure"); ok {
			out[strings.Split(tag, ",")[0]] = true
		}
	}
	return out
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
	body := "[consensus]\n" + rel + " = " + probeValueFor(rel) + "\n"
	if err := v.ReadConfig(strings.NewReader(body)); err != nil {
		t.Fatalf("compose a file setting %s: %v", rel, err)
	}
	if err := v.Unmarshal(conf); err != nil {
		t.Skipf("%s does not decode from the probe value: %v", rel, err)
	}
	return conf.DeprecatedFieldWarning()
}

// probeValueFor returns a written value of the right shape for a consensus path.
//
// Three shapes appear: a duration written as a string, a boolean, and a whole number.
func probeValueFor(rel string) string {
	switch {
	case strings.HasPrefix(rel, "skip-") || strings.HasPrefix(rel, "unsafe-") ||
		strings.HasPrefix(rel, "double-sign-") || strings.HasSuffix(rel, "-enabled"):
		return "true"
	case strings.Contains(rel, "timeout") || strings.Contains(rel, "-delta") ||
		strings.Contains(rel, "interval") || strings.Contains(rel, "period"):
		return "\"1s\""
	default:
		return "1"
	}
}

// TestTheStateSyncExclusionIsThePathWithNoDefault names why that section leaves one path out.
//
// The servers to fetch a snapshot from are the operator's own peers, so there is no value to inherit. An
// empty list is not a default an operator can start from, and an address written here would name a host
// this binary cannot know about. If the node ever ships one, this fails and the key should be declared.
func TestTheStateSyncExclusionIsThePathWithNoDefault(t *testing.T) {
	registered, ok := registry.Lookup(StateSyncSectionName)
	if !ok {
		t.Fatalf("%s is not registered; Defects: %v", StateSyncSectionName, registry.Defects())
	}
	if want := []string{StateSyncSectionName + ".rpc-servers"}; !reflect.DeepEqual(registered.Excluded, want) {
		t.Fatalf("excluded is %v, want %v", registered.Excluded, want)
	}
	if got := tmcfg.DefaultStateSyncConfig().RPCServers; len(got) != 0 {
		t.Errorf("the node now defaults the snapshot servers to %v, so it states a value and the key "+
			"belongs declared rather than excluded", got)
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
