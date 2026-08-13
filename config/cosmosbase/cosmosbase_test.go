package cosmosbase_test

import (
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/config/cosmosbase"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/spf13/viper"
)

func TestTheRegisteredStateSyncBaselineIsWhatANodeRunsToday(t *testing.T) {
	section, ok := registry.Lookup(cosmosbase.StateSyncSectionName)
	if !ok {
		t.Fatalf("%s did not register, so nothing below measures anything", cosmosbase.StateSyncSectionName)
	}
	want := srvconfig.DefaultConfig().StateSync
	for _, mode := range registry.Modes() {
		got, isConfig := section.Defaults(mode).(srvconfig.StateSyncConfig)
		if !isConfig {
			t.Fatalf("the baseline for %q is %T, not the upstream type", mode, section.Defaults(mode))
		}
		if got != want {
			t.Errorf("the baseline for %q mode is %+v and the upstream default is %+v. Registering a "+
				"section must not change what a node runs, and a difference here changes how often a "+
				"node snapshots while reading as a refactor", mode, got, want)
		}
	}
}

// TestTheDerivedKeysAreTheKeysTheReadersResolve holds the spelling against the constants.
//
// This section needs no schema written for it, unlike the two SeiDB sections, because the upstream type's
// mapstructure tags already name the keys the readers look up. Held rather than assumed: if those tags
// ever drift, this section quietly starts declaring keys nothing reads.
func TestTheDerivedKeysAreTheKeysTheReadersResolve(t *testing.T) {
	section, ok := registry.Lookup(cosmosbase.StateSyncSectionName)
	if !ok {
		t.Fatalf("%s did not register", cosmosbase.StateSyncSectionName)
	}
	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	live := []string{
		server.FlagStateSyncSnapshotInterval,
		server.FlagStateSyncSnapshotKeepRecent,
		server.FlagStateSyncSnapshotDir,
	}
	for _, key := range live {
		if !derived[key] {
			t.Errorf("a reader resolves %q and the registry derives %v. An operator's value reaches one "+
				"of those spellings and not the other", key, section.Keys)
		}
	}
	if len(section.Keys) != len(live) {
		t.Errorf("the registry derived %d keys and the readers resolve %d state-sync settings: %v",
			len(section.Keys), len(live), section.Keys)
	}
}

func TestRegisteringProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == cosmosbase.StateSyncSectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}

func TestNoExperimentalKeyShadowsThisSectionAfterRegistration(t *testing.T) {
	section, ok := registry.Lookup(cosmosbase.StateSyncSectionName)
	if !ok {
		t.Fatalf("%s did not register", cosmosbase.StateSyncSectionName)
	}
	specs := make([]configtest.KeySpec, 0, len(section.Keys))
	for _, key := range section.Keys {
		specs = append(specs, configtest.KeySpec{Key: key})
	}
	// The section name stays a literal. The wiring record reads it from this call's second argument, so a
	// constant would record every section in this package under one placeholder row.
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, "state-sync", specs)
}

// TestWiringMatchesTheRecord records which checks this package calls.
//
// A deleted check is the one edit the remaining checks cannot report, so the set of calls is held in a
// file rather than in whoever last read this directory.
func TestWiringMatchesTheRecord(t *testing.T) {
	configtest.CheckWiring(t)
}

// TestTheBaseSectionDescribesTheReaderItStandsInFor holds the node-wide keys against GetConfig.
//
// These keys need no schema written for them: BaseConfig's own mapstructure tags are the keys GetConfig
// reads, so it registers directly. What is unusual is only that the keys carry no prefix.
//
// GetConfig is the reader a running node uses. The other reader of this configuration, ParseConfig, is not
// a runtime path at all: it unmarshals into a struct to generate app.toml when the file is absent, so the
// tags decide what a fresh file contains while the keys decide what a node reads.
func TestTheBaseSectionDescribesTheReaderItStandsInFor(t *testing.T) {
	// The section name stays a literal. The wiring record reads it from this call's second argument.
	configtest.CheckSchemaMatchesTheReader(t, "base", configtest.SchemaCheck{
		Read: readBaseConfig,
		// Each probe differs from what an absent key casts to, which for every one of these is zero.
		Probe: map[string]any{
			"minimum-gas-prices":  "0.02usei",
			"pruning":             "everything",
			"pruning-keep-recent": "5",
			"pruning-interval":    "7",
			"halt-height":         uint64(100),
			"halt-time":           uint64(200),
			"min-retain-blocks":   uint64(300),
			"inter-block-cache":   true,
			"index-events":        []string{"tx.height"},
			"compaction-interval": uint64(400),
			"concurrency-workers": 9,
			"occ-enabled":         true,
		},
		Skip: map[string]string{
			"pruning-keep-every": "GetConfig does not read it; the pruning options a node runs are built " +
				"by GetPruningOptionsFromFlags, and TestPruningKeepEveryIsReadByThePruningOptions covers it",
		},
	})
}

