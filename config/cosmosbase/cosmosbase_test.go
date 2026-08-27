package cosmosbase

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
)

// requireDeclares holds one section's declared keys against the keys named for it.
func requireDeclares(t *testing.T, section string, reads []string) registry.Section {
	t.Helper()
	registered, ok := registry.Lookup(section)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", section)
	}
	want := append([]string(nil), reads...)
	sort.Strings(want)
	if !reflect.DeepEqual(registered.Keys, want) {
		t.Errorf("%s declares\n  %v\nand its reader resolves\n  %v", section, registered.Keys, want)
	}
	return registered
}

// TestTheNodeWideKeysAreTheOnesTheirReaderResolves holds the root section against the server's constants.
//
// Fourteen keys and not one of them carries a segment in front. The reader looks these up by the constants
// below, so a prefix here would declare fourteen keys no operator writes and leave the real ones
// undeclared.
func TestTheNodeWideKeysAreTheOnesTheirReaderResolves(t *testing.T) {
	section := requireDeclares(t, BaseSectionName, []string{
		server.FlagMinGasPrices, server.FlagPruning, server.FlagPruningKeepRecent,
		server.FlagPruningKeepEvery, server.FlagPruningInterval, server.FlagHaltHeight,
		server.FlagFreezeHeight, server.FlagHaltTime, server.FlagMinRetainBlocks,
		server.FlagInterBlockCache, server.FlagIndexEvents, server.FlagCompactionInterval,
		server.FlagConcurrencyWorkers, baseapp.FlagOccEnabled,
	})
	if section.Prefix != "" {
		t.Errorf("the section carries prefix %q, and one here renames every key it declares", section.Prefix)
	}
}

// TestTheSnapshotKeysAreTheOnesTheirReaderResolves holds the snapshot section against the server's
// constants.
func TestTheSnapshotKeysAreTheOnesTheirReaderResolves(t *testing.T) {
	requireDeclares(t, StateSyncSectionName, []string{
		server.FlagStateSyncSnapshotInterval,
		server.FlagStateSyncSnapshotKeepRecent,
		server.FlagStateSyncSnapshotDir,
	})
}

// TestTheRESTKeysAreTheOnesItsReaderResolves holds the REST section against the keys its reader looks up.
//
// Written out rather than taken from constants, because this reader has none: it looks each key up as a
// literal string where it reads it. That is the whole reason a comparison is worth making here.
func TestTheRESTKeysAreTheOnesItsReaderResolves(t *testing.T) {
	requireDeclares(t, APISectionName, []string{
		"api.enable", "api.swagger", "api.enabled-unsafe-cors", "api.address",
		"api.max-open-connections", "api.rpc-read-timeout", "api.rpc-write-timeout",
		"api.rpc-max-body-bytes",
	})
}

// TestTheGRPCKeysAreTheOnesItsReaderResolves holds the gRPC section against the keys its reader looks up.
//
// Written out for the same reason as the REST section: the reader has no constants for these.
func TestTheGRPCKeysAreTheOnesItsReaderResolves(t *testing.T) {
	requireDeclares(t, GRPCSectionName, []string{
		"grpc.enable", "grpc.address", "grpc.max-recv-msg-size", "grpc.max-open-connections",
		"grpc.max-connection-idle", "grpc.max-connection-age", "grpc.max-connection-age-grace",
		"grpc.keepalive-time", "grpc.keepalive-timeout", "grpc.keepalive-min-time",
		"grpc.keepalive-permit-without-stream",
	})
}

// TestTheMetricKeysAreTheOnesItsReaderResolves holds the metric section against the keys its reader looks
// up, the label set among them.
func TestTheMetricKeysAreTheOnesItsReaderResolves(t *testing.T) {
	requireDeclares(t, TelemetrySectionName, []string{
		"telemetry.service-name", "telemetry.enabled", "telemetry.enable-hostname",
		"telemetry.enable-hostname-label", "telemetry.enable-service-label",
		"telemetry.prometheus-retention-time", globalLabelsKey,
	})
}

