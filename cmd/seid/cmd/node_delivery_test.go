package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// The second delivery, driven the way an operator reaches it.
//
// A section whose reader looks its keys up one at a time is delivered by putting the value into the source.
// The node's own configuration file is read once into a struct before any of that, so a value put into the
// source reaches nothing and has to be decoded into the struct instead. These read the setting the node
// runs rather than the source it was resolved into, because a key can be correct in the source and absent
// from the struct.

// bootWithNodeFile runs a real boot against a sei.toml and a generated node configuration file.
func bootWithNodeFile(t *testing.T, seiToml string, edit func(*tmcfg.Config)) *server.Context {
	t.Helper()
	home := configtest.NewHome(t)
	dir := filepath.Join(home.Root, "config")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// The node's own file, generated the way the node generates it, so what the delivery writes over is
	// what an operator would actually have.
	live := tmcfg.DefaultConfig()
	if edit != nil {
		edit(live)
	}
	if err := tmcfg.WriteConfigFile(home.Root, live); err != nil {
		t.Fatalf("render the node's configuration file: %v", err)
	}
	if seiToml != "" {
		if err := os.WriteFile(filepath.Join(dir, "sei.toml"), []byte(seiToml), 0o600); err != nil {
			t.Fatalf("write sei.toml: %v", err)
		}
	}

	cmd := server.StartCmd(nil, home.Root, []trace.TracerProviderOption{})
	if err := cmd.Flags().Set("home", home.Root); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	ctx, err := runManager(t, configmanager.SeiConfigManager{}, cmd)
	if err != nil {
		t.Fatalf("the boot was refused: %v", err)
	}
	if ctx.Config == nil {
		t.Fatal("the boot produced no node configuration")
	}
	return ctx
}

const nodeFileHeader = "schema_version = 1\nnode_mode = \"validator\"\n"

// TestAWrittenValueReachesTheNodesOwnConfiguration is the property the whole thing rests on.
func TestAWrittenValueReachesTheNodesOwnConfiguration(t *testing.T) {
	configtest.Isolate(t)
	ctx := bootWithNodeFile(t, nodeFileHeader+
		"\n[instrumentation]\nprometheus = true\nmax-open-connections = 41\n", nil)

	if !ctx.Config.Instrumentation.Prometheus {
		t.Error("sei.toml turned the metrics listener on and the node runs with it off. The value was " +
			"resolved and put into a source that nothing reading this file ever consults")
	}
	if got := ctx.Config.Instrumentation.MaxOpenConnections; got != 41 {
		t.Errorf("sei.toml set max-open-connections to 41 and the node runs %d", got)
	}
}

// TestAnUnwrittenKeyKeepsWhatTheNodesOwnFileSaid separates delivering a value from overwriting one.
//
// A section read by a lookup can be delivered whole, because its reader has nowhere else to get a value
// from. A section read by a decode already holds what its own file said, put there before this ran. So a
// key the operator's sei.toml does not mention has to arrive at whatever that file gave it, and delivering
// a default instead replaces their file with one nobody chose, on every boot.
//
// The fixture turns the key on in the node's own file, where the default is off, so the two disagree.
// Without that they agree and the overwrite is invisible.
func TestAnUnwrittenKeyKeepsWhatTheNodesOwnFileSaid(t *testing.T) {
	configtest.Isolate(t)
	ctx := bootWithNodeFile(t, nodeFileHeader+"\n[mempool]\nsize = 4321\n", func(live *tmcfg.Config) {
		live.Instrumentation.Prometheus = true
	})

	if got := ctx.Config.Mempool.Size; got != 4321 {
		t.Fatalf("the written key arrived as %d, so this test cannot tell the two cases apart", got)
	}
	if !ctx.Config.Instrumentation.Prometheus {
		t.Error("the node's own file turned the metrics listener on, sei.toml said nothing about it, and " +
			"the node runs with it off. A default was delivered over the operator's own file, which " +
			"happens on every boot for every key their sei.toml does not mention")
	}
}

