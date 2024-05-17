package keeper_test

import (
	"math"
	"testing"

	"github.com/cosmos/cosmos-sdk/store/prefix"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
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
	err = keeper.ChargeRentForGas(ctx, keepertest.TestContract, 5000000, 0)
	require.Nil(t, err)
	contract, err := keeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, uint64(500000), contract.RentBalance)
	err = keeper.ChargeRentForGas(ctx, keepertest.TestContract, 6000000, 0)
	require.NotNil(t, err)
	contract, err = keeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, uint64(0), contract.RentBalance)
	err = keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		CodeId:       1,
		RentBalance:  1000000,
	})
	require.Nil(t, err)
	err = keeper.ChargeRentForGas(ctx, keepertest.TestContract, 5000000, 4000000)
	require.Nil(t, err)
	contract, err = keeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, uint64(900000), contract.RentBalance)
	err = keeper.ChargeRentForGas(ctx, keepertest.TestContract, 5000000, 6000000)
	require.Nil(t, err)
	contract, err = keeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, uint64(900000), contract.RentBalance)

	// delete contract
	keeper.DeleteContract(ctx, keepertest.TestContract)
	_, err = keeper.GetContract(ctx, keepertest.TestContract)
	require.NotNil(t, err)
}

func TestGetContractInfo(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.SetParams(ctx, types.Params{SudoCallGasPrice: sdk.NewDecWithPrec(1, 1), PriceSnapshotRetention: 1})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		CodeId:       1,
		RentBalance:  1000000,
	})
	// Successfully get a contract
	contract, err := keeper.GetContract(ctx, keepertest.TestContract)
	require.Equal(t, uint64(1000000), contract.RentBalance)
	require.Equal(t, uint64(1), contract.CodeId)
	require.Equal(t, keepertest.TestAccount, contract.Creator)

	// Getting a non exist contract should throw error for contract not exist
	_, err = keeper.GetContract(ctx, keepertest.TestContract2)
	require.Error(t, err)
	require.Equal(t, err, types.ErrContractNotExists)

	// Getting a corrupted record should throw error for unable to parse
	store := prefix.NewStore(
		ctx.KVStore(keeper.GetStoreKey()),
		[]byte("x-wasm-contract"),
	)
	bz := []byte("bad_contract")
	store.Set(types.ContractKey(keepertest.TestContract), bz)
	_, err = keeper.GetContract(ctx, keepertest.TestContract)
	require.Error(t, err)
	require.Equal(t, err, types.ErrParsingContractInfo)
}

func TestGetAllContractInfo(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.SetParams(ctx, types.Params{SudoCallGasPrice: sdk.NewDecWithPrec(1, 1), PriceSnapshotRetention: 1})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		CodeId:       1,
		RentBalance:  1000000,
	})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount2,
		ContractAddr: keepertest.TestContract2,
		CodeId:       2,
		RentBalance:  1000000,
	})
	contracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, uint64(1000000), contracts[0].RentBalance)
	require.Equal(t, uint64(1000000), contracts[1].RentBalance)
	require.Equal(t, uint64(1), contracts[0].CodeId)
	require.Equal(t, uint64(2), contracts[1].CodeId)
	require.Equal(t, keepertest.TestAccount, contracts[0].Creator)
	require.Equal(t, keepertest.TestContract, contracts[0].ContractAddr)
	require.Equal(t, keepertest.TestAccount2, contracts[1].Creator)
	require.Equal(t, keepertest.TestContract2, contracts[1].ContractAddr)
}

