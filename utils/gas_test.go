package utils_test

import (
	"testing"

	sdkstoretypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/stretchr/testify/require"
)

func TestNewGasMeterWithMultiplier(t *testing.T) {
	ctx := sdk.Context{}
	n, d := utils.NewGasMeterWithMultiplier(ctx, 10).Multiplier()
	require.Equal(t, uint64(1), n)
	require.Equal(t, uint64(1), d)
	ctx = ctx.WithGasMeter(sdkstoretypes.NewMultiplierGasMeter(10, 3, 4))
	n, d = utils.NewGasMeterWithMultiplier(ctx, 10).Multiplier()
	require.Equal(t, uint64(3), n)
	require.Equal(t, uint64(4), d)
}

func TestNewInfiniteGasMeterWithMultiplier(t *testing.T) {
	ctx := sdk.Context{}
	n, d := utils.NewInfiniteGasMeterWithMultiplier(ctx).Multiplier()
	require.Equal(t, uint64(1), n)
	require.Equal(t, uint64(1), d)
	ctx = ctx.WithGasMeter(sdkstoretypes.NewMultiplierGasMeter(10, 3, 4))
	n, d = utils.NewInfiniteGasMeterWithMultiplier(ctx).Multiplier()
	require.Equal(t, uint64(3), n)
	require.Equal(t, uint64(4), d)
}