// TestARefusedValueLeavesItsSectionAlone is the promise that makes this safe to enable.
//
// A decoder gathers errors and keeps going, so a value it refuses partway leaves its target holding some
// new values and some old, with nothing to compare against. The delivery decodes into a copy and publishes
// by replacing, so a refused value leaves the section exactly as the node had it.
func TestARefusedValueLeavesItsSectionAlone(t *testing.T) {
	configtest.Isolate(t)
	ctx := bootWithNodeFile(t, nodeFileHeader+
		"\n[instrumentation]\nprometheus = true\nmax-open-connections = \"not a number\"\n", nil)

	if got := ctx.Config.Instrumentation.MaxOpenConnections; got != 3 {
		t.Errorf("max-open-connections is %d after a refused value, want the 3 the node had. A "+
			"partly applied decode leaves settings nobody chose", got)
	}
	if ctx.Config.Instrumentation.Prometheus {
		t.Error("the value beside the refused one was applied, so a partial decode was published. " +
			"Either all of a section's values arrive or none do")
	}
}

// TestARefusedValueCostsOnlyItsOwnSection is why the delivery is per section.
//
// One decode for the whole file would mean an operator who fixed one setting and mistyped another boots
// with neither applied. The mistyped section is lost; the one beside it is not.
func TestARefusedValueCostsOnlyItsOwnSection(t *testing.T) {
	configtest.Isolate(t)
	ctx := bootWithNodeFile(t, nodeFileHeader+
		"\n[instrumentation]\nmax-open-connections = \"not a number\"\n"+
		"\n[mempool]\nsize = 4321\n", nil)

	if got := ctx.Config.Instrumentation.MaxOpenConnections; got != 3 {
		t.Errorf("the refused section was applied anyway, reading %d", got)
	}
	if got := ctx.Config.Mempool.Size; got != 4321 {
		t.Errorf("mempool.size is %d and sei.toml set it to 4321. A value refused in one section took "+
			"another section's settings down with it", got)
	}
}

// TestEachChannelWinsForADecodedKeyToo is precedence, asserted where a decoded value lands.
func TestEachChannelWinsForADecodedKeyToo(t *testing.T) {
	const key = "rpc.max-open-connections"
	body := nodeFileHeader + "\n[rpc]\nmax-open-connections = 111\n"

	t.Run("the file beats what the node's own file said", func(t *testing.T) {
		configtest.Isolate(t)
		ctx := bootWithNodeFile(t, body, nil)
		if got := ctx.Config.RPC.MaxOpenConnections; got != 111 {
			t.Errorf("the node runs %d with 111 in sei.toml; the value resolved and never reached the "+
				"struct the node reads", got)
		}
	})

	t.Run("the environment beats the file", func(t *testing.T) {
		configtest.Isolate(t)
		t.Setenv(registry.EnvName(key), "222")
		ctx := bootWithNodeFile(t, body, nil)
		if got := ctx.Config.RPC.MaxOpenConnections; got != 222 {
			t.Errorf("the node runs %d with 222 in the environment and 111 in the file", got)
		}
	})
}

// TestTheDeliveryLeavesTheRootDirectoryAlone is what the root-directory exclusions buy.
func TestTheDeliveryLeavesTheRootDirectoryAlone(t *testing.T) {
	configtest.Isolate(t)
	ctx := bootWithNodeFile(t, nodeFileHeader+"\n[instrumentation]\nprometheus = true\n", nil)

	if !ctx.Config.Instrumentation.Prometheus {
		t.Fatal("the delivery did not run, so this test would pass with the root directory declared")
	}
	if ctx.Config.RootDir == "" {
		t.Error("the node's root directory is empty after the delivery")
	}
	if ctx.Config.PrivValidator.RootDir == "" {
		t.Error("the signing key's root directory is empty after the delivery. A node that cannot find " +
			"its key does not sign")
	}
}
