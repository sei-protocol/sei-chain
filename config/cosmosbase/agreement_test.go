package cosmosbase

import (
	"fmt"
	"sort"
	"testing"

	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
)

// legacyConfigManagerDefaults is what each diverging key resolves to under the manager this replaces.
//
// A declared value is what seid init writes for a kind of node. That is not what a node with nothing
// written resolves, and these are the keys where the two differ. Most are reads that take no account of
// whether the key was present, so an absent key casts to a zero and the default beside it is lost.
//
// Held as text because the two sides carry different Go types for the same key often enough that comparing
// values would be comparing shapes. What matters here is which keys disagree and what a node gets instead.
var legacyConfigManagerDefaults = map[string]string{
	"api.address":              "",
	"api.max-open-connections": "0",
	"api.rpc-max-body-bytes":   "0",
	"api.rpc-read-timeout":     "0",
	"api.swagger":              "false",
	"grpc.enable":              "true",
	"minimum-gas-prices":       "",
	"occ-enabled":              "false",
	"pruning":                  "default",
	"pruning-keep-every":       "",
	"telemetry.enabled":        "false",
}

// reasoning says what a node gets under that manager, for the keys where it is worth stating.
var reasoning = map[string]string{
	"pruning": "a command flag of this name carries the standard schedule below the file, so a node with " +
		"nothing written prunes on that schedule where a generated file would have said keep everything",
	"grpc.enable": "a command flag of this name defaults the interface on, so a validator with nothing " +
		"written serves gRPC where a generated file would have written it off. This is the one interface " +
		"toggle of the two that diverges; the REST one agrees",
	"api.max-open-connections": "zero is unlimited, so the ceiling a generated file states is simply " +
		"absent from a node that never wrote it, and the same holds for the body-size ceiling beside it",
	"minimum-gas-prices": "an empty price refuses to start, so this key is one no running node can " +
		"actually have unwritten",
	"occ-enabled": "the transaction execution path, and no command flag carries it, so an absent key " +
		"reads as off where a generated file says on",
}

// readerValues is what a node resolves for a configuration carrying none of these keys.
//
// Driven through the reader rather than reasoned about, because the reader is the authority on what an
// absent key resolves to and its answer differs per key: some reads check that the key was present, most
// do not, and two are rescued by a clamp that does nothing for an absent value.
//
// The start command's flags are bound first, the way a booting node binds them, and that is what makes
// this the answer a node gets rather than the answer the reader gives in isolation. Seventeen of these keys
// are also command flags, so a flag's registration default is what an absent key reaches before the lookup
// comes back empty. Without the binding, a key like the gRPC toggle reads as its type's zero and the
// comparison would report agreement where a node disagrees.
//
// One key has to be supplied. The metric label set is the first thing the reader asks for and it refuses a
// configuration without it, so a reader handed nothing at all answers for no key at all.
func readerValues(t *testing.T) map[string]string {
	t.Helper()
	v := viper.New()
	start := server.StartCmd(nil, t.TempDir(), nil)
	if err := v.BindPFlags(start.Flags()); err != nil {
		t.Fatalf("bind the start flags: %v", err)
	}
	v.Set(globalLabelsKey, []any{})
	cfg, err := srvconfig.GetConfig(v)
	if err != nil {
		t.Fatalf("the reader refused a configuration carrying only the label set: %v", err)
	}

	return map[string]string{
		"minimum-gas-prices":                   fmt.Sprint(cfg.MinGasPrices),
		"pruning":                              fmt.Sprint(cfg.Pruning),
		"pruning-keep-recent":                  fmt.Sprint(cfg.PruningKeepRecent),
		"pruning-keep-every":                   fmt.Sprint(cfg.PruningKeepEvery),
		"pruning-interval":                     fmt.Sprint(cfg.PruningInterval),
		"halt-height":                          fmt.Sprint(cfg.HaltHeight),
		"halt-time":                            fmt.Sprint(cfg.HaltTime),
		"freeze-height":                        fmt.Sprint(cfg.FreezeHeight),
		"min-retain-blocks":                    fmt.Sprint(cfg.MinRetainBlocks),
		"inter-block-cache":                    fmt.Sprint(cfg.InterBlockCache),
		"compaction-interval":                  fmt.Sprint(cfg.CompactionInterval),
		"concurrency-workers":                  fmt.Sprint(cfg.ConcurrencyWorkers),
		"occ-enabled":                          fmt.Sprint(cfg.OccEnabled),
		"api.enable":                           fmt.Sprint(cfg.API.Enable),
		"api.swagger":                          fmt.Sprint(cfg.API.Swagger),
		"api.address":                          fmt.Sprint(cfg.API.Address),
		"api.enabled-unsafe-cors":              fmt.Sprint(cfg.API.EnableUnsafeCORS),
		"api.max-open-connections":             fmt.Sprint(cfg.API.MaxOpenConnections),
		"api.rpc-read-timeout":                 fmt.Sprint(cfg.API.RPCReadTimeout),
		"api.rpc-write-timeout":                fmt.Sprint(cfg.API.RPCWriteTimeout),
		"api.rpc-max-body-bytes":               fmt.Sprint(cfg.API.RPCMaxBodyBytes),
		"grpc.enable":                          fmt.Sprint(cfg.GRPC.Enable),
		"grpc.address":                         fmt.Sprint(cfg.GRPC.Address),
		"grpc.max-recv-msg-size":               fmt.Sprint(cfg.GRPC.MaxRecvMsgSize),
		"grpc.max-open-connections":            fmt.Sprint(cfg.GRPC.MaxOpenConnections),
		"grpc.max-connections-per-ip":          fmt.Sprint(cfg.GRPC.MaxConnectionsPerIP),
		"grpc.max-connection-idle":             fmt.Sprint(cfg.GRPC.MaxConnectionIdle),
		"grpc.max-connection-age":              fmt.Sprint(cfg.GRPC.MaxConnectionAge),
		"grpc.max-connection-age-grace":        fmt.Sprint(cfg.GRPC.MaxConnectionAgeGrace),
		"grpc.keepalive-time":                  fmt.Sprint(cfg.GRPC.KeepaliveTime),
		"grpc.keepalive-timeout":               fmt.Sprint(cfg.GRPC.KeepaliveTimeout),
		"grpc.keepalive-min-time":              fmt.Sprint(cfg.GRPC.KeepaliveMinTime),
		"grpc.keepalive-permit-without-stream": fmt.Sprint(cfg.GRPC.KeepalivePermitWithoutStream),
		"grpc.ip-rate-limit-rps":               fmt.Sprint(cfg.GRPC.IPRateLimitRPS),
		"grpc.ip-rate-limit-burst":             fmt.Sprint(cfg.GRPC.IPRateLimitBurst),
		"grpc.max-in-flight-per-ip":            fmt.Sprint(cfg.GRPC.MaxInFlightPerIP),
		"grpc.rate-limiting-enabled":           fmt.Sprint(cfg.GRPC.RateLimitingEnabled),
		"grpc.trusted-proxy-cidrs":             fmt.Sprint(cfg.GRPC.TrustedProxyCIDRs),
		"telemetry.service-name":               fmt.Sprint(cfg.Telemetry.ServiceName),
		"telemetry.enabled":                    fmt.Sprint(cfg.Telemetry.Enabled),
		"telemetry.enable-hostname":            fmt.Sprint(cfg.Telemetry.EnableHostname),
		"telemetry.enable-hostname-label":      fmt.Sprint(cfg.Telemetry.EnableHostnameLabel),
		"telemetry.enable-service-label":       fmt.Sprint(cfg.Telemetry.EnableServiceLabel),
		"telemetry.prometheus-retention-time":  fmt.Sprint(cfg.Telemetry.PrometheusRetentionTime),
		"state-sync.snapshot-interval":         fmt.Sprint(cfg.StateSync.SnapshotInterval),
		"state-sync.snapshot-keep-recent":      fmt.Sprint(cfg.StateSync.SnapshotKeepRecent),
		"state-sync.snapshot-directory":        fmt.Sprint(cfg.StateSync.SnapshotDirectory),
		"index-events":                         fmt.Sprint(cfg.IndexEvents),
		globalLabelsKey:                        fmt.Sprint(cfg.Telemetry.GlobalLabels),
	}
}

