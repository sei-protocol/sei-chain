package hashlog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeVersion(t *testing.T) {
	require.Equal(t, "v1.2.3", sanitizeVersion("v1.2.3"))
	require.Equal(t, "v1.2.3_rc1", sanitizeVersion("v1.2.3-rc1"))
	require.Equal(t, "build_123", sanitizeVersion("build 123"))
	require.Equal(t, "a_b_c", sanitizeVersion("a/b\\c"))
	require.Equal(t, "ok_", sanitizeVersion("ok,"))
	// Already-legal characters are left untouched.
	require.Equal(t, "ABCabc012._", sanitizeVersion("ABCabc012._"))
}

func TestSanitizeVersionRoundTripsThroughFileName(t *testing.T) {
	version := sanitizeVersion("v1.2.3-rc1 build")
	name := sealedFileName(5, 100, 200, version)
	first, last, parsedVersion, err := parseBlockNumbersFromFileName(name)
	require.NoError(t, err)
	require.Equal(t, uint64(100), first)
	require.Equal(t, uint64(200), last)
	require.Equal(t, version, parsedVersion)
}
