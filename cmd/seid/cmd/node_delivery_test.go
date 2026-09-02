package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// The kind the node's own file is rendered with, so the two agree. A disagreement stops the delivery
// outright, which is its own case rather than the backdrop for every other one.
var nodeFileHeader = "schema_version = 1\nnode_mode = \"" + tmcfg.DefaultConfig().Mode + "\"\n"

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

// TestAnUnwrittenKeyTakesTheDeclaredValue is what makes sei.toml the configuration rather than a patch.
//
// A section read by a decode holds what its own file said, and that file is not consulted for a declared
// key under this manager. So a key sei.toml does not mention arrives at the value this binary declares for
// the kind of node this is, and what config.toml said about it does not survive.
//
// The fixture turns the key on in the node's own file, where the declared value is off, so the two
// disagree. Without that they agree and there is nothing to observe.
//
// This is the change with the largest consequence for an existing node, which is why a path that renders
// sei.toml from the files a node already has must land before this is switched on anywhere.
func TestAnUnwrittenKeyTakesTheDeclaredValue(t *testing.T) {
	configtest.Isolate(t)
	ctx := bootWithNodeFile(t, nodeFileHeader+"\n[mempool]\nsize = 4321\n", func(live *tmcfg.Config) {
		live.Instrumentation.Prometheus = true
	})

	if got := ctx.Config.Mempool.Size; got != 4321 {
		t.Fatalf("the written key arrived as %d, so this test cannot tell the two cases apart", got)
	}
	if ctx.Config.Instrumentation.Prometheus {
		t.Error("the node's own file turned the metrics listener on, sei.toml said nothing about it, and " +
			"the node still runs with it on. config.toml is still answering for a declared key, so " +
			"sei.toml is a patch on the configuration rather than the configuration")
	}
}

// TestARefusedValueLeavesItsSectionAlone is the promise that makes this safe to enable.
//
// A decoder gathers errors and keeps going, so a value it refuses partway leaves its target holding some
// new values and some old, with nothing to compare against. The delivery decodes into a copy and publishes
// into the live one only once the whole section decodes, so a refused value leaves the section exactly as
// the node had it.
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
		t.Fatalf("--%s is not on this command, so nothing here can carry the key: %v. This is the only "+
			"guard on a flag name and its key being spelled differently, and skipping would leave it "+
			"passing while measuring nothing", flag, err)
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

// TestNoDeliveryCarriesADeclaredDefault is the one rule both deliveries depend on, named.
//
// A resolution answers for every declared key, and a declared value is what a provisioning command writes
// for a kind of node rather than what any particular node runs. Delivering one would replace a setting an
// operator never mentioned, on every boot, for every key their file omits. Both deliveries avoid that by
// narrowing to the keys a source supplied, and each does it in its own function.
//
// That makes it a rule three call sites remember rather than one a single function enforces, which is the
// shape this repository's own guidance says to guard. Until the narrowing has one home, this is the guard:
// it boots with a file that supplies one key and asserts that nothing else moved anywhere, across both
// deliveries and every mode.
func TestNoDeliveryCarriesADeclaredDefault(t *testing.T) {
	for _, mode := range registry.Modes() {
		t.Run(string(mode), func(t *testing.T) {
			configtest.Isolate(t)

			// The node's own file records the same kind, because a disagreement stops the delivery and
			// this measures what a delivery carries.
			runsAs := func(cfg *tmcfg.Config) { cfg.Mode = string(mode) }

			// What the node holds before any file supplies anything.
			bare := bootWithNodeFile(t, "schema_version = 1\nnode_mode = \""+string(mode)+"\"\n", runsAs)
			keys := everyDeclaredKey()
			// The node's own configuration holds the decoded sections and nothing else, so those are the
			// only keys it can be read for. Handing it the rest compares an absent value with an absent
			// value, which reports that every one of them is unchanged whatever the delivery did.
			decodedKeys := keysADecodeDelivers()
			before := configmanager.DescribeForTest(t, bare.Config, decodedKeys)
			beforeSource := map[string]string{}
			for _, key := range keys {
				beforeSource[key] = fmt.Sprint(bare.Viper.Get(key))
			}

			// The same node, with a file supplying exactly one key.
			after := bootWithNodeFile(t, "schema_version = 1\nnode_mode = \""+string(mode)+"\"\n"+
				"\n[mempool]\nsize = 4321\n", runsAs)
			if got := after.Config.Mempool.Size; got != 4321 {
				t.Fatalf("the one supplied key arrived as %d, so nothing was delivered and this test "+
					"would pass for a delivery that does nothing", got)
			}

			afterDescribed := configmanager.DescribeForTest(t, after.Config, decodedKeys)
			for _, key := range decodedKeys {
				if key == "mempool.size" {
					continue
				}
				if afterDescribed[key] != before[key] {
					t.Errorf("%s reads %q in the node's configuration after a file that supplies only "+
						"mempool.size, and %q before. A declared default was delivered over a setting "+
						"nobody wrote", key, afterDescribed[key], before[key])
				}
			}
			for _, key := range keys {
				if key == "mempool.size" {
					continue
				}
				if got := fmt.Sprint(after.Viper.Get(key)); got != beforeSource[key] {
					t.Errorf("%s reads %q in the source and %q before it. A declared default was "+
						"installed for a key nobody wrote", key, got, beforeSource[key])
				}
			}
		})
	}
}

