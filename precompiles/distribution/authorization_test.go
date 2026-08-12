package distribution_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/distribution"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authztypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestWithdrawAuthorizationFlow(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	blockTime := time.Unix(1_700_000_300, 0).UTC()
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2).WithBlockTime(blockTime)
	k := &testApp.EvmKeeper

	validator := setupValidator(t, ctx, testApp, stakingtypes.Bonded, testkeeper.MockPrivateKey().PubKey())
	operatorSeiAddr := sdk.AccAddress(validator)
	_, operatorEVMAddr := testkeeper.MockAddressPair()
	granteeSeiAddr, granteeEVMAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, operatorSeiAddr, operatorEVMAddr)
	k.SetAddressMapping(ctx, granteeSeiAddr, granteeEVMAddr)

	p, err := distribution.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}
	call := func(caller common.Address, methodName string, value *big.Int, args ...interface{}) ([]byte, error) {
		t.Helper()
		input, packErr := p.ABI.Pack(methodName, args...)
		require.NoError(t, packErr)
		ret, _, callErr := p.RunAndCalculateGas(&evm, caller, caller, input, 2_000_000, value, nil, false, false)
		return ret, callErr
	}
	assertSuccess := func(methodName string, ret []byte) {
		t.Helper()
		outputs, unpackErr := p.ABI.Methods[methodName].Outputs.Unpack(ret)
		require.NoError(t, unpackErr)
		require.Equal(t, []interface{}{true}, outputs)
	}

	expiration := blockTime.Add(time.Hour)
	ret, err := call(operatorEVMAddr, distribution.GrantWithdrawMethod, nil, granteeEVMAddr, expiration.Unix())
	require.NoError(t, err)
	assertSuccess(distribution.GrantWithdrawMethod, ret)
	for _, msg := range []sdk.Msg{
		&distrtypes.MsgWithdrawDelegatorReward{},
		&distrtypes.MsgWithdrawValidatorCommission{},
	} {
		authorization, storedExpiration := testApp.AuthzKeeper.GetCleanAuthorization(
			statedb.Ctx(),
			granteeSeiAddr,
			operatorSeiAddr,
			sdk.MsgTypeURL(msg),
		)
		require.IsType(t, &authztypes.GenericAuthorization{}, authorization)
		require.Equal(t, expiration, storedExpiration)
	}

	ret, err = call(
		granteeEVMAddr,
		distribution.WithdrawDelegationRewardsWithAuthzMethod,
		nil,
		operatorEVMAddr,
		validator.String(),
	)
	require.NoError(t, err)
	assertSuccess(distribution.WithdrawDelegationRewardsWithAuthzMethod, ret)

	withdrawSeiAddr, withdrawEVMAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(statedb.Ctx(), withdrawSeiAddr, withdrawEVMAddr)
	params := testApp.DistrKeeper.GetParams(statedb.Ctx())
	params.WithdrawAddrEnabled = true
	testApp.DistrKeeper.SetParams(statedb.Ctx(), params)
	ret, err = call(operatorEVMAddr, distribution.SetWithdrawAddressMethod, nil, withdrawEVMAddr)
	require.NoError(t, err)
	assertSuccess(distribution.SetWithdrawAddressMethod, ret)
	require.Equal(t, withdrawSeiAddr, testApp.DistrKeeper.GetDelegatorWithdrawAddr(statedb.Ctx(), operatorSeiAddr))

	commission := sdk.DecCoins{sdk.NewDecCoin(sdk.DefaultBondDenom, sdk.NewInt(10))}
	commissionCoins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(10)))
	require.NoError(t, testApp.BankKeeper.MintCoins(statedb.Ctx(), evmtypes.ModuleName, commissionCoins))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToModule(statedb.Ctx(), evmtypes.ModuleName, distrtypes.ModuleName, commissionCoins))
	testApp.DistrKeeper.SetValidatorOutstandingRewards(
		statedb.Ctx(),
		validator,
		distrtypes.ValidatorOutstandingRewards{Rewards: commission},
	)
	testApp.DistrKeeper.SetValidatorAccumulatedCommission(
		statedb.Ctx(),
		validator,
		distrtypes.ValidatorAccumulatedCommission{Commission: commission},
	)
	withdrawBalanceBefore := testApp.BankKeeper.GetBalance(statedb.Ctx(), withdrawSeiAddr, sdk.DefaultBondDenom)
	operatorBalanceBefore := testApp.BankKeeper.GetBalance(statedb.Ctx(), operatorSeiAddr, sdk.DefaultBondDenom)
	logCountBefore := len(statedb.GetAllLogs())

	ret, err = call(
		granteeEVMAddr,
		distribution.WithdrawValidatorCommissionWithAuthzMethod,
		nil,
		operatorEVMAddr,
	)
	require.NoError(t, err)
	assertSuccess(distribution.WithdrawValidatorCommissionWithAuthzMethod, ret)
	withdrawBalanceAfter := testApp.BankKeeper.GetBalance(statedb.Ctx(), withdrawSeiAddr, sdk.DefaultBondDenom)
	operatorBalanceAfter := testApp.BankKeeper.GetBalance(statedb.Ctx(), operatorSeiAddr, sdk.DefaultBondDenom)
	require.Equal(t, withdrawBalanceBefore.Amount.AddRaw(10), withdrawBalanceAfter.Amount)
	require.Equal(t, operatorBalanceBefore.Amount, operatorBalanceAfter.Amount)
	logs := statedb.GetAllLogs()
	require.Len(t, logs, logCountBefore+1)
	require.Equal(t, distribution.ValidatorCommissionEventSig, logs[logCountBefore].Topics[0])
	eventValues, err := p.ABI.Events[distribution.ValidatorCommissionEvent].Inputs.NonIndexed().Unpack(logs[logCountBefore].Data)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(10), eventValues[0])

	ret, err = call(operatorEVMAddr, distribution.RevokeWithdrawMethod, nil, granteeEVMAddr)
	require.NoError(t, err)
	assertSuccess(distribution.RevokeWithdrawMethod, ret)
	for _, msg := range []sdk.Msg{
		&distrtypes.MsgWithdrawDelegatorReward{},
		&distrtypes.MsgWithdrawValidatorCommission{},
	} {
		authorization, _ := testApp.AuthzKeeper.GetCleanAuthorization(
			statedb.Ctx(),
			granteeSeiAddr,
			operatorSeiAddr,
			sdk.MsgTypeURL(msg),
		)
		require.Nil(t, authorization)
	}

	_, err = call(
		granteeEVMAddr,
		distribution.WithdrawDelegationRewardsWithAuthzMethod,
		nil,
		operatorEVMAddr,
		validator.String(),
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
}
