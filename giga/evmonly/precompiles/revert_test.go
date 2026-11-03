package precompiles

import (
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

func TestRevertEncodesErrorString(t *testing.T) {
	out, err := Revert("use something else")
	require.True(t, err == vm.ErrExecutionReverted, "the EVM refunds unused gas only for the unwrapped sentinel") //nolint:errorlint // identity is the contract
	reason, err := abi.UnpackRevert(out)
	require.NoError(t, err)
	require.Equal(t, "use something else", reason)
}
