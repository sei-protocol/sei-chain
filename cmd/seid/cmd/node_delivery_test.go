package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

// TestATypedFlagReachesTheKeyItCarries covers the one channel an operator reaches for under pressure.
//
// A flag's name and the key it carries are not always spelled the same: the node's own flags separate words
// with an underscore where the tag they decode through uses a hyphen. Compared as strings such a flag looks
// like a name nothing declares, so it is dropped, and the file wins over the command line.
//
// Driven with the file and the flag disagreeing, and read off the struct the node runs from, because this
// key belongs to a section delivered by a decode.
func TestATypedFlagReachesTheKeyItCarries(t *testing.T) {
	const key = "p2p.unconditional-peer-ids"
	const flag = "p2p.unconditional_peer_ids"
	configtest.Isolate(t)

	home := configtest.NewHome(t)
	dir := filepath.Join(home.Root, "config")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := tmcfg.WriteConfigFile(home.Root, tmcfg.DefaultConfig()); err != nil {
		t.Fatalf("render the node's configuration file: %v", err)
	}
	body := nodeFileHeader + "\n[p2p]\nunconditional-peer-ids = \"from-the-file\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sei.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}

	cmd := server.StartCmd(nil, home.Root, []trace.TracerProviderOption{})
	if err := cmd.Flags().Set("home", home.Root); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set(flag, "from-the-command-line"); err != nil {
		t.Skipf("--%s is not on this command, so nothing here can carry the key: %v", flag, err)
	}
	ctx, err := runManager(t, configmanager.SeiConfigManager{}, cmd)
	if err != nil {
		t.Fatalf("the boot was refused: %v", err)
	}

	if got := ctx.Config.P2P.UnconditionalPeerIDs; got != "from-the-command-line" {
		t.Errorf("the node runs %q with --%s typed and a different value in the file, want the typed "+
			"one. The flag's name and the key it carries are spelled differently, so comparing them as "+
			"strings drops the flag and the file wins over the command line", got, flag)
	}
}

// TestALengthOfTimeWrittenAsAPlainNumberIsRefused covers a value that decodes cleanly and is wrong by a
// factor of a billion.
//
// The file format has no way to say how long something is, so a length of time is written as text with a
// unit. A plain number decodes as nanoseconds, the shortest unit there is, so sixty means sixty billionths
// of a second and the node starts. Nothing later objects, because nothing later can tell.
func TestALengthOfTimeWrittenAsAPlainNumberIsRefused(t *testing.T) {
	configtest.Isolate(t)
	was := tmcfg.DefaultConfig().Mempool.TTLDuration

	t.Run("a plain number is refused and the section is left alone", func(t *testing.T) {
		ctx := bootWithNodeFile(t, nodeFileHeader+"\n[mempool]\nttl-duration = 60\nsize = 4321\n", nil)
		if got := ctx.Config.Mempool.TTLDuration; got != was {
			t.Errorf("the node runs a time-to-live of %v after a plain 60 was written, want the %v it "+
				"had. Sixty read as nanoseconds is sixty billionths of a second", got, was)
		}
		if got := ctx.Config.Mempool.Size; got == 4321 {
			t.Error("the value beside the refused one was applied, so the section was published in part")
		}
	})

	t.Run("zero is applied, because zero is the same in every unit", func(t *testing.T) {
		// Several of these settings document zero as the way to turn them off, and three declare it as
		// their value, so an operator writing it is doing the ordinary thing. Refusing it would cost them
		// every other key in the section.
		ctx := bootWithNodeFile(t, nodeFileHeader+
			"\n[rpc]\ntimeout-read-header = 0\nmax-open-connections = 41\n", nil)
		if got := ctx.Config.RPC.TimeoutReadHeader; got != 0 {
			t.Errorf("the node runs a read-header timeout of %v with 0 written, want 0", got)
		}
		if got := ctx.Config.RPC.MaxOpenConnections; got != 41 {
			t.Errorf("max-open-connections is %d, so writing a zero length of time cost the section. "+
				"Zero nanoseconds and zero seconds are the same value, so there is nothing to refuse", got)
		}
	})

	t.Run("the same number with a unit is applied", func(t *testing.T) {
		ctx := bootWithNodeFile(t, nodeFileHeader+"\n[mempool]\nttl-duration = \"60s\"\n", nil)
		if got := ctx.Config.Mempool.TTLDuration; got != 60*time.Second {
			t.Errorf("the node runs %v with \"60s\" written, want 60s. Refusing a plain number must not "+
				"refuse the written form an operator is being asked for", got)
		}
	})
}

// TestTheReportSurvivesAQuietNode is what a fleet running its nodes quiet needs.
//
// One log level covers every logger in the process and an operator writes it. A fleet that sets it above the
// level these reports use turns this manager into a component that changes what a node runs and says nothing
// about it, and the report is the only place the node's own file and the running settings can be told apart.
//
// The level is what is asserted rather than a message, because a message can be absent for reasons that have
// nothing to do with whether it would have been printed.
func TestTheReportSurvivesAQuietNode(t *testing.T) {
	configtest.Isolate(t)

	ctx := bootWithNodeFile(t, nodeFileHeader+"log-level = \"error\"\n\n[mempool]\nsize = 4321\n", nil)
	if got := ctx.Config.Mempool.Size; got != 4321 {
		t.Fatalf("the value was not delivered (%d), so this test cannot show a report being kept", got)
	}
	if !configmanager.OwnReportingEnabledForTest() {
		t.Error("a node whose file sets the level to error delivered a value and this manager's own " +
			"reporting is switched off. The report is the only signal it has, and the node's own file " +
			"and its running settings can be told apart nowhere else")
	}
}

// TestANegativeNumberWhereTheSettingCannotHoldOneIsRefused covers the habit of writing minus one for
// "no limit".
//
// Most software an operator has used takes minus one that way. Here the field cannot hold a negative number,
// so the decoder wraps it to the largest value the field has: the ceiling on connected peers stops bounding
// anything, and a window measured in seconds becomes centuries. The value decodes cleanly, so nothing later
// objects.
func TestANegativeNumberWhereTheSettingCannotHoldOneIsRefused(t *testing.T) {
	configtest.Isolate(t)
	was := tmcfg.DefaultConfig().P2P.MaxConnections

	ctx := bootWithNodeFile(t, nodeFileHeader+
		"\n[p2p]\nmax-connections = -1\nsend-rate = 1234567\n", nil)

	if got := ctx.Config.P2P.MaxConnections; got != was {
		t.Errorf("the node allows %d connected peers after minus one was written, want the %d it had. "+
			"Minus one wraps to the largest value this setting can hold, which is no bound at all", got, was)
	}
	if got := ctx.Config.P2P.SendRate; got == 1234567 {
		t.Error("the value beside the refused one was applied, so the section was published in part")
	}
}
