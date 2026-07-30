package evmrpc

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/export"
	"github.com/stretchr/testify/require"
)

func TestValidateStateOverrides(t *testing.T) {
	addr := common.HexToAddress("0x1")
	slots := func(n int) map[common.Hash]common.Hash {
		m := make(map[common.Hash]common.Hash, n)
		for i := 0; i < n; i++ {
			m[common.BytesToHash([]byte{byte(i)})] = common.Hash{}
		}
		return m
	}

	require.NoError(t, validateStateOverrides(nil, 10, 10))

	// within limits
	ok := export.StateOverride{addr: {State: slots(5)}}
	require.NoError(t, validateStateOverrides(&ok, 10, 10))

	// too many slots via state
	tooManyState := export.StateOverride{addr: {State: slots(11)}}
	require.Error(t, validateStateOverrides(&tooManyState, 10, 10))

	// too many slots via stateDiff
	tooManyDiff := export.StateOverride{addr: {StateDiff: slots(11)}}
	require.Error(t, validateStateOverrides(&tooManyDiff, 10, 10))

	// too many accounts
	tooManyAccounts := export.StateOverride{
		common.HexToAddress("0x1"): {},
		common.HexToAddress("0x2"): {},
	}
	require.Error(t, validateStateOverrides(&tooManyAccounts, 1, 10))

	// zero disables limit
	require.NoError(t, validateStateOverrides(&tooManyState, 0, 0))
}
