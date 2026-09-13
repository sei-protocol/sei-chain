package replay_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/x/evm/replay"
)

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline: no [eth_replay]
// section means replay stays disabled. That default is load-bearing — enabling it
// makes app.New dial eth_rpc and panic if the endpoint is unreachable. Both sides
// move together when a default changes, so this asserts the reader's behavior
// rather than the values themselves.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	cfg, err := replay.ReadConfig(configtest.AppOpts{})
	if err != nil {
		t.Fatalf("an absent [eth_replay] section must read cleanly, got %v", err)
	}
	if cfg != replay.DefaultConfig {
		t.Fatalf("an absent [eth_replay] section resolved to %+v, want the declared defaults %+v",
			cfg, replay.DefaultConfig)
	}
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
