package configmanager

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/sections"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	serverconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"
)

// TestTheBootSeesEverySectionThisBinaryDeclares is what makes the registration import load-bearing.
//
// A section absent here is one whose values are never installed into the booting node, so its keys
// resolve through the machinery that answered them before. Nothing else in this package would notice,
// because an undeclared key is delegated by design.
func TestTheBootSeesEverySectionThisBinaryDeclares(t *testing.T) {
	if missing := sections.Missing(); len(missing) > 0 {
		t.Fatalf("these sections are absent from this test binary: %s\n\nTheir values are not installed "+
			"into a booting node and the delegation that covers them is silent by design",
			strings.Join(missing, ", "))
	}
}

// TestTheBootSaysWhichChannelSuppliedAValue is what the layered resolution buys an operator.
//
// A node whose file says one thing and whose behaviour is another gives an operator nothing to go on when
// the layers are merged before anything observes them. This is the one place the origin is still known,
// so a value arriving from somewhere other than the file has to be said out loud.
func TestTheBootSaysWhichChannelSuppliedAValue(t *testing.T) {
	configtest.Isolate(t)
	root := writeMinimalHome(t, "mode = \"full\"\n", "")

	// The file writes one declared key, and the environment takes a second one.
	seiToml := "schema_version = 1\nnode_mode = \"full\"\n\n[giga_executor]\nenabled = true\n"
	path := filepath.Join(root, "config", seiTomlName)
	require.NoError(t, os.WriteFile(path, []byte(seiToml), 0o600))
	t.Setenv(registry.EnvName("receipt-store.rs-backend"), "littidx")

	cmd := server.StartCmd(nil, "/foobar", []trace.TracerProviderOption{})
	require.NoError(t, cmd.Flags().Set(flags.FlagHome, root))
	serverCtx := &server.Context{}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverCtx))

	capture := &capturingHandler{}
	require.NoError(t, SeiConfigManager{logger: slog.New(capture)}.Apply(cmd,
		serverconfig.DefaultConfigTemplate, serverconfig.DefaultConfig()))

	var lines []string
	for i := range capture.records {
		lines = append(lines, renderRecord(capture.records[i]))
	}
	joined := strings.Join(lines, "\n")

	for _, want := range []string{"channel=file", "giga_executor.enabled", "channel=env",
		"receipt-store.rs-backend"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the boot did not report %q. An operator cannot tell which source won without "+
				"it:\n%s", want, joined)
		}
	}

	// Keys nobody chose are left out. A node resolves over a hundred declared keys and nearly all of
	// them take a baseline, so reporting those would bury the two above in the noise it creates.
	if strings.Contains(joined, "channel=default") {
		t.Errorf("the boot reported the baseline channel. Nearly every declared key takes its baseline, "+
			"so the keys an operator actually supplied are lost in the report:\n%s", joined)
	}
	if strings.Contains(joined, "receipt-store.async-write-buffer") {
		t.Errorf("the boot named a key that took its baseline. The report is meant to be what somebody "+
			"chose, not an inventory of the key space:\n%s", joined)
	}
}