// TestTheMetricSchemaRestatesTheUpstreamTypeExactlyOnceOver is what a schema costs.
//
// The schema exists for one field's shape, so every other field has to be the upstream field: same name,
// same tag, same type. A field that drifted would declare a key under a spelling the reader does not look
// up, or resolve a value of a type it cannot take, and the section would go on registering cleanly either
// way.
func TestTheMetricSchemaRestatesTheUpstreamTypeExactlyOnceOver(t *testing.T) {
	upstream := reflect.TypeOf(telemetry.Config{})
	schema := reflect.TypeOf(telemetrySchema{})
	if schema.NumField() != upstream.NumField() {
		t.Fatalf("the schema has %d fields and the upstream type has %d; a field on one side only is "+
			"either a key nothing reads or a setting nothing declares",
			schema.NumField(), upstream.NumField())
	}

	differing := 0
	for i := range schema.NumField() {
		got, want := schema.Field(i), upstream.Field(i)
		if got.Name != want.Name {
			t.Errorf("field %d is %s here and %s upstream", i, got.Name, want.Name)
			continue
		}
		if got.Tag != want.Tag {
			t.Errorf("%s is tagged %q here and %q upstream, so it declares a key the reader does not "+
				"look up", got.Name, got.Tag, want.Tag)
		}
		if got.Type == want.Type {
			continue
		}
		differing++
		if got.Name != "GlobalLabels" {
			t.Errorf("%s is %s here and %s upstream. The label set is the only field whose shape this "+
				"schema changes, so a second one is a divergence nothing decided",
				got.Name, got.Type, want.Type)
		}
	}
	if differing != 1 {
		t.Errorf("%d fields differ in type, want exactly one. If the upstream type came to match, this "+
			"schema is a restatement with nothing left to justify it", differing)
	}
}

// TestTheUpstreamDefaultCarriesNoLabels holds the assumption the declared label set is built on.
//
// The declared default is an empty list of rows, which is right only while the upstream default holds no
// labels. A default that gained a pair would need converting into the untyped rows the reader takes, and
// without that it reaches the reader as the shape it refuses.
func TestTheUpstreamDefaultCarriesNoLabels(t *testing.T) {
	if got := srvconfig.DefaultConfig().Telemetry.GlobalLabels; len(got) != 0 {
		t.Errorf("the upstream default carries %d label rows: %v. They need converting into untyped rows "+
			"here, because the reader asserts that shape rather than casting what it finds", len(got), got)
	}
}

