package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenesisValidation(t *testing.T) {
	genState := DefaultGenesisState()
	require.NoError(t, ValidateGenesis(genState))

	genState.Params.VotePeriod = 0
	require.Error(t, ValidateGenesis(genState))
}
