package registry_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// bounded is a section with rules its tags cannot express.
//
// An enum's member set and a number's range are facts about the setting, not about its type, so nothing
// derived from a struct tag can check them and the section is the only place that knows.
type bounded struct {
	WriteMode string `mapstructure:"write_mode"`
	Workers   int    `mapstructure:"workers"`
}

// Validate states this section's own rules.
func (b bounded) Validate() error {
	switch b.WriteMode {
	case "sync", "async":
	default:
		return fmt.Errorf("write_mode is %q, want sync or async", b.WriteMode)
	}
	if b.Workers < 1 || b.Workers > 64 {
		return fmt.Errorf("workers is %d, want between 1 and 64", b.Workers)
	}
	return nil
}

// unruled is a section stating no rules, which is most of them.
type unruled struct {
	Anything string `mapstructure:"anything"`
}

// registerBounded registers the section with rules, at a baseline that satisfies them.
func registerBounded(t *testing.T) {
	t.Helper()
	registry.Reset()
	registry.RegisterSection("bounded", &bounded{}, func(registry.Mode) any {
		return bounded{WriteMode: "sync", Workers: 8}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering bounded produced a defect: %v", d.Err)
	}
}

// TestASectionJudgesItsOwnResolvedValues is the check a struct tag cannot make.
//
// A tag says a value is a string; only the section knows which strings are members of the enum. The same
// for a number and its range. Without this the file parses, the type is right, and the node refuses the
// value at its next start.
func TestASectionJudgesItsOwnResolvedValues(t *testing.T) {
	registerBounded(t)

	for _, tc := range []struct {
		name    string
		written map[string]any
		refused bool
		says    string
	}{
		{"the baseline alone", nil, false, ""},
		{"a member of the enum", map[string]any{"bounded.write_mode": "async"}, false, ""},
		{"outside the enum", map[string]any{"bounded.write_mode": "backwards"}, true, "write_mode"},
		{"inside the range", map[string]any{"bounded.workers": 64}, false, ""},
		{"above the range", map[string]any{"bounded.workers": 65}, true, "workers"},
		{"below the range", map[string]any{"bounded.workers": 0}, true, "workers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := registry.Resolve(registry.ModeValidator, registry.FileLayer(tc.written))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			refusals := registry.ValidateResolved(resolved)

			if tc.refused && len(refusals) == 0 {
				t.Fatalf("%s was accepted. The file parses and the type is right, so nothing else would "+
					"catch it before the node refused the value at its next start", tc.name)
			}
			if !tc.refused && len(refusals) != 0 {
				t.Fatalf("%s was refused: %v", tc.name, refusals)
			}
			if tc.refused {
				if refusals[0].Section != "bounded" {
					t.Errorf("the refusal names section %q, want bounded", refusals[0].Section)
				}
				if !strings.Contains(refusals[0].Error(), tc.says) {
					t.Errorf("the refusal does not name %q: %v", tc.says, refusals[0])
				}
			}
		})
	}
}

// TestARuleSeesTheKeysTheOperatorLeftAlone is why this runs over the resolved values.
//
// A rule can span two keys. Handed only what a file writes, a section would see a zero for every key the
// operator did not mention and refuse a configuration that is entirely correct.
func TestARuleSeesTheKeysTheOperatorLeftAlone(t *testing.T) {
	registerBounded(t)
	// Only one of the two keys is written. The other has to arrive from the baseline, or the range check
	// sees a zero and refuses.
	resolved, err := registry.Resolve(registry.ModeValidator,
		registry.FileLayer(map[string]any{"bounded.write_mode": "async"}))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if refusals := registry.ValidateResolved(resolved); len(refusals) != 0 {
		t.Errorf("a file writing one of two keys was refused: %v.\n\nThe unwritten key resolves to its "+
			"baseline, so a section handed only written values would see a zero and refuse a correct "+
			"configuration", refusals)
	}
}

// TestASectionWithNoRulesIsNotAsked keeps declaring a section free.
//
// Most sections have nothing to say beyond their types. If a section without a Validate method were
// treated as failing, or as passing something, declaring one would cost a decision nobody needs to make.
func TestASectionWithNoRulesIsNotAsked(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("unruled", &unruled{}, func(registry.Mode) any {
		return unruled{Anything: "at all"}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering unruled produced a defect: %v", d.Err)
	}

	section, ok := registry.Lookup("unruled")
	if !ok {
		t.Fatal("unruled did not register")
	}
	if section.Validate != nil {
		t.Error("a section whose type states no rules was given a validator, so it now has a decision " +
			"to make that it never asked for")
	}

	resolved, err := registry.Resolve(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if refusals := registry.ValidateResolved(resolved); len(refusals) != 0 {
		t.Errorf("a section with no rules refused something: %v", refusals)
	}
}

// TestARuleIsFoundOnTheTypeRatherThanDeclared is what keeps it from being forgotten.
//
// The section is not asked to register its rules separately. A type that grows a Validate method is
// checked from then on, so the failure mode where the rules exist and nothing runs them cannot happen.
func TestARuleIsFoundOnTheTypeRatherThanDeclared(t *testing.T) {
	registerBounded(t)

	section, ok := registry.Lookup("bounded")
	if !ok {
		t.Fatal("bounded did not register")
	}
	if section.Validate == nil {
		t.Fatal("a section whose type has a Validate method was registered without one, so its rules " +
			"exist and nothing runs them")
	}
	// Driven directly, so this measures the wiring rather than what Resolve happens to produce.
	if err := section.Validate(map[string]any{"write_mode": "sync", "workers": 8}); err != nil {
		t.Errorf("values inside the rules were refused: %v", err)
	}
	if err := section.Validate(map[string]any{"write_mode": "sideways", "workers": 8}); err == nil {
		t.Error("a value outside the enum was accepted through the registered validator")
	}
}

// TestANestedFieldReachesItsRule holds the shape a flat key space has to be turned back into.
//
// Keys arrive flat because that is how a configuration source carries them, and a section with a nested
// field needs them nested again to be read. Without that the nested field stays at its zero value and a
// rule about it is checked against something the operator never wrote.
func TestANestedFieldReachesItsRule(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("outer", &nested{}, func(registry.Mode) any {
		return nested{Inner: inner{Limit: 5}}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering outer produced a defect: %v", d.Err)
	}

	section, _ := registry.Lookup("outer")
	if section.Validate == nil {
		t.Fatal("the section states a rule and was registered without a validator")
	}

	if err := section.Validate(map[string]any{"inner.limit": 5}); err != nil {
		t.Errorf("a nested value inside its rule was refused: %v", err)
	}
	if err := section.Validate(map[string]any{"inner.limit": 99}); err == nil {
		t.Error("a nested value outside its rule was accepted, so the flat key never reached the " +
			"nested field and the rule was checked against a zero")
	}
}

// nested has a rule about a field one level down.
type nested struct {
	Inner inner `mapstructure:"inner"`
}

type inner struct {
	Limit int `mapstructure:"limit"`
}

// Validate states the nested rule.
func (n nested) Validate() error {
	if n.Inner.Limit > 10 {
		return fmt.Errorf("inner.limit is %d, want 10 or less", n.Inner.Limit)
	}
	return nil
}
