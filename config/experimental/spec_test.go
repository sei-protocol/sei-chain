package experimental_test

import (
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/experimental"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
)

// PR3 of the ConfigManager stack: experimental configuration semantics.
//
// Gate 5, that these semantics run under both configuration managers, needs the command tree
// and lives in cmd/seid/cmd/configmanager/experimental_spec_test.go.

// FlatView is documented as matching servertypes.AppOptions structurally. Asserted at compile
// time, because every test here passes a *viper.Viper and none of them would notice the two
// drifting apart.
var _ experimental.FlatView = servertypes.AppOptions(nil)

// Keys declared for these gates, at package scope, which is the shape a real caller uses.
var (
	workers = experimental.Int("configspec.workers", 8, experimental.WithOwner("configtest"))
	label   = experimental.String("configspec.label", "unset", experimental.WithOwner("configtest"))
	toggle  = experimental.Bool("configspec.toggle", true, experimental.WithOwner("configtest"))
	unowned = experimental.Int("configspec.unowned", 1)
)

// written returns a source carrying keys under the experimental section, as an operator's file
// would after the handler merged it.
func written(kv map[string]any) *viper.Viper {
	v := viper.New()
	for k, val := range kv {
		v.Set(experimental.Section+"."+k, val)
	}
	return v
}

// TestGate1ADeclaredKeyReadsTypedWithItsDefault is the whole cost of shipping an experimental
// value: declaring it.
//
// Asserted on all three types with defaults that are not their type's zero, since a default of
// 0, "" or false would pass against an implementation that ignored the declaration.
func TestGate1ADeclaredKeyReadsTypedWithItsDefault(t *testing.T) {
	empty := viper.New()
	if got := workers.Get(empty); got != 8 {
		t.Errorf("an absent int key read %d, want its declared default 8", got)
	}
	if got := label.Get(empty); got != "unset" {
		t.Errorf("an absent string key read %q, want its declared default %q", got, "unset")
	}
	if got := toggle.Get(empty); !got {
		t.Error("an absent bool key read false, want its declared default true")
	}

	v := written(map[string]any{"configspec.workers": "16", "configspec.label": "prod", "configspec.toggle": "false"})
	if got := workers.Get(v); got != 16 {
		t.Errorf("a written int key read %d, want 16. TOML carries this as a string, so the declared "+
			"type has to absorb it", got)
	}
	if got := label.Get(v); got != "prod" {
		t.Errorf("a written string key read %q, want prod", got)
	}
	if got := toggle.Get(v); got {
		t.Error("a written bool key read true, want the written false")
	}
}

// TestGate2AnUndeclaredKeyIsReportedAndLeftInPlace is the load-bearing gate for rollback.
//
// Held in both directions: the finding fires, and the key still reads back off the source, which
// is what "left in place" has to mean for a rollback to recover the value.
func TestGate2AnUndeclaredKeyIsReportedAndLeftInPlace(t *testing.T) {
	const path = experimental.Section + ".configspec.from_a_later_release"
	v := written(map[string]any{"configspec.from_a_later_release": "42", "configspec.workers": "16"})

	findings := experimental.Check(v)
	undeclared := experimental.Undeclared(findings)
	if len(undeclared) != 1 || undeclared[0] != path {
		t.Fatalf("Check reported %v as undeclared, want exactly [%s]", undeclared, path)
	}
	for _, f := range findings {
		if f.Unrecognized && f.Err != nil {
			t.Errorf("%s is both undeclared and carries an error, so a caller that halts on errors "+
				"would refuse a boot over a key written for a later release", f.Path)
		}
	}
	if got := v.Get(path); got != "42" {
		t.Errorf("the undeclared key reads back as %#v after the check, want 42. Reporting must not "+
			"consume it, or a rollback to the release that declares it loses the value", got)
	}
	if got := workers.Get(v); got != 16 {
		t.Errorf("a declared key beside an undeclared one read %d, want 16", got)
	}
}

// TestGate2TheSectionWrittenAsAScalarIsReported covers the shape that silently disables every
// declared key.
//
// A scalar at the section path shadows the table, so every declared key under it resolves to its
// default. Without this the operator gets no signal at all.
func TestGate2TheSectionWrittenAsAScalarIsReported(t *testing.T) {
	v := viper.New()
	v.Set(experimental.Section, "true")

	if got := experimental.Undeclared(experimental.Check(v)); len(got) != 1 {
		t.Errorf("the section written as a scalar produced %v, want one finding. Every declared key "+
			"is shadowed in that shape and reads its default, so silence is the wrong answer", got)
	}
}