func TestGetContractGasLimit(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	contractAddr := sdk.MustAccAddressFromBech32("sei1suhgf5svhu4usrurvxzlgn54ksxmn8gljarjtxqnapv8kjnp4nrsgshtdj")
	keeper.SetParams(ctx, types.Params{SudoCallGasPrice: sdk.NewDecWithPrec(1, 1), PriceSnapshotRetention: 1})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: "sei1suhgf5svhu4usrurvxzlgn54ksxmn8gljarjtxqnapv8kjnp4nrsgshtdj",
		CodeId:       1,
		RentBalance:  1000000,
	})
	gasLimit, err := keeper.GetContractGasLimit(ctx, contractAddr)
	require.Nil(t, err)
	// verify that cap is applied in the event of excess rent present
	require.Equal(t, dexkeeper.ContractMaxSudoGas, gasLimit)

	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: "sei1suhgf5svhu4usrurvxzlgn54ksxmn8gljarjtxqnapv8kjnp4nrsgshtdj",
		CodeId:       1,
		RentBalance:  20000,
	})
	gasLimit, err = keeper.GetContractGasLimit(ctx, contractAddr)
	require.Nil(t, err)
	// verify that cap is applied in the event of excess rent present
	require.Equal(t, uint64(200000), gasLimit)

	params := keeper.GetParams(ctx)
	params.SudoCallGasPrice = sdk.NewDecWithPrec(1, 1) // 0.1
	keeper.SetParams(ctx, params)
	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: "sei1suhgf5svhu4usrurvxzlgn54ksxmn8gljarjtxqnapv8kjnp4nrsgshtdj",
		CodeId:       1,
		RentBalance:  math.MaxUint64,
	})
	gasLimit, err = keeper.GetContractGasLimit(ctx, contractAddr)
	require.Nil(t, err)
	// max uint64 / 0.1 would cause overflow so we cap it at max allowed sudo gas
	require.Equal(t, dexkeeper.ContractMaxSudoGas, gasLimit)
}

func TestGetRentsForContracts(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	addr := "sei1suhgf5svhu4usrurvxzlgn54ksxmn8gljarjtxqnapv8kjnp4nrsgshtdj"
	require.Equal(t, 0, len(keeper.GetRentsForContracts(ctx, []string{addr})))

	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: addr,
		CodeId:       1,
		RentBalance:  100,
	})
	require.Equal(t, map[string]uint64{addr: uint64(100)}, keeper.GetRentsForContracts(ctx, []string{addr}))
}

