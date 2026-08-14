package replay_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	"github.com/sei-protocol/sei-chain/x/evm/replay"
)

// ethReplayKeys is the [eth_replay] section's read-site manifest. All four keys
// are guarded and checked.
//
// Two properties here are load-bearing beyond the section. eth_replay_enabled
// gates a path that dials EthRPC during app.New and panics if it is unreachable,
// so the default must stay false on a production node. And the live key for the
// state-check toggle is contract_state_checks, while the app.toml template
// renders eth_replay_contract_state_checks — a name nothing reads. The template
// key is therefore a silent no-op; this table pins the name that actually
// resolves, so a "fix" that renamed the read site would fail here rather than
// silently changing which nodes run the checks.
var ethReplayKeys = []configtest.KeySpec{
	{
		Key: "eth_replay.eth_replay_enabled", Path: "Enabled", Cast: configtest.CastBool,
		Checked: true,
		Why:     "default false; enabling makes app.New dial eth_rpc and panic if unreachable",
	},
	{
		Key: "eth_replay.eth_rpc", Path: "EthRPC", Cast: configtest.CastString,
		Checked: true,
		Why:     "default is a hardcoded third-party endpoint",
	},
	{
		Key: "eth_replay.eth_data_dir", Path: "EthDataDir", Cast: configtest.CastString,
		Checked: true,
	},
	{
		Key: "eth_replay.contract_state_checks", Path: "ContractStateChecks", Cast: configtest.CastBool,
		Checked: true,
		Why:     "the live key; the template renders eth_replay_contract_state_checks, which nothing reads",
	},
}

func readETHReplay(opts configtest.AppOpts) (any, error) { return replay.ReadConfig(opts) }

func FuzzReadConfig(f *testing.F) {
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
	seeds.AddRow(uint(0), fuzzing.KindBool, "true", int64(1), true)
	seeds.AddRow(uint(1), fuzzing.KindString, "http://127.0.0.1:8545", int64(0), false)
	// A path an operator would relocate chaindata to, and deliberately not
	// "/root/.ethereum/chaindata": that is the in-code default, so a seed carrying it
	// resolves EthDataDir exactly as the absent key does and the row would hold for a
	// reader that never looks eth_replay.eth_data_dir up.
	seeds.AddRow(uint(2), fuzzing.KindString, "/mnt/eth/chaindata", int64(0), false)
	seeds.AddRow(uint(3), fuzzing.KindBoolString, "", int64(0), true)
	seeds.AddRow(uint(1), fuzzing.KindAnySlice, "", int64(0), false)
	seeds.AddRow(uint(0), fuzzing.KindString, "enabled", int64(0), false)
	// contract_state_checks is a bool cast, so a word reaches its error path.
	seeds.AddRow(uint(3), fuzzing.KindString, "sometimes", int64(0), false)
	// eth_data_dir is a string cast, and cast.ToStringE accepts every scalar, so only a non-scalar
	// shape is malformed for it.
	seeds.AddRow(uint(2), fuzzing.KindStringSlice, "", int64(0), false)
	seeds.AddRow(uint(2), fuzzing.KindNil, "", int64(0), false)

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "eth_replay", readETHReplay, ethReplayKeys, seeds)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(ethReplayKeys, keyIdx)
		configtest.CheckRow(t, "eth_replay", readETHReplay, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline, most
// importantly that replay stays disabled when the section is absent.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	configtest.CheckAbsent(t, "eth_replay", readETHReplay, replay.DefaultConfig)
}

// TestTemplateKeyIsInert records the divergence between the templated key name
// and the key the reader looks up. Setting only the templated spelling must leave
// the resolved value at its default — the toggle an operator edits in a generated
// app.toml does nothing. This is pinned, not fixed: renaming either side changes
// which nodes run contract state checks, so it belongs to the migration
// framework, not to a drive-by rename.
func TestTemplateKeyIsInert(t *testing.T) {
	cfg, err := replay.ReadConfig(configtest.AppOpts{
		"eth_replay.eth_replay_contract_state_checks": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ContractStateChecks {
		t.Fatal("the templated eth_replay_contract_state_checks key resolved a value; " +
			"if the read site was renamed on purpose, update this test and ship a migration")
	}
}

// TestDefaultsMatchTheRecordedValues pins the eth_replay defaults themselves.
//
// The absent-keys row above proves the reader returns the declared defaults; it cannot prove
// which values those are, because both sides of that comparison come from this package. This
// compares them against testdata/eth_replay.golden, an independent recording, so a default that
// moves shows the new value in a diff instead of passing silently.
func TestDefaultsMatchTheRecordedValues(t *testing.T) {
	configtest.CheckDefaults(t, "eth_replay", replay.DefaultConfig)
}

// TestKeyNamesMatchTheRecordedNames pins the four key names themselves.
//
// This section already carries the cost of an unrecorded rename: the app.toml template renders
// eth_replay_contract_state_checks and the reader looks up contract_state_checks, so the key an
// operator edits in a generated file does nothing. The record holds the spelling that
// resolves, which is the one a fix has to migrate from rather than quietly replace.
func TestKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "eth_replay", ethReplayKeys)
}

// TestManifestNamesEveryField enforces the claim ethReplayKeys makes about itself: that it names
// every key the reader looks up. Left as prose the claim can drift, and it is the artifact a
// replacement implementation reads as this section's contract.
func TestManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "eth_replay", replay.DefaultConfig, ethReplayKeys)
}

// TestWiringMatchesTheRecord pins which checks each of this package's sections is wired to.
//
// Every other check here reports a change to what it asserts. None reports a check being removed, so
// this records the wiring and fails when it thins out.
func TestWiringMatchesTheRecord(t *testing.T) {
	configtest.CheckWiring(t)
}

// TestNoExperimentalKeyShadowsThisSection is this section's half of the experimental collision
// check.
//
// It lives here because a KeySpec manifest is an unexported package-level var in a _test.go file,
// so this is the only test binary that can see both this section's live keys and the experimental
// registry. A test in cmd/seid/cmd cannot reference these vars at all.
//
// A declared experimental name is the path the key occupies after promotion, so a name equal to one
// of these keys would put two declarations on one path. The check compares whole spellings only;
// a semantic duplicate under a different name stays a review question.
func TestNoExperimentalKeyShadowsThisSection(t *testing.T) {
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, "eth_replay", ethReplayKeys)
}
