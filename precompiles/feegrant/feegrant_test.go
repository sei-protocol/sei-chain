package feegrant_test

import (
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/feegrant"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	feegranttypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestFeegrantPrecompileAddress(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	p, err := feegrant.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	require.Equal(t, "0x0000000000000000000000000000000000001010", p.Address().Hex())
}

func TestFeegrantQueries(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	p, err := feegrant.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}

	granterSei, granterEvm := testkeeper.MockAddressPair()
	granteeSei, granteeEvm := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, granterSei, granterEvm)
	k.SetAddressMapping(ctx, granteeSei, granteeEvm)

	allowance := &feegranttypes.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000))),
	}
	require.NoError(t, testApp.FeeGrantKeeper.GrantAllowance(ctx, granterSei, granteeSei, allowance))

	// The precompile JSON-encodes the grant's allowance Any; build the same
	// encoding here for exact-output comparisons.
	allowanceAny, err := codectypes.NewAnyWithValue(allowance)
	require.NoError(t, err)
	allowanceJSON, err := testApp.GetPrecompileKeepers().Codec().MarshalAsJSON(allowanceAny)
	require.NoError(t, err)
	require.Contains(t, string(allowanceJSON), "@type")
	require.Contains(t, string(allowanceJSON), "BasicAllowance")

	executor := p.GetExecutor().(*feegrant.PrecompileExecutor)

	t.Run("allowance", func(t *testing.T) {
		method, err := p.ABI.MethodById(executor.AllowanceID)
		require.NoError(t, err)

		inputs, err := method.Inputs.Pack(granterEvm, granteeEvm)
		require.NoError(t, err)

		ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, nil, nil, true, false)
		require.NoError(t, err)

		outputs, err := method.Outputs.Unpack(ret)
		require.NoError(t, err)
		require.Len(t, outputs, 1)
		grant := *abi.ConvertType(outputs[0], new(feegrant.Grant)).(*feegrant.Grant)
		require.Equal(t, granterSei.String(), grant.Granter)
		require.Equal(t, granteeSei.String(), grant.Grantee)
		require.Contains(t, string(grant.Allowance), "@type")
		require.Equal(t, string(allowanceJSON), string(grant.Allowance))
	})

	expectedListOutput := feegrant.AllowancesResponse{
		Allowances: []feegrant.Grant{
			{
				Granter:   granterSei.String(),
				Grantee:   granteeSei.String(),
				Allowance: allowanceJSON,
			},
		},
	}

	t.Run("allowances", func(t *testing.T) {
		method, err := p.ABI.MethodById(executor.AllowancesID)
		require.NoError(t, err)

		inputs, err := method.Inputs.Pack(granteeEvm, []byte{})
		require.NoError(t, err)

		ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, nil, nil, true, false)
		require.NoError(t, err)

		expected, err := method.Outputs.Pack(expectedListOutput)
		require.NoError(t, err)
		require.Equal(t, expected, ret)
	})

	t.Run("allowancesByGranter", func(t *testing.T) {
		method, err := p.ABI.MethodById(executor.AllowancesByGranterID)
		require.NoError(t, err)

		inputs, err := method.Inputs.Pack(granterEvm, []byte{})
		require.NoError(t, err)

		ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, nil, nil, true, false)
		require.NoError(t, err)

		expected, err := method.Outputs.Pack(expectedListOutput)
		require.NoError(t, err)
		require.Equal(t, expected, ret)
	})

	t.Run("allowance not found", func(t *testing.T) {
		otherSei, otherEvm := testkeeper.MockAddressPair()
		k.SetAddressMapping(ctx, otherSei, otherEvm)

		method, err := p.ABI.MethodById(executor.AllowanceID)
		require.NoError(t, err)

		inputs, err := method.Inputs.Pack(otherEvm, granteeEvm)
		require.NoError(t, err)

		ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, nil, nil, true, false)
		require.Error(t, err)
		require.Equal(t, vm.ErrExecutionReverted, err)
		require.Nil(t, ret)
	})

	t.Run("fails if value passed", func(t *testing.T) {
		method, err := p.ABI.MethodById(executor.AllowanceID)
		require.NoError(t, err)

		inputs, err := method.Inputs.Pack(granterEvm, granteeEvm)
		require.NoError(t, err)

		ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, big.NewInt(1), nil, false, false)
		require.Error(t, err)
		require.Equal(t, vm.ErrExecutionReverted, err)
		require.Nil(t, ret)
	})

	t.Run("fails for unassociated address", func(t *testing.T) {
		_, unassociatedEvm := testkeeper.MockAddressPair()

		method, err := p.ABI.MethodById(executor.AllowanceID)
		require.NoError(t, err)

		inputs, err := method.Inputs.Pack(unassociatedEvm, granteeEvm)
		require.NoError(t, err)

		ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, nil, nil, true, false)
		require.Error(t, err)
		require.Equal(t, vm.ErrExecutionReverted, err)
		require.Nil(t, ret)
	})
}