// TestGate3ADeclaredKeyIsTypeCheckedWhereverItsValueCameFrom is the root-cause gate.
//
// The check pass walks what this binary declares and asks the source for each key, rather than
// walking what the source enumerates. That direction is the whole fix: a value can reach
// Handle.Get through a channel the source does not enumerate, and a pass driven by the written
// set cannot see it. Both channels are driven here.
func TestGate3ADeclaredKeyIsTypeCheckedWhereverItsValueCameFrom(t *testing.T) {
	t.Run("from a file", func(t *testing.T) {
		v := written(map[string]any{"configspec.workers": "not-a-number"})

		invalid := experimental.Invalid(experimental.Check(v))
		if len(invalid) != 1 {
			t.Fatalf("Check reported %d invalid values for a declared int set to a non-number, want 1", len(invalid))
		}
		f := invalid[0]
		if !strings.Contains(f.Path, "configspec.workers") {
			t.Errorf("the finding names %q rather than configspec.workers", f.Path)
		}
		if f.Owner != "configtest" {
			t.Errorf("the finding reports owner %q, want configtest; a bad value needs someone to ask", f.Owner)
		}
		if f.Kind != "int" {
			t.Errorf("the finding reports declared_type %q, want int; an operator cannot tell what "+
				"shape was expected without it", f.Kind)
		}
		if got := workers.Get(v); got != 8 {
			t.Errorf("a key with an unconvertible value read %d, want the declared default 8", got)
		}
	})

	// The channel that had no coverage, and the reason this gate exists. A source with
	// AutomaticEnv resolves an environment value for a key it never enumerates, so AllKeys()
	// cannot see it and only a declared-set walk can.
	t.Run("from the environment", func(t *testing.T) {
		t.Setenv("CONFIGSPEC_EXPERIMENTAL_CONFIGSPEC_WORKERS", "not-a-number")
		v := viper.New()
		v.SetEnvPrefix("configspec")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
		v.AutomaticEnv()

		if got := v.Get(workers.Path()); got != "not-a-number" {
			t.Fatalf("the fixture did not put the value on the environment channel: Get returned %#v. "+
				"Without that this gate is vacuous", got)
		}
		if keys := v.AllKeys(); len(keys) != 0 {
			t.Fatalf("AllKeys() returned %v, so the environment value is enumerable and this gate no "+
				"longer covers the channel it exists for", keys)
		}

		invalid := experimental.Invalid(experimental.Check(v))
		if len(invalid) != 1 {
			t.Fatalf("Check reported %d invalid values for an environment-supplied non-number, want 1. "+
				"An unenumerable channel still feeds Handle.Get, so a pass that cannot see it makes "+
				"Get's fall back to a default a silent substitution", len(invalid))
		}
		if got := workers.Get(v); got != 8 {
			t.Errorf("the environment value read %d, want the declared default 8", got)
		}
	})
}

