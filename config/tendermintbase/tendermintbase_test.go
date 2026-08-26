package tendermintbase

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/app/seeds"
	"github.com/sei-protocol/sei-chain/config/registry"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
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
		if !strings.HasPrefix(key, P2PSectionName+".") && !strings.HasPrefix(key, RPCSectionName+".") {
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
		{P2PSectionName, &tmcfg.P2PConfig{}, 3 + len(patchedByTheNodeController)},
		{RPCSectionName, &tmcfg.RPCConfig{}, 1},
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
	for _, rel := range append([]string{filledFromTheCommandLine, derivedFromTheConnectionLimit,
		readByNothing}, patchedByTheNodeController...) {
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

	// The two the controller patches: their reason is that something outside this binary writes them
	// after the file is written, which no test here can see. What is measured instead is that a
	// generated file does carry them, since a key the file did not write would need a different reason.
	for _, rel := range patchedByTheNodeController {
		if !generatedFileCarries(t, rel) {
			t.Errorf("%s is excluded as a path something outside the binary writes and a generated file "+
				"no longer carries it, so the reason for leaving it out has changed", rel)
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
	// The generator's own source for each key, read from it rather than restated, so a change there
	// moves this. A chain identifier is needed for the peer seeds because that is the input the mode
	// rules do not carry, and the public networks are the ones an operator runs.
	supplied := map[string]string{
		P2PSectionName + ".bootstrap-peers": seeds.BootstrapPeers("pacific-1"),
	}

	resolved, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, key := range filledByTheGenerator {
		want, named := supplied[key]
		if !named {
			t.Errorf("%s is on the list and no writer is named for it, so nothing measures that the "+
				"generator fills it", key)
			continue
		}
		declared, declares := resolved.Values[key]
		if !declares {
			t.Errorf("%s is on the list and no section declares it; a key the generated file carries "+
				"belongs in the key space", key)
			continue
		}
		if want == "" {
			t.Errorf("%s names a writer that supplies nothing, so the row records no divergence", key)
			continue
		}
		if fmt.Sprint(declared) == want {
			t.Errorf("%s is declared as %q and the generator supplies the same, so it no longer "+
				"diverges. Take it off the list, and off forMode's exception", declared, want)
		}
	}
	if len(supplied) != len(filledByTheGenerator) {
		t.Errorf("%d writers are named and %d keys are listed", len(supplied), len(filledByTheGenerator))
	}
}
