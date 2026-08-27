package configmanager

import (
	"bytes"
	"fmt"
	"log/slog"
	"strconv"
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