// readBaseConfig runs the reader a running node uses over a set of written values.
//
// The telemetry labels are supplied because GetConfig asserts their type outright rather than casting, and
// refuses the whole configuration when the assertion fails. That is the one read of seventy-two that can
// stop a node, and it is why the telemetry table needs deciding about separately from these keys.
func readBaseConfig(opts configtest.AppOpts) (any, error) {
	v := viper.New()
	v.Set("telemetry.global-labels", []any{})
	for key, value := range opts {
		v.Set(key, value)
	}
	cfg, err := srvconfig.GetConfig(v)
	if err != nil {
		return nil, err
	}
	return cfg.BaseConfig, nil
}

// TestPruningKeepEveryIsReadByThePruningOptions covers the one key GetConfig leaves alone.
//
// Its value reaches a node through the pruning options the application is built with, not through the
// server configuration, so it is declared here and checked against the reader that resolves it.
func TestPruningKeepEveryIsReadByThePruningOptions(t *testing.T) {
	section, ok := registry.Lookup(cosmosbase.BaseSectionName)
	if !ok {
		t.Fatalf("%s did not register", cosmosbase.BaseSectionName)
	}
	declared := false
	for _, key := range section.Keys {
		if key == "pruning-keep-every" {
			declared = true
		}
	}
	if !declared {
		t.Fatal("pruning-keep-every is not declared, so skipping it in the check above hides nothing " +
			"and this test measures nothing")
	}

	// Custom is the one strategy that reads the three interval keys at all.
	opts := configtest.AppOpts{
		server.FlagPruning:           "custom",
		server.FlagPruningKeepRecent: "100",
		server.FlagPruningInterval:   "10",
		server.FlagPruningKeepEvery:  "3",
	}
	got, err := server.GetPruningOptionsFromFlags(opts)
	if err != nil {
		t.Fatalf("the reader refused a usable custom pruning configuration: %v", err)
	}
	if got.KeepEvery != 3 {
		t.Errorf("pruning-keep-every = 3 resolved to KeepEvery %d. The key is declared, so a value an "+
			"operator writes has to reach the setting it names", got.KeepEvery)
	}
}

// TestTheAPISchemaDescribesTheReaderItStandsInFor holds the served-interface keys against GetConfig.
func TestTheAPISchemaDescribesTheReaderItStandsInFor(t *testing.T) {
	// The section name stays a literal. The wiring record reads it from this call's second argument.
	configtest.CheckSchemaMatchesTheReader(t, "api", configtest.SchemaCheck{
		Read: func(opts configtest.AppOpts) (any, error) {
			cfg, err := readServerConfig(opts)
			if err != nil {
				return nil, err
			}
			return cfg.API, nil
		},
		// Each probe differs from what an absent key casts to, which for every one of these is zero.
		Probe: map[string]any{
			"api.enable":               true,
			"api.swagger":              true,
			"api.enabled-unsafe-cors":  true,
			"api.address":              "tcp://127.0.0.1:1318",
			"api.max-open-connections": uint(64),
			"api.rpc-read-timeout":     uint(30),
			"api.rpc-write-timeout":    uint(40),
			"api.rpc-max-body-bytes":   uint(2_000_000),
		},
	})
}

// TestTheGRPCSchemaDescribesTheReaderItStandsInFor holds the same for the gRPC interface.
//
// Seven of its eleven keys carry a duration, which no other declared section has. A duration reaches the
// reader as text an operator writes, and the reader casts it, so the probes are written the way a file
// would carry them.
func TestTheGRPCSchemaDescribesTheReaderItStandsInFor(t *testing.T) {
	configtest.CheckSchemaMatchesTheReader(t, "grpc", configtest.SchemaCheck{
		Read: func(opts configtest.AppOpts) (any, error) {
			cfg, err := readServerConfig(opts)
			if err != nil {
				return nil, err
			}
			return cfg.GRPC, nil
		},
		Probe: map[string]any{
			"grpc.enable":                          true,
			"grpc.address":                         "127.0.0.1:9091",
			"grpc.max-recv-msg-size":               1 << 21,
			"grpc.max-open-connections":            uint(64),
			"grpc.max-connection-idle":             time.Minute,
			"grpc.max-connection-age":              2 * time.Minute,
			"grpc.max-connection-age-grace":        3 * time.Minute,
			"grpc.keepalive-time":                  4 * time.Minute,
			"grpc.keepalive-timeout":               5 * time.Minute,
			"grpc.keepalive-min-time":              6 * time.Minute,
			"grpc.keepalive-permit-without-stream": true,
		},
	})
}

