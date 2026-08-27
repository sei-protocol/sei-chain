package configmanager

import (
	"bytes"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// TestEveryDeclaredKeyOfADecodedSectionIsDelivered is what makes sei.toml the configuration for the
// sections a reader decodes whole.
//
// The install cannot reach them, so a value only arrives through this decode. Every key the resolution
// answered has to arrive, not only the ones a source wrote, or a key sei.toml leaves out would keep
// whatever config.toml said and the file would be a patch rather than the configuration.
func TestEveryDeclaredKeyOfADecodedSectionIsDelivered(t *testing.T) {
	var out bytes.Buffer
	log := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// A node holding a value nobody wrote in sei.toml.
	live := tmcfg.DefaultConfig()
	live.P2P.MaxConnections = 999
	ctx := &server.Context{Config: live}

	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	forADecode, _ := registry.ResolvedAndOwnedByDecodedSections(resolved)
	deliverDecodedSections(ctx, forADecode, log)

	declared := resolved.Values["p2p.max-connections"]
	if declared == nil {
		t.Fatal("p2p.max-connections declares nothing, so this measures nothing")
	}
	if got := uint64(ctx.Config.P2P.MaxConnections); got != asUint(t, declared) {
		t.Errorf("the node runs a peer ceiling of %d after a resolution that declares %v and a sei.toml "+
			"that mentions nothing, want the declared value. A key the file leaves out has to take the "+
			"declaration, or config.toml is still the configuration", got, declared)
	}
}

// asUint reads a declared numeric value as an unsigned number.
func asUint(t *testing.T, v any) uint64 {
	t.Helper()
	n, err := strconv.ParseUint(fmt.Sprint(v), 10, 64)
	if err != nil {
		t.Fatalf("the declared value %v is not a number: %v", v, err)
	}
	return n
}

// TestAPasswordInASettingDoesNotReachTheReport covers the one value here that is a secret.
//
// The transaction index can be told to write to PostgreSQL, and it is told so with a connection string that
// carries the password in it. This report is the only place the running configuration is written down,
// which makes it the only place that password reaches a log file, a journal and whatever ships them onward.
// The node's own configuration file holds the same string, and nothing there reads it out to a log.
//
// The report cannot be turned down either: this package holds its own logger at a floor so a quiet fleet
// still sees what a delivery changed.
func TestAPasswordInASettingDoesNotReachTheReport(t *testing.T) {
	const password = "sup3rs3cret"
	const dsn = "postgres://seid:" + password + "@10.0.0.9:5432/idx"

	var out bytes.Buffer
	log := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reportWhatMoved("tx-index",
		[]string{"tx-index.psql-conn"},
		map[string]string{"tx-index.psql-conn": ""},
		map[string]string{"tx-index.psql-conn": dsn},
		log)

	if strings.Contains(out.String(), password) {
		t.Errorf("the report carries the password from a connection string: %s", out.String())
	}
	if !strings.Contains(out.String(), "10.0.0.9:5432") {
		t.Errorf("the report no longer says where the index writes, so an operator cannot tell what "+
			"moved: %s", out.String())
	}
	if !strings.Contains(out.String(), "tx-index.psql-conn") {
		t.Errorf("the report does not name the key that moved: %s", out.String())
	}
}

// TestAValueWithNoPasswordIsReportedAsWritten keeps the redaction from rewriting ordinary values.
//
// Most settings are not connection strings, and a value an operator reads back has to be the one they
// wrote. A path, a host and port, and a list of words all parse as something a URL parser accepts, so the
// narrow case is what has to be detected rather than anything that parses.
func TestAValueWithNoPasswordIsReportedAsWritten(t *testing.T) {
	for _, value := range []string{
		"tcp://0.0.0.0:26656",
		"/var/lib/sei/data",
		"kv",
		"",
		"postgres://seid@10.0.0.9:5432/idx",
		"a,b,c",
	} {
		if got := withoutCredentials(value); got != value {
			t.Errorf("%q is reported as %q, and an operator reading it back has to see what they wrote",
				value, got)
		}
	}
}