// TestTheLabelSetIsRefusedFromTheEnvironment covers the one key no variable here can supply.
//
// Its reader asserts a list of untyped rows and an environment carries one string, so resolving the
// variable installs a value the reader refuses, and it refuses in the first statement of the whole server
// configuration. The node stops. Leaving the channel out means the file's value applies and the node runs.
func TestTheLabelSetIsRefusedFromTheEnvironment(t *testing.T) {
	resolved, err := registry.Resolve(registry.ModeValidator, registry.Sources{
		LookupEnv: func(name string) (string, bool) {
			if name == registry.EnvName(globalLabelsKey) {
				return "chain_id=pacific-1", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	reason, reported := resolved.Ignored[globalLabelsKey]
	if !reported {
		t.Errorf("a variable was set for %s and nothing reports that it did nothing. An operator whose "+
			"variable is ignored has to be told", globalLabelsKey)
	}
	if reported && reason == "" {
		t.Errorf("%s is reported as ignored and carries no reason. The report is the only place an "+
			"operator learns their variable did nothing, and without a reason it does not tell them "+
			"which channel to use instead", globalLabelsKey)
	}
	if got := resolved.Values[globalLabelsKey]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("%s resolved to %#v (%T), want the declared default it was left to",
			globalLabelsKey, got, got)
	}
	for _, key := range resolved.Overrides {
		if key == globalLabelsKey {
			t.Errorf("%s is reported as a value an operator supplied, and the variable did nothing",
				globalLabelsKey)
		}
	}
}

// TestEachKindOfNodeResolvesTheInterfacesItIsFor is the mode-varying part of these sections.
//
// Three settings differ by kind of node, and the values are written out here rather than taken from the
// same rules the sections read, so a change to those rules fails this and gets looked at. Each matters in a
// different direction. A node that serves queries needs the two interfaces that serve them, and declaring
// them closed would take a service away from one. A validator is meant to expose as little as it can, and
// declaring gRPC open would state the opposite of that on every validator. And how many blocks a node
// keeps is a decision about its disk.
func TestEachKindOfNodeResolvesTheInterfacesItIsFor(t *testing.T) {
	byMode := map[registry.Mode]struct {
		api, grpc bool
		retain    uint64
	}{
		registry.ModeValidator: {api: false, grpc: false, retain: 0},
		registry.ModeSeed:      {api: false, grpc: false, retain: 0},
		registry.ModeFull:      {api: true, grpc: true, retain: 100000},
		registry.ModeArchive:   {api: true, grpc: true, retain: 0},
	}
	for _, mode := range registry.Modes() {
		want, named := byMode[mode]
		if !named {
			t.Fatalf("mode %q has no expectation here, so a mode was added and this was not revisited", mode)
		}
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		for key, expected := range map[string]any{
			"api.enable":        want.api,
			"grpc.enable":       want.grpc,
			"min-retain-blocks": want.retain,
		} {
			if got := resolved.Values[key]; !reflect.DeepEqual(got, expected) {
				t.Errorf("mode %q: %s resolves to %#v, want %#v", mode, key, got, expected)
			}
		}
	}
}

// TestDefaultsAreTheUpstreamOnesApartFromTheModeRules covers everything a mode does not change.
//
// Compared against the upstream defaults with the same mode rules applied, so this holds the sections to
// carrying the whole of that configuration rather than a subset of it, and the three settings the rules
// touch are pinned by name above.
func TestDefaultsAreTheUpstreamOnesApartFromTheModeRules(t *testing.T) {
	for _, mode := range registry.Modes() {
		live := srvconfig.DefaultConfig()
		params.SetAppConfigByMode(live, params.NodeMode(mode))
		for _, c := range []struct {
			section string
			got     any
			want    any
		}{
			{BaseSectionName, baseDefaults(mode), live.BaseConfig},
			{APISectionName, apiDefaults(mode), live.API},
			{GRPCSectionName, grpcDefaults(mode), live.GRPC},
			{StateSyncSectionName, stateSyncDefaults(mode), live.StateSync},
		} {
			if !reflect.DeepEqual(c.got, c.want) {
				t.Errorf("mode %q: %s resolves to something other than that mode's upstream configuration",
					mode, c.section)
			}
		}

		if _, ok := telemetryDefaults(mode).(telemetrySchema); !ok {
			t.Fatalf("mode %q: the metric defaults returned %T, want the schema", mode, telemetryDefaults(mode))
		}
		// Every field the schema copies by hand, held against the upstream value, and held as the
		// resolved key rather than as a struct field. The section that has to restate its values is the
		// one where a field can be assigned from the wrong neighbour, and a struct comparison would not
		// see it: each field still holds a value, and the count still matches.
		requireResolvesTelemetry(t, mode, live.Telemetry)
	}
}

// requireResolvesTelemetry holds every key the metric schema declares against the upstream value.
func requireResolvesTelemetry(t *testing.T, mode registry.Mode, live telemetry.Config) {
	t.Helper()
	resolved, err := registry.Resolve(mode, registry.Sources{})
	if err != nil {
		t.Fatalf("mode %q: %v", mode, err)
	}
	for key, want := range map[string]any{
		"telemetry.service-name":              live.ServiceName,
		"telemetry.enabled":                   live.Enabled,
		"telemetry.enable-hostname":           live.EnableHostname,
		"telemetry.enable-hostname-label":     live.EnableHostnameLabel,
		"telemetry.enable-service-label":      live.EnableServiceLabel,
		"telemetry.prometheus-retention-time": live.PrometheusRetentionTime,
		globalLabelsKey:                       []any{},
	} {
		if got := resolved.Values[key]; !reflect.DeepEqual(got, want) {
			t.Errorf("mode %q: %s resolves to %#v (%T), want %#v (%T)", mode, key, got, got, want, want)
		}
	}
}

// TestTheSectionsThisPackageRegistersAreUsable covers what the registry refuses.
//
// Scoped to the five names this file registers. A refusal that depends on what else has registered is
// not this package's to answer for, and the sweep that covers it belongs where every section is linked.
func TestTheSectionsThisPackageRegistersAreUsable(t *testing.T) {
	mine := map[string]bool{
		BaseSectionName: true, APISectionName: true, GRPCSectionName: true,
		TelemetrySectionName: true, StateSyncSectionName: true,
	}
	for _, defect := range registry.Defects() {
		if mine[defect.Section] {
			t.Errorf("%s was refused, so none of its keys is declared: %v", defect.Section, defect.Err)
		}
	}
}
