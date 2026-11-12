package dec

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func TestQuoMatchesSDK(t *testing.T) {
	rng := rand.New(rand.NewSource(11)) //nolint:gosec // deterministic test data
	for range 2_000 {
		total := big.NewInt(rng.Int63n(1<<40) + 1)
		power := new(big.Int).Rand(rng, total)
		want := sdk.NewIntFromBigInt(power).ToDec().Quo(sdk.NewIntFromBigInt(total).ToDec())
		require.Equal(t, want.BigInt().String(), Quo(FromInt(power), FromInt(total)).String(), "%s / %s", power, total)
	}
}