// everyDeclaredKey returns every key any registered section declares, sorted.
func everyDeclaredKey() []string {
	keys := registry.Keys()
	sort.Strings(keys)
	return keys
}

// TestANumberTooLargeForTheSettingIsRefused reaches the same failure as a negative one, from the other side.
//
// A number the field cannot hold is not refused by the decoder. It saturates, so the largest value the field
// has becomes what the setting means. That is precisely the outcome the guard beside this refuses a minus
// one for, and a number written far too high arrives at it without passing anything that objects.
func TestANumberTooLargeForTheSettingIsRefused(t *testing.T) {
	configtest.Isolate(t)
	was := tmcfg.DefaultConfig().P2P.MaxConnections

	ctx := bootWithNodeFile(t, nodeFileHeader+"\n[p2p]\nmax-connections = 1e20\nmax-incoming-connection-attempts = 7\n",
		nil)
	if got := ctx.Config.P2P.MaxConnections; got != was {
		t.Errorf("the node runs a connection ceiling of %d after 1e20 was written, want the %d it had. A "+
			"number that size saturates to the largest the field holds, so the ceiling bounds nothing",
			got, was)
	}
	if got := ctx.Config.P2P.MaxIncomingConnectionAttempts; got == 7 {
		t.Error("the value beside the refused one was applied, so the section was published in part")
	}
}

// TestAFractionWhereTheSettingHoldsWholeNumbersIsRefused covers a value that decodes to a different number.
//
// A fraction is truncated rather than rounded, so a mempool written as one and a half decodes to one.
// Nothing later objects, because by the time anything reads it the value is a whole number and a perfectly
// ordinary one.
func TestAFractionWhereTheSettingHoldsWholeNumbersIsRefused(t *testing.T) {
	configtest.Isolate(t)
	was := tmcfg.DefaultConfig().Mempool.Size

	ctx := bootWithNodeFile(t, nodeFileHeader+"\n[mempool]\nsize = 1.5\n", nil)
	if got := ctx.Config.Mempool.Size; got != was {
		t.Errorf("the node runs a mempool of %d after 1.5 was written, want the %d it had. The fraction "+
			"is dropped rather than rounded, so the node would carry a single transaction", got, was)
	}
}

// keysADecodeDelivers returns every key the decoded sections own, sorted.
//
// Through the one accessor that answers both halves from a single read of the registry, so a test cannot
// see a registry the boot did not.
func keysADecodeDelivers() []string {
	_, keys := registry.ResolvedAndOwnedByDecodedSections(registry.Resolved{})
	return keys
}

// TestAValueTheNodesOwnRulesRejectIsRefused covers what decodes cleanly, means what it says, and still
// breaks the node.
//
// A negative transaction-size ceiling is a valid int, so nothing about its shape is wrong. The node then
// measures every transaction against it and finds all of them larger, so it accepts none. The node's own
// rules refuse exactly this, and they are checked on the rehearsal copy because that is the one place a
// copy exists to check.
func TestAValueTheNodesOwnRulesRejectIsRefused(t *testing.T) {
	configtest.Isolate(t)
	was := tmcfg.DefaultConfig().Mempool.MaxTxBytes

	ctx := bootWithNodeFile(t, nodeFileHeader+"\n[mempool]\nmax-tx-bytes = -1\nsize = 4321\n", nil)
	if got := ctx.Config.Mempool.MaxTxBytes; got != was {
		t.Errorf("the node runs a transaction-size ceiling of %d after -1 was written, want the %d it "+
			"had. Every transaction measures larger than a negative ceiling, so the node would accept "+
			"none of them", got, was)
	}
	if got := ctx.Config.Mempool.Size; got == 4321 {
		t.Error("the value beside the refused one was applied, so the section was published in part")
	}
}