// TestGate3BlankingANonStringKeyIsReported closes the coercion cast performs silently.
//
// cast reads "" as zero with no error, so a blanked number or boolean would pin the key to zero
// rather than its declared default. sei-config rejects the same shape one layer down for the
// same reason. An empty string on a string key is a legitimate value and stays one.
func TestGate3BlankingANonStringKeyIsReported(t *testing.T) {
	for _, tc := range []struct {
		name    string
		key     string
		wantErr bool
		check   func(*testing.T, *viper.Viper)
	}{
		{"int", workers.Key(), true, func(t *testing.T, v *viper.Viper) {
			if got := workers.Get(v); got != 8 {
				t.Errorf("a blanked int read %d, want the declared default 8. Zero commonly means "+
					"unlimited or disabled, so pinning it silently is the damaging direction", got)
			}
		}},
		{"bool", toggle.Key(), true, func(t *testing.T, v *viper.Viper) {
			if got := toggle.Get(v); !got {
				t.Error("a blanked bool read false, want the declared default true")
			}
		}},
		{"string", label.Key(), false, func(t *testing.T, v *viper.Viper) {
			if got := label.Get(v); got != "" {
				t.Errorf("a blanked string read %q, want the empty string an operator wrote. An empty "+
					"string is a legitimate value for a string key", got)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := written(map[string]any{tc.key: ""})

			invalid := experimental.Invalid(experimental.Check(v))
			if got := len(invalid) > 0; got != tc.wantErr {
				t.Errorf("blanking %s produced %d findings, wantErr=%v", tc.key, len(invalid), tc.wantErr)
			}
			tc.check(t, v)
		})
	}
}

// TestGate3AMixedCaseDeclarationPanicsAtRegistration is the choke-point guard.
//
// A configuration source enumerates in lower case and resolves case-insensitively, so a
// mixed-case declaration would register under one spelling and be enumerated under another: an
// operator would be told no binary declares their key while its value was discarded. The guard
// lives in the one function every declaration passes through, so a second entry point cannot
// forget it.
func TestGate3AMixedCaseDeclarationPanicsAtRegistration(t *testing.T) {
	for _, key := range []string{"configspec.MixedCase", "Configspec.upper", "configspec.END"} {
		t.Run(key, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("declaring %q did not panic. It would register under that spelling and be "+
						"enumerated lower-cased, so an operator who wrote it is told no binary declares "+
						"it while its value is discarded", key)
				}
				if msg, _ := r.(string); !strings.Contains(msg, "lower case") {
					t.Errorf("the panic was %v, which does not say what is wrong with the key", r)
				}
			}()
			experimental.Int(key, 1, experimental.WithOwner("configtest"))
		})
	}
}

// TestGate3EveryDeclaredKeyRoundTripsThroughLookup guards every future declaration, rather than
// the three cases above.
//
// For each declared key, the identity the registry holds has to be the identity the check pass
// derives from a source. One test then covers a declaration nobody has written yet.
func TestGate3EveryDeclaredKeyRoundTripsThroughLookup(t *testing.T) {
	keys := experimental.Keys()
	if len(keys) == 0 {
		t.Fatal("no keys are declared, so this would hold for a registry that dropped every declaration")
	}
	for _, key := range keys {
		d, ok := experimental.Lookup(key)
		if !ok {
			t.Errorf("%s is enumerated but does not resolve, so the registry disagrees with itself", key)
			continue
		}
		// The path a source enumerates, lowered, is what the check pass slices a key out of.
		derived := strings.TrimPrefix(strings.ToLower(d.Path()), experimental.Section+".")
		if _, ok := experimental.Lookup(derived); !ok {
			t.Errorf("%s resolves at %q, and the key derived from that path (%q) does not resolve. The "+
				"check pass would report this declared key as undeclared", key, d.Path(), derived)
		}
	}
}

// TestGate4ExperimentalKeysCannotBeReachedByAStableRegistryWalk is why an experimental key can
// change shape in a patch release.
//
// The fingerprint lands in PR4; this asserts the structural property that keeps these keys out
// of it. Every declared path is namespaced under the experimental section, and no stable section
// struct is named for that section, so a walk over section structs cannot produce one.
func TestGate4ExperimentalKeysCannotBeReachedByAStableRegistryWalk(t *testing.T) {
	prefix := experimental.Section + "."
	for _, key := range experimental.Keys() {
		d, _ := experimental.Lookup(key)
		if !strings.HasPrefix(d.Path(), prefix) {
			t.Errorf("%s resolves at %q, outside the %q section, so a stable registry walking section "+
				"structs could produce this path and put the key in the fingerprint", key, d.Path(), experimental.Section)
		}
		if strings.HasPrefix(key, prefix) {
			t.Errorf("%s carries the section in its own identity. Promotion moves the declaration "+
				"between registries and must not rename the key", key)
		}
	}
	if _, ok := experimental.Lookup("configspec.never-declared"); ok {
		t.Error("Lookup answered for a key nothing declared, so an undeclared key would never be reported")
	}
}

// TestGate4AnUnownedKeyStillRegistersAndReportsSo keeps the owner requirement honest. An unowned
// key is still better registered than absent, and reports as unknown so it is not read as owned
// by whoever looks last.
func TestGate4AnUnownedKeyStillRegistersAndReportsSo(t *testing.T) {
	d, ok := experimental.Lookup(unowned.Key())
	if !ok {
		t.Fatal("a declaration without an owner did not register")
	}
	if d.Owner() != "unknown" {
		t.Errorf("an unowned key reports owner %q, want unknown", d.Owner())
	}
}
