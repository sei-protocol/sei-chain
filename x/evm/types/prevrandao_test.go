package types_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestPrevRandao(t *testing.T) {
	blockTime := time.Date(2026, 1, 2, 14, 28, 1, 540537286, time.UTC)
	got, err := types.PrevRandao(blockTime)
	require.NoError(t, err)
	require.Equal(t, common.HexToHash("0x753efd32f4a9d19b90b63f109cd4674b3104591ac57d57627625d92751ed8a50"), got)
}

// TestPrevRandaoHashesLocation pins that the same instant in a zero-offset zone other than
// time.UTC hashes differently, which is why callers must pass a time.UTC time.
func TestPrevRandaoHashesLocation(t *testing.T) {
	blockTime := time.Date(2026, 1, 2, 14, 28, 1, 540537286, time.UTC)
	got, err := types.PrevRandao(blockTime.In(time.FixedZone("UTC", 0)))
	require.NoError(t, err)
	require.Equal(t, common.HexToHash("0x0d178cbdc5ed1576a13c61f2b8a10242200048b6f89577b5d2532489e7312434"), got)
}
