package mint_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/mint"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

func TestPrecompile_Run_Params(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// seed a token release schedule through the keeper
	mintParams := testApp.MintKeeper.GetParams(ctx)
	mintParams.TokenReleaseSchedule = []minttypes.ScheduledTokenRelease{
		{
			StartDate:          "2024-01-01",
			EndDate:            "2024-02-01",
			TokenReleaseAmount: 1000,
		},
	}
	testApp.MintKeeper.SetParams(ctx, mintParams)

	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}

	p, err := mint.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	method, err := p.ABI.MethodById(p.GetExecutor().(*mint.PrecompileExecutor).ParamsID)
	require.NoError(t, err)

	expected := mint.MintParams{
		MintDenom: mintParams.MintDenom,
		TokenReleaseSchedule: []mint.ScheduledTokenRelease{
			{
				StartDate:          "2024-01-01",
				EndDate:            "2024-02-01",
				TokenReleaseAmount: 1000,
			},
		},
	}
	expectedBz, err := method.Outputs.Pack(expected)
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, method.ID, 100000, nil, nil, true, false)
	require.NoError(t, err)
	require.Equal(t, expectedBz, ret)

	// default genesis mint denom is the bond denom
	require.Equal(t, "usei", mintParams.MintDenom)

	// sending value to a view method reverts
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, method.ID, 100000, big.NewInt(1), nil, false, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
}

func TestPrecompile_Run_Minter(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// seed a minter through the keeper
	minter := minttypes.NewMinter("2024-01-01", "2024-02-01", "usei", 5000)
	testApp.MintKeeper.SetMinter(ctx, minter)

	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}

	p, err := mint.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	method, err := p.ABI.MethodById(p.GetExecutor().(*mint.PrecompileExecutor).MinterID)
	require.NoError(t, err)

	expected := mint.Minter{
		StartDate:           minter.StartDate,
		EndDate:             minter.EndDate,
		Denom:               minter.Denom,
		TotalMintAmount:     minter.TotalMintAmount,
		RemainingMintAmount: minter.RemainingMintAmount,
		LastMintAmount:      minter.LastMintAmount,
		LastMintDate:        minter.LastMintDate,
		LastMintHeight:      minter.LastMintHeight,
	}
	expectedBz, err := method.Outputs.Pack(expected)
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, method.ID, 100000, nil, nil, true, false)
	require.NoError(t, err)
	require.Equal(t, expectedBz, ret)

	require.Equal(t, "usei", expected.Denom)
	require.Equal(t, uint64(5000), expected.TotalMintAmount)
	require.Equal(t, uint64(5000), expected.RemainingMintAmount)

	// sending value to a view method reverts
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, method.ID, 100000, big.NewInt(1), nil, false, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
}
