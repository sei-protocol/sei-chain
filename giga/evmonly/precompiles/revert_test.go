package precompiles

import (
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

func TestRevertEncodesErrorString(t *testing.T) {
	out, err := Revert("use something else")
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	reason, err := abi.UnpackRevert(out)
	require.NoError(t, err)
	require.Equal(t, "use something else", reason)
}
