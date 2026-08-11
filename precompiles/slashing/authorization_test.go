package slashing_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/slashing"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authztypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestUnjailAuthorizationFlow(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	blockTime := time.Unix(1_700_000_200, 0).UTC()
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2).WithBlockTime(blockTime)
	k := &testApp.EvmKeeper

	valPub := testkeeper.MockPrivateKey().PubKey()
	valAddr := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub)
	consAddr := sdk.ConsAddress(valPub.Address())
	testApp.StakingKeeper.Jail(ctx, consAddr)

	operatorSeiAddr := sdk.AccAddress(valAddr)
	_, operatorEVMAddr := testkeeper.MockAddressPair()
	granteeSeiAddr, granteeEVMAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, operatorSeiAddr, operatorEVMAddr)
	k.SetAddressMapping(ctx, granteeSeiAddr, granteeEVMAddr)

	p, err := slashing.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}
	call := func(caller common.Address, methodName string, value *big.Int, args ...interface{}) ([]byte, error) {
		t.Helper()
		input, packErr := p.ABI.Pack(methodName, args...)
		require.NoError(t, packErr)
		ret, _, callErr := p.RunAndCalculateGas(&evm, caller, caller, input, 1_000_000, value, nil, false, false)
		return ret, callErr
	}
	assertSuccess := func(methodName string, ret []byte) {
		t.Helper()
		outputs, unpackErr := p.ABI.Methods[methodName].Outputs.Unpack(ret)
		require.NoError(t, unpackErr)
		require.Equal(t, []interface{}{true}, outputs)
	}

	expiration := blockTime.Add(time.Hour)
	ret, err := call(operatorEVMAddr, slashing.GrantUnjailMethod, nil, granteeEVMAddr, expiration.Unix())
	require.NoError(t, err)
	assertSuccess(slashing.GrantUnjailMethod, ret)
	authorization, storedExpiration := testApp.AuthzKeeper.GetCleanAuthorization(
		statedb.Ctx(),
		granteeSeiAddr,
		operatorSeiAddr,
		sdk.MsgTypeURL(&slashingtypes.MsgUnjail{}),
	)
	require.IsType(t, &authztypes.GenericAuthorization{}, authorization)
	require.Equal(t, expiration, storedExpiration)

	ret, err = call(granteeEVMAddr, slashing.UnjailWithAuthzMethod, nil, operatorEVMAddr)
	require.NoError(t, err)
	assertSuccess(slashing.UnjailWithAuthzMethod, ret)
	require.False(t, testApp.StakingKeeper.Validator(statedb.Ctx(), valAddr).IsJailed())

	testApp.StakingKeeper.Jail(statedb.Ctx(), consAddr)
	ret, err = call(operatorEVMAddr, slashing.RevokeUnjailMethod, nil, granteeEVMAddr)
	require.NoError(t, err)
	assertSuccess(slashing.RevokeUnjailMethod, ret)
	authorization, _ = testApp.AuthzKeeper.GetCleanAuthorization(
		statedb.Ctx(),
		granteeSeiAddr,
		operatorSeiAddr,
		sdk.MsgTypeURL(&slashingtypes.MsgUnjail{}),
	)
	require.Nil(t, authorization)

	_, err = call(granteeEVMAddr, slashing.UnjailWithAuthzMethod, nil, operatorEVMAddr)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.True(t, testApp.StakingKeeper.Validator(statedb.Ctx(), valAddr).IsJailed())
}