func TestClearDependenciesForContract(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)

	// no dependency whatsoever
	contract := types.ContractInfoV2{
		ContractAddr: keepertest.TestContract,
	}
	keeper.SetContract(ctx, &contract)
	require.NotPanics(t, func() { keeper.ClearDependenciesForContract(ctx, contract) })

	// has upstreams
	contract = types.ContractInfoV2{
		ContractAddr:            keepertest.TestContract,
		NumIncomingDependencies: 2,
	}
	keptContract := types.ContractInfoV2{
		ContractAddr:            "sei1yum4v0v5l92jkxn8xpn9mjg7wuldk784ctg424ue8gqvdp88qzlqt6zc4h",
		NumIncomingDependencies: 2,
	}
	upA := types.ContractInfoV2{
		ContractAddr: "sei105y5ssrsr8p8erkteagrguea6wcdgehlaamfup4lhrlm0y6eyhdsckcxdh",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency:              keepertest.TestContract,
				ImmediateYoungerSibling: "sei1y8ghk8q8d2rswrf3gv7hv2lfsewu8tvp6ysnlkzspu7k0aqkthdqwdqvk0",
			},
			{
				Dependency:              "sei1yum4v0v5l92jkxn8xpn9mjg7wuldk784ctg424ue8gqvdp88qzlqt6zc4h",
				ImmediateYoungerSibling: "sei193dzcmy7lwuj4eda3zpwwt9ejal00xva0vawcvhgsyyp5cfh6jyqj2vsuv",
			},
		},
	}
	upB := types.ContractInfoV2{
		ContractAddr: "sei1y8ghk8q8d2rswrf3gv7hv2lfsewu8tvp6ysnlkzspu7k0aqkthdqwdqvk0",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency:            keepertest.TestContract,
				ImmediateElderSibling: "sei105y5ssrsr8p8erkteagrguea6wcdgehlaamfup4lhrlm0y6eyhdsckcxdh",
			},
		},
	}
	upC := types.ContractInfoV2{
		ContractAddr: "sei193dzcmy7lwuj4eda3zpwwt9ejal00xva0vawcvhgsyyp5cfh6jyqj2vsuv",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency:            "sei1yum4v0v5l92jkxn8xpn9mjg7wuldk784ctg424ue8gqvdp88qzlqt6zc4h",
				ImmediateElderSibling: "sei105y5ssrsr8p8erkteagrguea6wcdgehlaamfup4lhrlm0y6eyhdsckcxdh",
			},
		},
	}
	keeper.SetContract(ctx, &contract)
	keeper.SetContract(ctx, &keptContract)
	keeper.SetContract(ctx, &upA)
	keeper.SetContract(ctx, &upB)
	keeper.SetContract(ctx, &upC)
	require.NotPanics(t, func() { keeper.ClearDependenciesForContract(ctx, contract) })
	upA, err := keeper.GetContract(ctx, upA.ContractAddr)
	require.Nil(t, err)
	require.Equal(t, 1, len(upA.Dependencies))
	upB, err = keeper.GetContract(ctx, upB.ContractAddr)
	require.Nil(t, err)
	require.Equal(t, 0, len(upB.Dependencies))
	upC, err = keeper.GetContract(ctx, upC.ContractAddr)
	require.Nil(t, err)
	require.Equal(t, 1, len(upC.Dependencies))

	// has downstreams
	contract = types.ContractInfoV2{
		ContractAddr: keepertest.TestContract,
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency:              "sei1ehyucudueas79h0zwufcnxtv7s2sfmwc6rt0v0hzczdgvyr3p56qhprg6n",
				ImmediateElderSibling:   "sei1uyprmp0lu8w8z8kwxp7mxanrtrgn4lp7j557pxe4v8sczzdzl7ysk832hh",
				ImmediateYoungerSibling: "sei1yum4v0v5l92jkxn8xpn9mjg7wuldk784ctg424ue8gqvdp88qzlqt6zc4h",
			}, {
				Dependency: "sei1n23ymwg2y7m55x5vwf2qk0als9cr592q4uc5de08c6qmaeryet4qye4w77",
			},
		},
	}
	keptContractA := types.ContractInfoV2{
		ContractAddr: "sei1yum4v0v5l92jkxn8xpn9mjg7wuldk784ctg424ue8gqvdp88qzlqt6zc4h",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency:            "sei1ehyucudueas79h0zwufcnxtv7s2sfmwc6rt0v0hzczdgvyr3p56qhprg6n",
				ImmediateElderSibling: keepertest.TestContract,
			},
		},
	}
	keptContractB := types.ContractInfoV2{
		ContractAddr: "sei1uyprmp0lu8w8z8kwxp7mxanrtrgn4lp7j557pxe4v8sczzdzl7ysk832hh",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency:              "sei1ehyucudueas79h0zwufcnxtv7s2sfmwc6rt0v0hzczdgvyr3p56qhprg6n",
				ImmediateYoungerSibling: keepertest.TestContract,
			},
		},
	}
	downA := types.ContractInfoV2{
		ContractAddr:            "sei1ehyucudueas79h0zwufcnxtv7s2sfmwc6rt0v0hzczdgvyr3p56qhprg6n",
		NumIncomingDependencies: 3,
	}
	downB := types.ContractInfoV2{
		ContractAddr:            "sei1n23ymwg2y7m55x5vwf2qk0als9cr592q4uc5de08c6qmaeryet4qye4w77",
		NumIncomingDependencies: 1,
	}
	keeper.SetContract(ctx, &contract)
	keeper.SetContract(ctx, &keptContractA)
	keeper.SetContract(ctx, &keptContractB)
	keeper.SetContract(ctx, &downA)
	keeper.SetContract(ctx, &downB)

	require.NotPanics(t, func() { keeper.ClearDependenciesForContract(ctx, contract) })
	keptContractA, err = keeper.GetContract(ctx, keptContractA.ContractAddr)
	require.Nil(t, err)
	require.Equal(t, types.ContractInfoV2{
		ContractAddr: "sei1yum4v0v5l92jkxn8xpn9mjg7wuldk784ctg424ue8gqvdp88qzlqt6zc4h",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency:            "sei1ehyucudueas79h0zwufcnxtv7s2sfmwc6rt0v0hzczdgvyr3p56qhprg6n",
				ImmediateElderSibling: "sei1uyprmp0lu8w8z8kwxp7mxanrtrgn4lp7j557pxe4v8sczzdzl7ysk832hh",
			},
		},
	}, keptContractA)
	keptContractB, err = keeper.GetContract(ctx, keptContractB.ContractAddr)
	require.Nil(t, err)
	require.Equal(t, types.ContractInfoV2{
		ContractAddr: "sei1uyprmp0lu8w8z8kwxp7mxanrtrgn4lp7j557pxe4v8sczzdzl7ysk832hh",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency:              "sei1ehyucudueas79h0zwufcnxtv7s2sfmwc6rt0v0hzczdgvyr3p56qhprg6n",
				ImmediateYoungerSibling: "sei1yum4v0v5l92jkxn8xpn9mjg7wuldk784ctg424ue8gqvdp88qzlqt6zc4h",
			},
		},
	}, keptContractB)
	downA, err = keeper.GetContract(ctx, downA.ContractAddr)
	require.Nil(t, err)
	require.Equal(t, int64(2), downA.NumIncomingDependencies)
	downB, err = keeper.GetContract(ctx, downB.ContractAddr)
	require.Nil(t, err)
	require.Equal(t, int64(0), downB.NumIncomingDependencies)
}

