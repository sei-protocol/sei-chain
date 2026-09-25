package config

import (
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/stretchr/testify/require"
)

// TestTheDeclaredKeysAreTheKeysThisReaderResolves holds the declaration against the reader.
func TestTheDeclaredKeysAreTheKeysThisReaderResolves(t *testing.T) {
	for _, defect := range registry.Defects() {
		require.NotEqual(t, SectionName, defect.Section, "%s was refused: %v", SectionName, defect.Err)
	}
	section, ok := registry.Lookup(SectionName)
	require.True(t, ok, "%s is not registered, so nothing resolves its keys", SectionName)

	want := []string{
		FlagStorageMode,
		FlagStorageReceipts,
		FlagStorageRollbackWindow,
		FlagStorageLookbackWindow,
		FlagStoragePruneInterval,
		FlagStorageCheckpointTimeInterval,
		FlagStorageCheckpointBlockInterval,
		FlagExecutionMinGasPrice,
		FlagExecutionOCCWorkers,
		FlagExecutionParseWorkers,
		FlagExecutionBlockResultPoolSize,
		FlagExecutionHTTPPort,
	}
	sort.Strings(want)
	require.Equal(t, want, section.Keys)
}

// TestTheDefaultsAreWhatTheNodeAlreadyRuns keeps the section from restating the values by hand.
func TestTheDefaultsAreWhatTheNodeAlreadyRuns(t *testing.T) {
	for _, mode := range registry.Modes() {
		got, ok := defaults(mode).(Config)
		require.True(t, ok, "mode %q: defaults returned %T, want Config", mode, defaults(mode))
		require.Equal(t, DefaultConfig, got, "mode %q", mode)
	}
}
