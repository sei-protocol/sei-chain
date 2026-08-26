package registry_test

import (
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// nodeWide is a probe for the settings written at the top of a file rather than inside a table.
type nodeWide struct {
	Pruning     string `mapstructure:"pruning"`
	HaltHeight  uint64 `mapstructure:"halt-height"`
	Concurrency int    `mapstructure:"concurrency-workers"`
}

// TestARootSectionDeclaresKeysWithNoPrefix is the whole of what registering root keys adds.
//
// Some settings are node-wide and are written at the top of a file. Giving them a section would rename
// them, and a renamed key is one an operator's existing file no longer reaches.
func TestARootSectionDeclaresKeysWithNoPrefix(t *testing.T) {
	registry.Reset()
	registry.RegisterRootKeys("base", &nodeWide{}, func(registry.Mode) any {
		return nodeWide{Pruning: "nothing", Concurrency: 4}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering root keys was refused: %v", d.Err)
	}

	section, ok := registry.Lookup("base")
	if !ok {
		t.Fatal("the section did not register under its name, so nothing can look it up or report on it")
	}
	if section.Prefix != "" {
		t.Errorf("the section carries prefix %q, and one here renames every key it declares", section.Prefix)
	}
	if got := strings.Join(section.Keys, ","); got != "concurrency-workers,halt-height,pruning" {
		t.Errorf("derived %q, want the three keys with no prefix. A leading segment is a key no operator "+
			"writes", got)
	}

	// The default has to render under the same prefix-free names, or a declared key states no value and
	// the resolution is refused rather than short.
	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := resolved.Values["pruning"]; got != "nothing" {
		t.Errorf("pruning resolved to %#v, want %q", got, "nothing")
	}
}

// TestTwoSectionsCannotDeclareTheSameKey was impossible while every key carried its section's name.
//
// Two prefixes cannot collide. Two root sections can, and the default rendered for such a key would be
// whichever section the walk reached last. Refused by the environment check, which two identical keys
// reach by answering to one variable, and named as the one key it is rather than as two spellings.
func TestTwoSectionsCannotDeclareTheSameKey(t *testing.T) {
	registry.Reset()
	same := func(name string) {
		registry.RegisterRootKeys(name, &struct {
			Pruning string `mapstructure:"pruning"`
		}{}, func(registry.Mode) any {
			return struct {
				Pruning string `mapstructure:"pruning"`
			}{Pruning: "nothing"}
		})
	}
	same("base")
	same("other")

	if _, ok := registry.Lookup("other"); ok {
		t.Fatal("both sections declared the same key. One default renders over the other and which one " +
			"wins depends on the order the sections are walked, so the value a node runs is not decided " +
			"by anything an operator or a reviewer can see")
	}
	defects := registry.Defects()
	if len(defects) != 1 {
		t.Fatalf("recorded %d defects, want one", len(defects))
	}
	// Named as one key two sections declare rather than as two spellings of one variable, which is the
	// reason the same check gives for the collision it was written for.
	if got := defects[0].Err.Error(); !strings.Contains(got, "is declared by two sections") {
		t.Errorf("the refusal reads %q, and an identical key is not an environment spelling collision", got)
	}
}

// TestAKeyTheEnvironmentCannotDeliverIsLeftToTheOtherSources is what refusing a channel buys.
//
// The environment carries one string per name. A reader taking its value's exact type cannot be handed
// one, so resolving the variable installs a value that stops the node. Skipping it means the file's value
// applies and the node runs.
func TestAKeyTheEnvironmentCannotDeliverIsLeftToTheOtherSources(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &struct {
		Rows  []any  `mapstructure:"rows"`
		Plain string `mapstructure:"plain"`
	}{}, func(registry.Mode) any {
		return struct {
			Rows  []any  `mapstructure:"rows"`
			Plain string `mapstructure:"plain"`
		}{Rows: []any{}, Plain: "from the default"}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("the registration was refused: %v", d.Err)
	}

	// Both variables are set. Only the one the environment can carry is allowed to answer.
	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{
		LookupEnv: func(name string) (string, bool) {
			switch name {
			case "SEID_PROBE_ROWS":
				return "chain_id=pacific-1", true
			case "SEID_PROBE_PLAIN":
				return "from the environment", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if got := resolved.Values["probe.rows"]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("probe.rows resolved to %#v (%T), want the default it was left to. Its reader takes a "+
			"list of rows, so installing the environment's string stops the node", got, got)
	}
	if got := resolved.Values["probe.plain"]; got != "from the environment" {
		t.Errorf("probe.plain resolved to %#v; refusing one key's channel closed another's", got)
	}
	sort.Strings(resolved.Overrides)
	if got := strings.Join(resolved.Overrides, ","); got != "probe.plain" {
		t.Errorf("overrides are %q, want only probe.plain. A key nothing supplied is not one an operator "+
			"has taken responsibility for", got)
	}
}

// TestAModeThisBinaryDoesNotDeclareIsRefused closes a resolution that answered for anything.
//
// A section's defaults answer per mode, and a mode this package does not know reaches whatever each
// section does with an argument it cannot match. Nothing about that is a decision anyone made: the mode
// rules these sections read answer for an unrecognised mode as though it were a full node, so an empty
// string, a capitalised name or one with a trailing space resolved the interfaces a full node serves onto
// whichever node asked.
func TestAModeThisBinaryDoesNotDeclareIsRefused(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &struct {
		Serves bool `mapstructure:"serves"`
	}{}, func(mode registry.Mode) any {
		return struct {
			Serves bool `mapstructure:"serves"`
		}{Serves: mode == registry.ModeFull || mode == registry.ModeArchive}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("the probe was refused: %v", d.Err)
	}

	for _, mode := range registry.Modes() {
		if _, err := registry.Resolve(mode, registry.Sources{}); err != nil {
			t.Errorf("mode %q is declared and did not resolve: %v", mode, err)
		}
	}
	for _, mode := range []registry.Mode{"", "Validator", "validator ", "VALIDATOR", "sentry"} {
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err == nil {
			t.Errorf("mode %q resolved, to serves=%v. A mode nothing declares has no answer, and the one "+
				"it reached is whatever the rules do with an argument they cannot match",
				mode, resolved.Values["probe.serves"])
		}
	}
}

// TestAVariableSetForARefusedKeyIsReported is what makes the required reason worth requiring.
//
// The channel is skipped and the value discarded, which is the point. But an operator who set the variable
// believes otherwise, and a reason nothing can attach to their own action is a reason nobody is told. So
// the variable is still read, and the key comes back named.
func TestAVariableSetForARefusedKeyIsReported(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &struct {
		Rows  []any  `mapstructure:"rows"`
		Plain string `mapstructure:"plain"`
	}{}, func(registry.Mode) any {
		return struct {
			Rows  []any  `mapstructure:"rows"`
			Plain string `mapstructure:"plain"`
		}{Rows: []any{}, Plain: "from the default"}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("the probe was refused: %v", d.Err)
	}

	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{
		LookupEnv: func(name string) (string, bool) {
			if name == registry.EnvName("probe.rows") {
				return "chain_id=pacific-1", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if got := strings.Join(resolved.Ignored, ","); got != "probe.rows" {
		t.Errorf("the ignored variables are %q, want probe.rows. An operator set it and nothing here can "+
			"tell them it did nothing", got)
	}
	if !reflect.DeepEqual(resolved.Values["probe.rows"], []any{}) {
		t.Errorf("probe.rows resolved to %#v, and the channel was supposed to be skipped",
			resolved.Values["probe.rows"])
	}

	// A refused key nobody set is not news, so it is not reported.
	quiet, err := registry.Resolve(registry.ModeFull, registry.Sources{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(quiet.Ignored) != 0 {
		t.Errorf("a refused key nobody set is reported as ignored: %v", quiet.Ignored)
	}
}

// TestTheEnvironmentCarriesAListOfWordsAndNotAListOfLists is what makes the rule right rather than broad.
//
// A variable holds one string. That is a value for anything read as a single word or number, and by long
// convention a list of those written with commas between them. Nothing conventional puts a structure inside
// one variable.
//
// So the line is not "lists cannot come from the environment". A list of words can, and taking that channel
// away from one would be removing something operators use. A list whose elements are themselves lists, or
// are unconstrained, cannot: a comma-separated string does not become one, and a reader that asks for the
// exact shape gets a string and stops the node.
func TestTheEnvironmentCarriesAListOfWordsAndNotAListOfLists(t *testing.T) {
	type shapes struct {
		Words []string `mapstructure:"words"`
		Rows  []any    `mapstructure:"rows"`
		One   string   `mapstructure:"one"`
	}
	registry.Reset()
	registry.RegisterSection("shapes", &shapes{}, func(registry.Mode) any {
		return shapes{Words: []string{"a"}, Rows: []any{}, One: "from the default"}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("the registration was refused: %v", d.Err)
	}

	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{
		LookupEnv: func(name string) (string, bool) {
			switch name {
			case "SEID_SHAPES_WORDS":
				return "x,y", true
			case "SEID_SHAPES_ROWS":
				return "chain_id=pacific-1", true
			case "SEID_SHAPES_ONE":
				return "from the environment", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if got := resolved.Values["shapes.one"]; got != "from the environment" {
		t.Errorf("shapes.one is %#v; a single value is what a variable carries", got)
	}
	if got := resolved.Values["shapes.words"]; got != "x,y" {
		t.Errorf("shapes.words is %#v, want the variable's value. A list of words is what a variable "+
			"conventionally carries, and refusing it would take away a channel operators use", got)
	}
	if got := resolved.Values["shapes.rows"]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("shapes.rows is %#v, want the declared default it was left to. A variable holds one "+
			"string and this setting is a list of unconstrained elements, so the string would reach a "+
			"reader that asked for rows", got)
	}
	if !slices.Contains(resolved.Ignored, "shapes.rows") {
		t.Errorf("Ignored is %v and does not name shapes.rows. A variable was set for it and did "+
			"nothing, and an operator has to be told that", resolved.Ignored)
	}
	for _, key := range []string{"shapes.one", "shapes.words"} {
		if slices.Contains(resolved.Ignored, key) {
			t.Errorf("%s is reported as ignored and its variable answered", key)
		}
	}
}
