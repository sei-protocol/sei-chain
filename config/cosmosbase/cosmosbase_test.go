package cosmosbase

import (
	"reflect"
	"sort"
	"testing"

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
		"telemetry.prometheus-retention-time", GlobalLabelsKey,
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
	reason, refused := registry.EnvCannotDeliver()[GlobalLabelsKey]
	if !refused {
		t.Fatalf("%s is not refused from the environment, so a variable naming it resolves to a string "+
			"and installing that stops the node", GlobalLabelsKey)
	}
	if reason == "" {
		t.Error("the refusal carries no reason, so an operator whose variable is ignored cannot be told why")
	}

	resolved, err := registry.Resolve(registry.ModeValidator, registry.Sources{
		LookupEnv: func(name string) (string, bool) {
			if name == registry.EnvName(GlobalLabelsKey) {
				return "chain_id=pacific-1", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := resolved.Values[GlobalLabelsKey]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("%s resolved to %#v (%T), want the declared default it was left to",
			GlobalLabelsKey, got, got)
	}
	for _, key := range resolved.Overrides {
		if key == GlobalLabelsKey {
			t.Errorf("%s is reported as a value an operator supplied, and the variable did nothing",
				GlobalLabelsKey)
		}
	}
}

// TestDefaultsAreTheUpstreamOnesForEveryMode covers the value side of all five registrations.
//
// Unchanged by mode, which is the decision worth pinning. seid init writes three of these keys per mode,
// so a node it provisioned carries them as written values; these are what a node with nothing written
// runs.
func TestDefaultsAreTheUpstreamOnesForEveryMode(t *testing.T) {
	for _, mode := range registry.Modes() {
		live := srvconfig.DefaultConfig()
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
				t.Errorf("mode %q: %s resolves to something other than the upstream default", mode, c.section)
			}
		}

		metrics, ok := telemetryDefaults(mode).(telemetrySchema)
		if !ok {
			t.Fatalf("mode %q: the metric defaults returned %T, want the schema", mode, telemetryDefaults(mode))
		}
		if metrics.Enabled != live.Telemetry.Enabled ||
			metrics.PrometheusRetentionTime != live.Telemetry.PrometheusRetentionTime ||
			metrics.ServiceName != live.Telemetry.ServiceName {
			t.Errorf("mode %q: the metric defaults are not the upstream ones: %+v", mode, metrics)
		}
	}
}

// TestEverySectionHereRegistersCleanly covers what the registry itself refuses.
func TestEverySectionHereRegistersCleanly(t *testing.T) {
	for _, defect := range registry.Defects() {
		t.Errorf("%s is registered and defective: %v", defect.Section, defect.Err)
	}
}
