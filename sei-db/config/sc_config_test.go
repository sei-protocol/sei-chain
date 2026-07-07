package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSCWriteMode(t *testing.T) {
	parsed, err := ParseSCWriteMode("cosmos_only")
	require.NoError(t, err)
	require.Equal(t, MemiavlOnly, parsed)

	parsed, err = ParseSCWriteMode("migrate_evm")
	require.NoError(t, err)
	require.Equal(t, MigrateEVM, parsed)

	_, err = ParseSCWriteMode("bogus")
	require.Error(t, err)
}
