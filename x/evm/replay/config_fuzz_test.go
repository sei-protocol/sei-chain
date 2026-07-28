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
	f.Add(uint(0), uint8(2), "true", int64(1), true)
	f.Add(uint(1), uint8(1), "http://127.0.0.1:8545", int64(0), false)
	f.Add(uint(2), uint8(1), "/root/.ethereum/chaindata", int64(0), false)
	f.Add(uint(3), uint8(8), "", int64(0), true)
	f.Add(uint(1), uint8(10), "", int64(0), false)
	f.Add(uint(0), uint8(1), "enabled", int64(0), false)
	f.Add(uint(2), uint8(0), "", int64(0), false)

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
