package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestChargeRentForGas(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.SetParams(ctx, types.Params{SudoCallGasPrice: sdk.NewDecWithPrec(1, 1), PriceSnapshotRetention: 1})
	err := keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		CodeId:       1,
		RentBalance:  1000000,
	})
	require.Nil(t, err)
	err = keeper.ChargeRentForGas(ctx, keepertest.TestContract, 5000000)
	require.Nil(t, err)
	contract, err := keeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, uint64(500000), contract.RentBalance)
	err = keeper.ChargeRentForGas(ctx, keepertest.TestContract, 6000000)
	require.NotNil(t, err)
	contract, err = keeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, uint64(0), contract.RentBalance)
}