func TestGetContractWithoutGasCharge(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	_ = keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		CodeId:       1,
		RentBalance:  1000000,
	})
	// regular gas meter case
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, 10000))
	contract, err := keeper.GetContractWithoutGasCharge(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, keepertest.TestContract, contract.ContractAddr)
	require.Equal(t, uint64(0), ctx.GasMeter().GasConsumed())
	require.Equal(t, uint64(10000), ctx.GasMeter().Limit())

	// regular gas meter out of gas case
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, 1))
	contract, err = keeper.GetContractWithoutGasCharge(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, keepertest.TestContract, contract.ContractAddr)
	require.Equal(t, uint64(0), ctx.GasMeter().GasConsumed())
	require.Equal(t, uint64(1), ctx.GasMeter().Limit())

	// infinite gas meter case
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	contract, err = keeper.GetContractWithoutGasCharge(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, keepertest.TestContract, contract.ContractAddr)
	require.Equal(t, uint64(0), ctx.GasMeter().GasConsumed())
	require.Equal(t, uint64(0), ctx.GasMeter().Limit())
}

func TestGetAllProcessableContractInfo(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	require.Greater(t, keeper.GetMinProcessableRent(ctx), uint64(0))

	goodContract := types.ContractInfoV2{
		ContractAddr:      "sei1avny5w9rcj7lmqmse8kukg2edvq4adqk8vlf58",
		NeedOrderMatching: true,
		RentBalance:       keeper.GetMinProcessableRent(ctx) + 1,
	}
	noMatchingContract := types.ContractInfoV2{
		ContractAddr:      "sei1fww2a30qc4sh25crhugcclaq2supxkpxeyz9lr",
		NeedOrderMatching: false,
		RentBalance:       keeper.GetMinProcessableRent(ctx) + 1,
	}
	suspendedContract := types.ContractInfoV2{
		ContractAddr:      "sei1hh95z3a5vk560khjnnkd3en8r0hu063mw64jzd",
		NeedOrderMatching: true,
		RentBalance:       keeper.GetMinProcessableRent(ctx) + 1,
		Suspended:         true,
	}
	outOfRentContract := types.ContractInfoV2{
		ContractAddr:      "sei1v2ye9tnmzwx5983emm00j0c7tyxqu855ktxw5l",
		NeedOrderMatching: true,
		RentBalance:       keeper.GetMinProcessableRent(ctx) - 1,
	}
	require.NoError(t, keeper.SetContract(ctx, &goodContract))
	require.NoError(t, keeper.SetContract(ctx, &noMatchingContract))
	require.NoError(t, keeper.SetContract(ctx, &suspendedContract))
	require.NoError(t, keeper.SetContract(ctx, &outOfRentContract))

	processableContracts := keeper.GetAllProcessableContractInfo(ctx)
	require.Equal(t, 1, len(processableContracts))
	require.Equal(t, "sei1avny5w9rcj7lmqmse8kukg2edvq4adqk8vlf58", processableContracts[0].ContractAddr)
}
