package types

import (
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// EmptyCodeHash must match go-ethereum's empty-code hash; a mismatch would make every account
// without code read as a contract, or a contract as an EOA.
func TestEmptyCodeHashMatchesEthereum(t *testing.T) {
	require.Equal(t, ethtypes.EmptyCodeHash, EmptyCodeHash)
}
