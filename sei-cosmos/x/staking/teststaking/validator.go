package teststaking

import (
	"testing"

	"github.com/stretchr/testify/require"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// NewValidator is a testing helper method to create validators in tests
func NewValidator(t testing.TB, operator seitypes.ValAddress, pubKey cryptotypes.PubKey) types.Validator {
	v, err := types.NewValidator(operator, pubKey, types.Description{})
	require.NoError(t, err)
	return v
}
