package cmd

import (
	"context"
	"path/filepath"
	"testing"

	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

func TestStartLoadsConfigAndForcesEVMOnly(t *testing.T) {
	home := t.TempDir()
	config := tmconfig.DefaultConfig()
	config.Moniker = "from-file"
	config.EVMOnly = false
	tmconfig.EnsureRoot(home)
	if err := tmconfig.WriteConfigFile(home, config); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var got *tmconfig.Config
	root := newRootCmd(home, func(_ context.Context, config *tmconfig.Config, freezeHeight uint64) error {
		got = config
		if freezeHeight != 19 {
			t.Fatalf("freeze height = %d, want 19", freezeHeight)
		}
		return nil
	})
	root.SetArgs([]string{"start", "--moniker", "from-flag", "--freeze-height", "19"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got == nil {
		t.Fatal("node runner was not called")
	}
	if !got.EVMOnly {
		t.Fatal("giga did not enable EVM-only execution")
	}
	if got.Moniker != "from-flag" {
		t.Fatalf("moniker = %q, want from-flag", got.Moniker)
	}
	if got.RootDir != home {
		t.Fatalf("root = %q, want %q", got.RootDir, home)
	}
	if got.GenesisFile() != filepath.Join(home, "config", "genesis.json") {
		t.Fatalf("genesis file = %q", got.GenesisFile())
	}
}

func TestStartReadsGigaEnvironment(t *testing.T) {
	home := t.TempDir()
	config := tmconfig.DefaultConfig()
	tmconfig.EnsureRoot(home)
	if err := tmconfig.WriteConfigFile(home, config); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("GIGA_HOME", home)
	t.Setenv("GIGA_MONIKER", "from-environment")

	var got *tmconfig.Config
	root := newRootCmd(t.TempDir(), func(_ context.Context, config *tmconfig.Config, _ uint64) error {
		got = config
		return nil
	})
	root.SetArgs([]string{"start"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got == nil {
		t.Fatal("node runner was not called")
	}
	if got.RootDir != home {
		t.Fatalf("root = %q, want %q", got.RootDir, home)
	}
	if got.Moniker != "from-environment" {
		t.Fatalf("moniker = %q, want from-environment", got.Moniker)
	}
}

func TestStartRequiresExistingConfig(t *testing.T) {
	root := newRootCmd(t.TempDir(), func(context.Context, *tmconfig.Config, uint64) error {
		t.Fatal("node runner must not be called")
		return nil
	})
	root.SetArgs([]string{"start"})
	if err := root.ExecuteContext(t.Context()); err == nil {
		t.Fatal("start succeeded without config.toml")
	}
}