// TestASectionLandsOnANodeAlreadyFailingItsOwnRules covers a node the boot never validated.
//
// A boot does not apply the node's rules to an existing config.toml, so a node can already hold a value
// they reject. A section is held to its own rules, so a failure somewhere else is never asked about, and
// refusing on one would blame this section for a failure it did not cause.
func TestASectionLandsOnANodeAlreadyFailingItsOwnRules(t *testing.T) {
	configtest.Isolate(t)

	// A node whose own file already holds a value its rules reject, in a section nobody is changing.
	alreadyInvalid := func(c *tmcfg.Config) { c.Mempool.MaxTxBytes = -1 }

	ctx := bootWithNodeFile(t, nodeFileHeader+"\n[p2p]\nmax-incoming-connection-attempts = 7\n", alreadyInvalid)
	if got := ctx.Config.P2P.MaxIncomingConnectionAttempts; got != 7 {
		t.Errorf("the written value is %d on a node that already failed its own rules, want 7. A section "+
			"cannot be held to rules the node was already breaking, or nothing can ever be delivered to "+
			"it again", got)
	}
}

// TestAnEmptyValueIsRefusedRatherThanReadAsZero covers the shape a template leaves behind.
//
// A value is decoded weakly, so an empty string becomes the zero of whatever numeric field it lands in and
// nothing objects: not the decoder, and not the node's own rules. The written line reads as a setting an
// operator left alone and decodes as one they turned off. A ceiling of zero on connections is a node with no
// peers, and a transaction size of zero is a node that accepts nothing.
//
// It is also what an unfilled variable renders as, so it arrives on a node nobody edited by hand.
func TestAnEmptyValueIsRefusedRatherThanReadAsZero(t *testing.T) {
	for _, tc := range []struct {
		section, key string
		reads        func(*tmcfg.Config) int
	}{
		{"mempool", "size", func(c *tmcfg.Config) int { return c.Mempool.Size }},
		{"mempool", "max-tx-bytes", func(c *tmcfg.Config) int { return c.Mempool.MaxTxBytes }},
		{"mempool", "cache-size", func(c *tmcfg.Config) int { return c.Mempool.CacheSize }},
		{"statesync", "fetchers", func(c *tmcfg.Config) int { return int(c.StateSync.Fetchers) }},
	} {
		t.Run(tc.section+"."+tc.key, func(t *testing.T) {
			configtest.Isolate(t)
			was := tc.reads(tmcfg.DefaultConfig())
			if was == 0 {
				t.Fatalf("%s.%s declares zero, so this case cannot tell a refusal from a delivery",
					tc.section, tc.key)
			}

			ctx := bootWithNodeFile(t, nodeFileHeader+
				"\n["+tc.section+"]\n"+tc.key+" = \"\"\n", nil)

			if got := tc.reads(ctx.Config); got != was {
				t.Errorf("%s.%s reads %d after being written empty, want the %d it declares. An empty "+
					"value decoded to zero and turned the setting off, and nothing refused it",
					tc.section, tc.key, got, was)
			}
		})
	}
}

// TestASectionBreakingItsOwnRulesIsRefusedEvenOnAnAlreadyInvalidNode is the other half of that.
//
// Holding a section to the whole configuration's rules cannot tell the two apart: the whole set stops at
// the first failing section, so on a node already failing anywhere, a value written here fails the same
// check and lands under a line saying the node was already broken.
//
// The already-invalid section has to sort after the one being delivered. Sections are delivered in order,
// so an earlier one is corrected by its own declared values before a later one is ever checked, and the
// case only shows up when the failure is still standing.
func TestASectionBreakingItsOwnRulesIsRefusedEvenOnAnAlreadyInvalidNode(t *testing.T) {
	configtest.Isolate(t)
	const held = 77

	alreadyInvalid := func(c *tmcfg.Config) {
		// statesync sorts after rpc, so this is still failing when rpc is delivered.
		c.StateSync.Enable = true
		c.StateSync.UseP2P = false
		c.StateSync.RPCServers = nil
		// A distinctive value in the section being delivered, so a refusal cannot look like a delivery.
		c.RPC.MaxSubscriptionClients = held
	}
	ctx := bootWithNodeFile(t, nodeFileHeader+"\n[rpc]\nmax-subscription-clients = -1\n", alreadyInvalid)

	if got := ctx.Config.RPC.MaxSubscriptionClients; got != held {
		t.Errorf("rpc.max-subscription-clients reads %d after a written value its own rules reject, want "+
			"the %d the node held. A failure standing in a later section made this section's own failure "+
			"read as somebody else's", got, held)
	}
}
