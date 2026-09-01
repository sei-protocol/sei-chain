package giga

import (
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// EmptyCodeHash is written out byte by byte rather than imported, so it is held against the value it
// was copied from. A wrong constant would make every account without code read as a contract.
func TestEmptyCodeHashMatchesEthereum(t *testing.T) {
	require.Equal(t, Hash(ethtypes.EmptyCodeHash), EmptyCodeHash)
}
