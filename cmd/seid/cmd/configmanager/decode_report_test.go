package configmanager

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
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
	deliverDecodedSections(ctx, forADecode, log, log.Info)

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

// TestAnUnreadKeyIsNotComparedAsUnchanged covers the statement the report must withhold.
//
// A key neither side could be read for is absent from both answers, so a comparison finds it equal and says
// it did not move. That is what a key an operator wrote and got looks like, produced by having read nothing,
// and the line naming the unread keys exists precisely so the report does not claim it.
func TestAnUnreadKeyIsNotComparedAsUnchanged(t *testing.T) {
	keys := []string{"mempool.size", "mempool.ttl-duration", "mempool.max-tx-bytes"}
	unread := asSet([]string{"mempool.ttl-duration"})

	got := whatBothSidesCouldBeReadFor(keys, unread)
	want := []string{"mempool.size", "mempool.max-tx-bytes"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the keys compared are %v, want %v", got, want)
	}

	// The report over the surviving keys must still say what moved, so the filter cannot be a way of
	// reporting nothing.
	var out bytes.Buffer
	log := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reportWhatMoved("mempool", got,
		map[string]string{"mempool.size": "5000", "mempool.max-tx-bytes": "1048576"},
		map[string]string{"mempool.size": "4321", "mempool.max-tx-bytes": "1048576"},
		log, log.Info)
	if !strings.Contains(out.String(), "mempool.size") {
		t.Errorf("the report does not name the key that moved:\n%s", out.String())
	}
	if strings.Contains(out.String(), "ttl-duration") {
		t.Errorf("the report names a key neither side could be read for:\n%s", out.String())
	}
}

// theSecretANodeHolds is a PostgreSQL connection string of the shape tx-index.psql-conn carries.
const theSecretANodeHolds = "postgresql://indexer:hunter2@db/tx"

// TestNoReportPrintsAValueANodeHolds is the bound every line this delivery writes is held to.
//
// tx-index.psql-conn holds a password. A node that sets it in its own file, and leaves it out of sei.toml,
// gets the declared empty value delivered over it, so the key moves on every boot of that node. Validator
// operators hand their logs to third parties, so a report that renders what a key held publishes the
// password to all of them.
func TestNoReportPrintsAValueANodeHolds(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A file that mentions the key nowhere, which is the node this is about.
	if err := os.WriteFile(filepath.Join(home, "config", seiTomlName),
		[]byte("schema_version = 1\nnode_mode = \""+tmcfg.DefaultConfig().Mode+"\"\n"), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}
	if strings.Contains(theSecretANodeHolds, tmcfg.DefaultConfig().Mode) {
		t.Fatalf("the secret contains the node kind %q, which the reports print, so this cannot tell a "+
			"leaked value from a line that is meant to be there", tmcfg.DefaultConfig().Mode)
	}

	sctx := server.NewDefaultContext()
	sctx.Config.TxIndex.PsqlConn = theSecretANodeHolds

	// The command that runs a node, so the routine line reports at the level an operator reads.
	cmd := &cobra.Command{Use: "start"}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, sctx))
	cmd.Flags().String(flags.FlagHome, home, "")

	var out bytes.Buffer
	installResolved(cmd, nil, slog.New(slog.NewTextHandler(&out,
		&slog.HandlerOptions{Level: slog.LevelDebug})))

	if got := sctx.Config.TxIndex.PsqlConn; got == theSecretANodeHolds {
		t.Fatalf("the declared value was not delivered over the connection string, so the key did not "+
			"move and no report named it:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "this section's settings now differ") {
		t.Fatalf("the report that names what moved is absent, so this measures nothing:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "tx-index.psql-conn") {
		t.Fatalf("the report does not name the key that moved, so this measures a line about other "+
			"keys:\n%s", out.String())
	}
	for _, leaked := range []string{theSecretANodeHolds, "hunter2"} {
		if strings.Contains(out.String(), leaked) {
			t.Errorf("a log line carries %q, which is a value this node's own file holds. Every operator "+
				"who shares a log shares this:\n%s", leaked, out.String())
		}
	}
}
