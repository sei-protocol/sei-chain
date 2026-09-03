package cmd

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// keysAGeneratedFileStatesThatNothingDeclares is every key this binary writes into a node's own
// configuration files and does not declare, with the reason it is left out.
//
// An undeclared key goes on resolving from the file that states it. Under the versioned manager that is
// the one thing sei.toml cannot reach: the resolution has nothing to say about the key, so writing that
// file changes nothing for it and neither does writing it again on every start. A node enrolled in
// automatic configuration management is held to its kind's defaults for every key but these.
//
// So each one is a decision, and the decision is recorded here rather than left as the absence of a
// registration. A key added to a template without a section to declare it fails the measurement below
// instead of quietly answering from app.toml.
//
// Where the reason belongs to a registered section, it lives with that section and the row names the
// constant holding it. Restating it here would let the two drift.
var keysAGeneratedFileStatesThatNothingDeclares = map[string]string{
	// Not a supported feature of this chain. The upstream server still starts one when the key is on, and
	// it prints that it is a beta feature not to be used in production. Pending removal.
	"rosetta.enable":     "rosetta is not a supported feature",
	"rosetta.address":    "rosetta is not a supported feature",
	"rosetta.blockchain": "rosetta is not a supported feature",
	"rosetta.network":    "rosetta is not a supported feature",
	"rosetta.offline":    "rosetta is not a supported feature",
	"rosetta.retries":    "rosetta is not a supported feature",

	// The reader that builds the server configuration from the source fills this whole section from its
	// own defaults and never asks the source for any of these keys. Measured: a written page ceiling of
	// 500 arrives as the default 1000, and a written allowlist arrives empty. Declaring one would offer a
	// setting that changes nothing, and the file's own comment already tells an operator to set one of
	// them.
	"query.disable-limits": "the server configuration reader never asks the source for this section",
	"query.trusted-cidrs":  "the server configuration reader never asks the source for this section",
	"query.max-limit":      "the server configuration reader never asks the source for this section",
	"query.max-offset":     "the server configuration reader never asks the source for this section",
	"query.max-iterations": "the server configuration reader never asks the source for this section",

	// No reader resolves it. The struct the command renders sets one value for this key and the template
	// beside it renders another as a bare literal, which is held by a test of its own.
	"wasm.lru_size": "no reader resolves it",

	// The template renders this name and the reader looks up contract_state_checks, which is the key this
	// binary declares. A value written under this name reaches nothing.
	"eth_replay.eth_replay_contract_state_checks": "the reader looks up contract_state_checks",

	// Recorded where the sections that would carry them are registered.
	"mode":                           "config/tendermintbase, statedAtTheTopOfTheFile",
	"mempool.max-batch-bytes":        "config/tendermintbase, unreadAndUnmarked",
	"mempool.pending-ttl-duration":   "config/tendermintbase, neverReachTheMempool",
	"mempool.pending-ttl-num-blocks": "config/tendermintbase, neverReachTheMempool",
	"self-remediation.p2p-no-peers-available-window-seconds": "config/tendermintbase, reachesNoReactor",
}

// TestEveryKeyAGeneratedFileStatesIsDeclaredOrRecorded closes the gap a missing registration leaves.
//
// Two sides of the same key space, and neither one is checked by anything else. A key this binary writes
// into a file and does not declare resolves from that file forever, and a key recorded here that the
// binary has started declaring makes the record claim a hole that is closed.
//
// The sections a registration excludes are covered too, because an excluded path is still written into
// the file. Excluding a path and declaring the section it sits in are different statements, and only the
// first leaves the key resolving from app.toml.
func TestEveryKeyAGeneratedFileStatesIsDeclaredOrRecorded(t *testing.T) {
	configtest.Isolate(t)
	home := aNodeRunningAs(t, registry.ModeValidator)

	stated := whatTheFilesState(t, home)
	declared := make(map[string]bool, len(registry.Keys()))
	for _, key := range registry.Keys() {
		declared[key] = true
	}

	var undeclared []string
	for _, key := range stated {
		if declared[key] {
			continue
		}
		undeclared = append(undeclared, key)
		if _, recorded := keysAGeneratedFileStatesThatNothingDeclares[key]; !recorded {
			t.Errorf("this binary writes %s into a node's own files and does not declare it, and nothing "+
				"records why. It resolves from that file whatever a sei.toml says, so a node enrolled in "+
				"automatic configuration management is not held to its kind's defaults for it", key)
		}
	}

	for key := range keysAGeneratedFileStatesThatNothingDeclares {
		if declared[key] {
			t.Errorf("%s is recorded as undeclared and this binary declares it, so the record names a "+
				"hole that is closed", key)
		}
	}

	sort.Strings(undeclared)
	if len(undeclared) != len(keysAGeneratedFileStatesThatNothingDeclares) {
		t.Errorf("measured %d undeclared keys and %d are recorded: %v",
			len(undeclared), len(keysAGeneratedFileStatesThatNothingDeclares), undeclared)
	}
}

// whatTheFilesState returns every key the node's own configuration files state, by dotted name.
//
// Read the way a lookup reads them, so the names are the ones a reader resolves rather than the shape the
// files nest them in.
func whatTheFilesState(t *testing.T, home string) []string {
	t.Helper()
	var all []string
	for _, name := range []string{"app.toml", "config.toml"} {
		v := viper.New()
		v.SetConfigFile(filepath.Join(home, "config", name))
		if err := v.ReadInConfig(); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		all = append(all, v.AllKeys()...)
	}
	sort.Strings(all)
	return all
}
