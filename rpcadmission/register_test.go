package rpcadmission

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

	want := []string{flagGlobalLimit}
	for _, class := range allMethodClasses {
		want = append(want, classLimitsPath+string(class), classTimeoutsPath+string(class))
	}
	sort.Strings(want)
	require.Equal(t, want, section.Keys)
}

func TestEveryKeyResolvesToItsDefault(t *testing.T) {
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode, registry.Sources{})
		require.NoError(t, err)
		require.Equal(t, DefaultConfig.GlobalLimit, resolved.Values[flagGlobalLimit])
		for _, setting := range limitSettings(&DefaultConfig.ClassLimits) {
			require.Equal(t, *setting.value, resolved.Values[classLimitsPath+string(setting.class)])
		}
		for _, setting := range timeoutSettings(&DefaultConfig.ClassTimeouts) {
			require.Equal(t, *setting.value, resolved.Values[classTimeoutsPath+string(setting.class)])
		}
	}
}