// TestTheDivergencesFromTheReaderAreTheRecordedOnes measures what a comment used to count.
//
// A declared value is what seid init writes for a kind of node, and for a good number of these keys that is
// not what a node with nothing written resolves. Which keys those are was carried in prose, in four
// paragraphs, and one of the counts was wrong. Prose cannot fail when it is wrong.
//
// So the set is measured. A key that starts diverging fails, and so does one that stops, which means
// guarding a read has to account for its row rather than quietly making a sentence stale.
//
// Run for the mode whose declared values match the reader's own mode-blind answer most closely, because
// the reader takes no mode and comparing every mode against it would report the mode rules as divergences.
// The mode-varying keys are held by name in the test beside this one.
func TestTheDivergencesFromTheReaderAreTheRecordedOnes(t *testing.T) {
	reader := readerValues(t)
	resolved, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	var measured []string
	for key, got := range reader {
		declared, declares := resolved.Values[key]
		if !declares {
			t.Errorf("%s is read by the upstream reader and no section here declares it", key)
			continue
		}
		if fmt.Sprint(declared) == got {
			if _, listed := legacyConfigManagerDefaults[key]; listed {
				t.Errorf("%s no longer diverges, both sides being %v. Take it off the record, so the "+
					"record stays the set of keys a generated file states differently from a node that "+
					"never wrote them", key, declared)
			}
			continue
		}
		measured = append(measured, key)
		want, listed := legacyConfigManagerDefaults[key]
		switch {
		case !listed:
			t.Errorf("%s is declared as %v and a node with nothing written resolves %q, and nothing "+
				"records that. %s", key, declared, got, reasoning[key])
		case want != got:
			t.Errorf("%s is recorded as resolving %q and resolves %q", key, want, got)
		}
	}

	sort.Strings(measured)
	if len(measured) != len(legacyConfigManagerDefaults) {
		t.Errorf("measured %d divergences and %d are recorded: %v",
			len(measured), len(legacyConfigManagerDefaults), measured)
	}
}

// TestEveryKeyTheseSectionsDeclareIsOneTheReaderResolves holds the two lists against each other.
//
// The reader's side is written out above, which is a second statement of the same key set. It is the only
// statement available: this reader looks its keys up as inline strings rather than through constants, so
// there is nothing to compare a tag against. A key on one side only is either a setting an operator writes
// that no reader fills, or one the reader fills that no section here declares.
func TestEveryKeyTheseSectionsDeclareIsOneTheReaderResolves(t *testing.T) {
	reader := readerValues(t)
	for _, section := range []string{
		BaseSectionName, APISectionName, GRPCSectionName, TelemetrySectionName, StateSyncSectionName,
	} {
		registered, ok := registry.Lookup(section)
		if !ok {
			t.Fatalf("%s is not registered", section)
		}
		for _, key := range registered.Keys {
			if _, filled := reader[key]; !filled {
				t.Errorf("%s declares %s and no field above is paired with it", section, key)
			}
		}
	}
}