// readServerConfig runs the reader a running node uses over a set of written values.
//
// The telemetry labels are supplied because the reader asserts their type outright rather than casting, and
// refuses the whole configuration when the assertion fails. That is the one read of seventy-two that can
// stop a node.
func readServerConfig(opts configtest.AppOpts) (srvconfig.Config, error) {
	v := viper.New()
	v.Set("telemetry.global-labels", []any{})
	for key, value := range opts {
		v.Set(key, value)
	}
	return srvconfig.GetConfig(v)
}

func TestTheDerivedAPIAndGRPCKeysAreTheKeysTheReaderResolves(t *testing.T) {
	for _, want := range []struct {
		section string
		count   int
	}{{APISectionNameForTest, 8}, {GRPCSectionNameForTest, 11}} {
		section, ok := registry.Lookup(want.section)
		if !ok {
			t.Fatalf("%s did not register", want.section)
		}
		if len(section.Keys) != want.count {
			t.Errorf("%s derived %d keys, want %d: %v. The upstream type's tags are the only spelling, so "+
				"a count that moves is a key an operator's file no longer reaches",
				want.section, len(section.Keys), want.count, section.Keys)
		}
		for _, key := range section.Keys {
			if !strings.HasPrefix(key, want.section+".") {
				t.Errorf("%s derived %q, which is not under its own section", want.section, key)
			}
		}
	}
}

// The section names as literals, for the count check above. Written out because the wiring record reads a
// section name from a call's second argument, and these are not those calls.
const (
	APISectionNameForTest  = "api"
	GRPCSectionNameForTest = "grpc"
)

// TestTheTelemetrySchemaDescribesTheReaderItStandsInFor holds the metric settings against GetConfig.
//
// The label set is probed with a list of pairs, which is the only shape its reader takes. That is the same
// fact that keeps it out of the environment layer.
func TestTheTelemetrySchemaDescribesTheReaderItStandsInFor(t *testing.T) {
	// The section name stays a literal. The wiring record reads it from this call's second argument.
	configtest.CheckSchemaMatchesTheReader(t, "telemetry", configtest.SchemaCheck{
		Read: func(opts configtest.AppOpts) (any, error) {
			cfg, err := readServerConfig(opts)
			if err != nil {
				return nil, err
			}
			return cfg.Telemetry, nil
		},
		Probe: map[string]any{
			"telemetry.service-name":              "sei-probe",
			"telemetry.enabled":                   true,
			"telemetry.enable-hostname":           true,
			"telemetry.enable-hostname-label":     true,
			"telemetry.enable-service-label":      true,
			"telemetry.prometheus-retention-time": int64(600),
			"telemetry.global-labels":             []any{[]any{"chain_id", "pacific-1"}},
		},
	})
}

// TestTheLabelSetIsRefusedFromTheEnvironment is the decision this section is built around.
//
// The reader takes the label set's exact type rather than casting what it finds, so no single environment
// string can supply it, and resolving one would install a value that stops the node. The channel is refused
// instead, which means the file's value applies and the node runs.
//
// That is deliberately not what the machinery this replaces does. It resolves the variable and the node
// refuses to start. The difference is recorded here rather than left for somebody to discover.
func TestTheLabelSetIsRefusedFromTheEnvironment(t *testing.T) {
	refused := registry.EnvCannotDeliver()
	reason, named := refused[cosmosbase.GlobalLabelsKey]
	if !named {
		t.Fatalf("%s is not refused from the environment. A variable holding it resolves to the top of the "+
			"order and installs a value the reader cannot use, which stops the node",
			cosmosbase.GlobalLabelsKey)
	}
	if reason == "" {
		t.Error("the refusal carries no reason, so an operator told their variable is ignored cannot learn why")
	}

	// The layer is what has to leave it out; the record above is only a statement about it.
	layer := registry.EnvLayer(func(string) (string, bool) { return "chain_id=pacific-1", true })
	if _, present := layer.Values[cosmosbase.GlobalLabelsKey]; present {
		t.Error("the environment layer carried the label set anyway. The record and the layer have to agree, " +
			"or the refusal is a comment")
	}
	// Every other key in the section still comes from the environment, so the refusal is one key and not
	// the whole section.
	if _, present := layer.Values["telemetry.service-name"]; !present {
		t.Error("the environment layer dropped a key it can deliver. Refusing one key must not refuse the " +
			"section it belongs to")
	}
}
