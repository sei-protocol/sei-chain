package evmonly

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestPrevRandao(t *testing.T) {
	blockTime := time.Unix(1_700_000_001, 250_000_000)

	// Verify: the value is pinned, since it is part of consensus.
	require.Equal(t, common.HexToHash("0x013f59ded399d312ae39256c2eaaaff7c53dc0d2c8df9bf11b0f8536b402b8d5"), PrevRandao(blockTime))
	// Verify: two blocks in the same second differ.
	require.NotEqual(t, PrevRandao(blockTime), PrevRandao(blockTime.Add(time.Millisecond)))
	// Verify: the time zone does not change the value.
	require.Equal(t, PrevRandao(blockTime), PrevRandao(blockTime.In(time.FixedZone("UTC+5", 5*60*60))))
}
