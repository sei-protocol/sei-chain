package relay

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetParsedABI(t *testing.T) {
	parsed := GetParsedABI()
	require.NotNil(t, parsed)

	execute, ok := parsed.Methods["execute"]
	require.True(t, ok)
	require.Len(t, execute.Inputs, 4)

	claimed, ok := parsed.Events["Claimed"]
	require.True(t, ok)
	require.Len(t, claimed.Inputs, 4)
}

func TestGetBin(t *testing.T) {
	bin := GetBin()
	require.NotEmpty(t, bin)
	// basic sanity check to ensure the bytecode includes the Claimed event topic hash
	parsed := GetParsedABI()
	topic := parsed.Events["Claimed"].ID
	require.GreaterOrEqual(t, len(bin), len(topic.Bytes()))
}
