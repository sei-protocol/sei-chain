package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestHideContractEvidence(t *testing.T) {
	k, ctx := keepertest.MockEVMKeeper()
	_, addr := keepertest.MockAddressPair()
	code := []byte{0x1, 0x2, 0x3}
	k.SetCode(ctx, addr, code)

	err := k.HideContractEvidence(ctx, addr)
	require.NoError(t, err)

	require.Nil(t, k.GetCode(ctx, addr))

	store := k.PrefixStore(ctx, types.ContractMetaKeyPrefix)
	bz := store.Get(types.ContractMetadataKey(addr))
	require.NotNil(t, bz)
	require.Equal(t, code, bz)
}
