package slashing_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/slashing"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func mockConsAddress() sdk.ConsAddress {
	privKey := ed25519.GenPrivKey()
	return sdk.ConsAddress(privKey.PubKey().Address())
}

func TestPrecompile_Run_Params(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}

	p, err := slashing.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	method, err := p.ABI.MethodById(p.GetExecutor().(*slashing.PrecompileExecutor).ParamsID)
	require.NoError(t, err)

	slashingParams := testApp.SlashingKeeper.GetParams(ctx)
	expected := slashing.SlashingParams{
		SignedBlocksWindow:      slashingParams.SignedBlocksWindow,
		MinSignedPerWindow:      slashingParams.MinSignedPerWindow.String(),
		DowntimeJailDuration:    uint64(slashingParams.DowntimeJailDuration.Seconds()),
		SlashFractionDoubleSign: slashingParams.SlashFractionDoubleSign.String(),
		SlashFractionDowntime:   slashingParams.SlashFractionDowntime.String(),
	}
	expectedBz, err := method.Outputs.Pack(expected)
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, method.ID, 100000, nil, nil, true, false)
	require.NoError(t, err)
	require.Equal(t, expectedBz, ret)

	// sanity check against default genesis
	require.Greater(t, expected.SignedBlocksWindow, int64(0))
	require.NotEmpty(t, expected.MinSignedPerWindow)
	require.NotEmpty(t, expected.SlashFractionDoubleSign)
	require.NotEmpty(t, expected.SlashFractionDowntime)

	// sending value to a view method reverts
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, method.ID, 100000, big.NewInt(1), nil, false, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
}

func TestPrecompile_Run_SigningInfo(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// seed a signing info through the keeper
	consAddr := mockConsAddress()
	info := slashingtypes.NewValidatorSigningInfo(consAddr, 1, 5, time.Unix(1000, 0).UTC(), true, 7)
	testApp.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, info)

	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}

	p, err := slashing.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	method, err := p.ABI.MethodById(p.GetExecutor().(*slashing.PrecompileExecutor).SigningInfoID)
	require.NoError(t, err)

	expected := slashing.SigningInfo{
		ValidatorAddress:    consAddr.String(),
		StartHeight:         1,
		IndexOffset:         5,
		JailedUntil:         1000,
		Tombstoned:          true,
		MissedBlocksCounter: 7,
	}
	expectedBz, err := method.Outputs.Pack(expected)
	require.NoError(t, err)

	inputs, err := method.Inputs.Pack(consAddr.String())
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 100000, nil, nil, true, false)
	require.NoError(t, err)
	require.Equal(t, expectedBz, ret)

	// unknown validator reverts
	unknownInputs, err := method.Inputs.Pack(mockConsAddress().String())
	require.NoError(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, unknownInputs...), 100000, nil, nil, true, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)

	// invalid bech32 address reverts
	invalidInputs, err := method.Inputs.Pack("notanaddress")
	require.NoError(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, invalidInputs...), 100000, nil, nil, true, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)

	// sending value to a view method reverts
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 100000, big.NewInt(1), nil, false, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
}

func TestPrecompile_Run_SigningInfos(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// seed two signing infos through the keeper
	consAddr1 := mockConsAddress()
	consAddr2 := mockConsAddress()
	info1 := slashingtypes.NewValidatorSigningInfo(consAddr1, 1, 2, time.Unix(100, 0).UTC(), false, 3)
	info2 := slashingtypes.NewValidatorSigningInfo(consAddr2, 4, 5, time.Unix(200, 0).UTC(), true, 6)
	testApp.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr1, info1)
	testApp.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr2, info2)

	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}

	p, err := slashing.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	method, err := p.ABI.MethodById(p.GetExecutor().(*slashing.PrecompileExecutor).SigningInfosID)
	require.NoError(t, err)

	// build the expected list from the keeper's own iteration (same store
	// order as the paginated query); state may contain infos seeded by
	// other tests sharing EVMTestApp
	expected := slashing.SigningInfosResponse{}
	testApp.SlashingKeeper.IterateValidatorSigningInfos(ctx, func(_ sdk.ConsAddress, info slashingtypes.ValidatorSigningInfo) bool {
		expected.SigningInfos = append(expected.SigningInfos, slashing.SigningInfo{
			ValidatorAddress:    info.Address,
			StartHeight:         info.StartHeight,
			IndexOffset:         info.IndexOffset,
			JailedUntil:         info.JailedUntil.Unix(),
			Tombstoned:          info.Tombstoned,
			MissedBlocksCounter: info.MissedBlocksCounter,
		})
		return false
	})
	require.GreaterOrEqual(t, len(expected.SigningInfos), 2)
	require.Contains(t, expected.SigningInfos, slashing.SigningInfo{
		ValidatorAddress:    consAddr1.String(),
		StartHeight:         1,
		IndexOffset:         2,
		JailedUntil:         100,
		Tombstoned:          false,
		MissedBlocksCounter: 3,
	})
	require.Contains(t, expected.SigningInfos, slashing.SigningInfo{
		ValidatorAddress:    consAddr2.String(),
		StartHeight:         4,
		IndexOffset:         5,
		JailedUntil:         200,
		Tombstoned:          true,
		MissedBlocksCounter: 6,
	})
	expectedBz, err := method.Outputs.Pack(expected)
	require.NoError(t, err)

	inputs, err := method.Inputs.Pack([]byte{})
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 100000, nil, nil, true, false)
	require.NoError(t, err)
	require.Equal(t, expectedBz, ret)

	// sending value to a view method reverts
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 100000, big.NewInt(1), nil, false, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
}
