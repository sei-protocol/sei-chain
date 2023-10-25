package keeper

import (
	"math"
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestGetChainID(t *testing.T) {
	k, _, _ := MockEVMKeeper()
	require.Equal(t, int64(1), k.ChainID().Int64())
}

func TestGetVMBlockContext(t *testing.T) {
	k, _, ctx := MockEVMKeeper()
	moduleAddr := k.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	evmAddr, _ := k.GetEVMAddress(ctx, moduleAddr)
	k.DeleteAddressMapping(ctx, moduleAddr, evmAddr)
	_, err := k.GetVMBlockContext(ctx, 0)
	require.NotNil(t, err)
}

func TestGetHashFn(t *testing.T) {
	k, _, ctx := MockEVMKeeper()
	f := k.getHashFn(ctx)
	require.Equal(t, common.Hash{}, f(math.MaxInt64+1))
	require.Equal(t, common.BytesToHash(ctx.HeaderHash()), f(uint64(ctx.BlockHeight())))
	require.Equal(t, common.Hash{}, f(uint64(ctx.BlockHeight())+1))
	require.Equal(t, common.Hash{}, f(uint64(ctx.BlockHeight())-1))
}
