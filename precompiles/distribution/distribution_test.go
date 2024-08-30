package distribution_test

import (
	"context"
	"embed"
	"encoding/hex"
	"math/big"
	"reflect"
	"testing"
	"time"

	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	crptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	abitypes "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/app"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/distribution"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

//go:embed abi.json
//go:embed staking_abi.json
var f embed.FS

func TestWithdraw(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	distrParams := testApp.DistrKeeper.GetParams(ctx)
	distrParams.WithdrawAddrEnabled = true
	testApp.DistrKeeper.SetParams(ctx, distrParams)
	k := &testApp.EvmKeeper
	valPub1 := secp256k1.GenPrivKey().PubKey()
	val := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub1)

	// delegate
	abi := pcommon.MustGetABI(f, "staking_abi.json")
	args, err := abi.Pack("delegate", val.String())
	require.Nil(t, err)

	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	addr := common.HexToAddress(staking.StakingAddress)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(100_000_000_000_000),
		Data:     args,
		Nonce:    0,
	}
	chainID := k.ChainID(ctx)
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
	require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))

	msgServer := keeper.NewMsgServerImpl(k)

	ante.Preprocess(ctx, req)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	d, found := testApp.StakingKeeper.GetDelegation(ctx, seiAddr, val)
	require.True(t, found)
	require.Equal(t, int64(100), d.Shares.RoundInt().Int64())

	// set withdraw addr
	withdrawSeiAddr, withdrawAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, withdrawSeiAddr, withdrawAddr)
	abi = pcommon.MustGetABI(f, "abi.json")
	args, err = abi.Pack("setWithdrawAddress", withdrawAddr)
	require.Nil(t, err)
	addr = common.HexToAddress(distribution.DistrAddress)
	txData = ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     args,
		Nonce:    1,
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err = evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	ante.Preprocess(ctx, req)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)
	require.Equal(t, withdrawSeiAddr.String(), testApp.DistrKeeper.GetDelegatorWithdrawAddr(ctx, seiAddr).String())

	// withdraw
	args, err = abi.Pack("withdrawDelegationRewards", val.String())
	require.Nil(t, err)
	txData = ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     args,
		Nonce:    2,
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err = evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	ante.Preprocess(ctx, req)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)
	require.Equal(t, uint64(64124), res.GasUsed)

	// reinitialized
	d, found = testApp.StakingKeeper.GetDelegation(ctx, seiAddr, val)
	require.True(t, found)
}

func TestWithdrawMultipleDelegationRewards(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	distrParams := testApp.DistrKeeper.GetParams(ctx)
	distrParams.WithdrawAddrEnabled = true
	testApp.DistrKeeper.SetParams(ctx, distrParams)
	k := &testApp.EvmKeeper
	validators := []sdk.ValAddress{
		getValidator(t, ctx, testApp),
		getValidator(t, ctx, testApp),
		getValidator(t, ctx, testApp)}

	abi := pcommon.MustGetABI(f, "staking_abi.json")
	privKey := testkeeper.MockPrivateKey()
	addr := common.HexToAddress(staking.StakingAddress)
	chainID := k.ChainID(ctx)
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	msgServer := keeper.NewMsgServerImpl(k)
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	// delegate
	for _, val := range validators {
		delegate(ctx, t, abi, addr, k, val, testApp, privKey, signer, msgServer)
	}

	// set withdraw addr and withdraw
	setWithdrawAddressAndWithdraw(ctx, t, addr, validators, k, testApp, privKey, signer, msgServer)

}

func delegate(ctx sdk.Context,
	t *testing.T,
	abi abitypes.ABI,
	addr common.Address,
	k *keeper.Keeper,
	val sdk.ValAddress,
	testApp *app.App,
	privKey crptotypes.PrivKey,
	signer ethtypes.Signer,
	msgServer evmtypes.MsgServer) {
	args, err := abi.Pack("delegate", val.String())
	require.Nil(t, err)

	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(100_000_000_000_000),
		Data:     args,
		Nonce:    0,
	}
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	seiAddr, _ := testkeeper.PrivateKeyToAddresses(privKey)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
	require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))

	ante.Preprocess(ctx, req)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	d, found := testApp.StakingKeeper.GetDelegation(ctx, seiAddr, val)
	require.True(t, found)
	require.Equal(t, int64(100), d.Shares.RoundInt().Int64())
}

func setWithdrawAddressAndWithdraw(
	ctx sdk.Context,
	t *testing.T,
	addr common.Address,
	vals []sdk.ValAddress,
	k *keeper.Keeper,
	testApp *app.App,
	privKey crptotypes.PrivKey,
	signer ethtypes.Signer,
	msgServer evmtypes.MsgServer,
) {
	withdrawSeiAddr, withdrawAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, withdrawSeiAddr, withdrawAddr)
	abi := pcommon.MustGetABI(f, "abi.json")
	args, err := abi.Pack("setWithdrawAddress", withdrawAddr)
	require.Nil(t, err)
	addr = common.HexToAddress(distribution.DistrAddress)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     args,
		Nonce:    1,
	}
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	ante.Preprocess(ctx, req)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)
	seiAddr, _ := testkeeper.PrivateKeyToAddresses(privKey)
	require.Equal(t, withdrawSeiAddr.String(), testApp.DistrKeeper.GetDelegatorWithdrawAddr(ctx, seiAddr).String())

	var validators []string
	for _, val := range vals {
		validators = append(validators, val.String())
	}

	args, err = abi.Pack("withdrawMultipleDelegationRewards", validators)
	require.Nil(t, err)
	txData = ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     args,
		Nonce:    2,
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	r, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	ante.Preprocess(ctx, r)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), r)
	require.Nil(t, err)
	require.Empty(t, res.VmError)
	require.Equal(t, uint64(148290), res.GasUsed)

	// reinitialized
	for _, val := range vals {
		_, found := testApp.StakingKeeper.GetDelegation(ctx, seiAddr, val)
		require.True(t, found)
	}
}

func getValidator(t *testing.T, ctx sdk.Context, testApp *app.App) sdk.ValAddress {
	return setupValidator(t, ctx, testApp, stakingtypes.Unbonded, secp256k1.GenPrivKey().PubKey())
}

func setupValidator(t *testing.T, ctx sdk.Context, a *app.App, bondStatus stakingtypes.BondStatus, valPub crptotypes.PubKey) sdk.ValAddress {
	valAddr := sdk.ValAddress(valPub.Address())
	bondDenom := a.StakingKeeper.GetParams(ctx).BondDenom
	selfBond := sdk.NewCoins(sdk.Coin{Amount: sdk.NewInt(100), Denom: bondDenom})

	err := a.BankKeeper.MintCoins(ctx, minttypes.ModuleName, selfBond)
	require.NoError(t, err)

	err = a.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, sdk.AccAddress(valAddr), selfBond)
	require.NoError(t, err)

	sh := teststaking.NewHelper(t, ctx, a.StakingKeeper)
	msg := sh.CreateValidatorMsg(valAddr, valPub, selfBond[0].Amount)
	sh.Handle(msg, true)

	val, found := a.StakingKeeper.GetValidator(ctx, valAddr)
	require.True(t, found)

	val = val.UpdateStatus(bondStatus)
	a.StakingKeeper.SetValidator(ctx, val)

	consAddr, err := val.GetConsAddr()
	require.NoError(t, err)

	signingInfo := slashingtypes.NewValidatorSigningInfo(
		consAddr,
		ctx.BlockHeight(),
		0,
		time.Unix(0, 0),
		false,
		0,
	)
	a.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, signingInfo)

	return valAddr
}

func TestPrecompile_RunAndCalculateGas_WithdrawDelegationRewards(t *testing.T) {
	_, notAssociatedCallerEvmAddress := testkeeper.MockAddressPair()
	_, contractEvmAddress := testkeeper.MockAddressPair()
	validatorAddress := "seivaloper1reedlc9w8p7jrpqfky4c5k90nea4p6dhk5yqgd"

	type fields struct {
		Precompile                          pcommon.Precompile
		distrKeeper                         pcommon.DistributionKeeper
		evmKeeper                           pcommon.EVMKeeper
		address                             common.Address
		SetWithdrawAddrID                   []byte
		WithdrawDelegationRewardsID         []byte
		WithdrawMultipleDelegationRewardsID []byte
	}
	type args struct {
		evm             *vm.EVM
		caller          common.Address
		callingContract common.Address
		validator       string
		suppliedGas     uint64
		value           *big.Int
		readOnly        bool
	}
	tests := []struct {
		name             string
		fields           fields
		args             args
		wantRet          []byte
		wantRemainingGas uint64
		wantErr          bool
		wantErrMsg       string
	}{
		{
			name:   "fails if value is being sent",
			fields: fields{},
			args: args{
				validator: validatorAddress,
				value:     big.NewInt(10),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "sending funds to a non-payable function",
		},
		{
			name:   "fails if delegator is not passed",
			fields: fields{},
			args: args{
				validator:   validatorAddress,
				suppliedGas: uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       evmtypes.NewAssociationMissingErr("0x0000000000000000000000000000000000000000").Error(),
		},
		{
			name:   "fails if delegator is not associated",
			fields: fields{},
			args: args{
				caller:          notAssociatedCallerEvmAddress,
				callingContract: notAssociatedCallerEvmAddress,
				validator:       validatorAddress,
				suppliedGas:     uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       evmtypes.NewAssociationMissingErr(notAssociatedCallerEvmAddress.String()).Error(),
		},
		{
			name:             "fails if no args passed",
			fields:           fields{},
			args:             args{},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "{ReadFlat}",
		},
		{
			name:   "fails if caller != callingContract",
			fields: fields{},
			args: args{
				caller:          notAssociatedCallerEvmAddress,
				callingContract: contractEvmAddress,
				validator:       validatorAddress,
				suppliedGas:     uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot delegatecall distr",
		},
		{
			name:   "fails if caller != callingContract and callingContract not set",
			fields: fields{},
			args: args{
				caller:          notAssociatedCallerEvmAddress,
				callingContract: contractEvmAddress,
				validator:       validatorAddress,
				suppliedGas:     uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot delegatecall distr",
		},
		{
			name:   "fails if readOnly",
			fields: fields{},
			args: args{
				caller:          notAssociatedCallerEvmAddress,
				callingContract: notAssociatedCallerEvmAddress,
				validator:       validatorAddress,
				suppliedGas:     uint64(1000000),
				readOnly:        true,
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot call distr precompile from staticcall",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testApp := testkeeper.EVMTestApp
			ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
			k := &testApp.EvmKeeper
			stateDb := state.NewDBImpl(ctx, k, true)
			evm := vm.EVM{
				StateDB: stateDb,
			}
			p, _ := distribution.NewPrecompile(tt.fields.distrKeeper, k)
			withdraw, err := p.ABI.MethodById(p.GetExecutor().(*distribution.PrecompileExecutor).WithdrawDelegationRewardsID)
			require.Nil(t, err)
			inputs, err := withdraw.Inputs.Pack(tt.args.validator)
			require.Nil(t, err)
			gotRet, gotRemainingGas, err := p.RunAndCalculateGas(&evm, tt.args.caller, tt.args.callingContract, append(p.GetExecutor().(*distribution.PrecompileExecutor).WithdrawDelegationRewardsID, inputs...), tt.args.suppliedGas, tt.args.value, nil, tt.args.readOnly)
			if (err != nil) != tt.wantErr {
				t.Errorf("RunAndCalculateGas() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Equal(t, tt.wantErrMsg, string(gotRet))
			} else if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("RunAndCalculateGas() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
			if gotRemainingGas != tt.wantRemainingGas {
				t.Errorf("RunAndCalculateGas() gotRemainingGas = %v, want %v", gotRemainingGas, tt.wantRemainingGas)
			}
		})
	}
}

func TestPrecompile_RunAndCalculateGas_WithdrawMultipleDelegationRewards(t *testing.T) {
	_, notAssociatedCallerEvmAddress := testkeeper.MockAddressPair()
	_, contractEvmAddress := testkeeper.MockAddressPair()
	validatorAddresses := []string{"seivaloper1reedlc9w8p7jrpqfky4c5k90nea4p6dhk5yqgd"}

	type fields struct {
		Precompile                          pcommon.Precompile
		distrKeeper                         pcommon.DistributionKeeper
		evmKeeper                           pcommon.EVMKeeper
		address                             common.Address
		SetWithdrawAddrID                   []byte
		WithdrawDelegationRewardsID         []byte
		WithdrawMultipleDelegationRewardsID []byte
	}
	type args struct {
		evm             *vm.EVM
		caller          common.Address
		callingContract common.Address
		validators      []string
		suppliedGas     uint64
		value           *big.Int
		readOnly        bool
	}
	tests := []struct {
		name             string
		fields           fields
		args             args
		wantRet          []byte
		wantRemainingGas uint64
		wantErr          bool
		wantErrMsg       string
	}{
		{
			name:   "fails if value is being sent",
			fields: fields{},
			args: args{
				validators: validatorAddresses,
				value:      big.NewInt(10),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "sending funds to a non-payable function",
		},
		{
			name:   "fails if delegator is not passed",
			fields: fields{},
			args: args{
				validators:  validatorAddresses,
				suppliedGas: uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       evmtypes.NewAssociationMissingErr("0x0000000000000000000000000000000000000000").Error(),
		},
		{
			name:   "fails if delegator is not associated",
			fields: fields{},
			args: args{
				caller:          notAssociatedCallerEvmAddress,
				callingContract: notAssociatedCallerEvmAddress,
				validators:      validatorAddresses,
				suppliedGas:     uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       evmtypes.NewAssociationMissingErr(notAssociatedCallerEvmAddress.String()).Error(),
		},
		{
			name:             "fails if no args passed",
			fields:           fields{},
			args:             args{},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "{ReadFlat}",
		},
		{
			name:   "fails if caller != callingContract",
			fields: fields{},
			args: args{
				caller:          notAssociatedCallerEvmAddress,
				callingContract: contractEvmAddress,
				validators:      validatorAddresses,
				suppliedGas:     uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot delegatecall distr",
		},
		{
			name:   "fails if caller != callingContract and callingContract not set",
			fields: fields{},
			args: args{
				caller:      notAssociatedCallerEvmAddress,
				validators:  validatorAddresses,
				suppliedGas: uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot delegatecall distr",
		},
		{
			name:   "fails if readOnly",
			fields: fields{},
			args: args{
				caller:          notAssociatedCallerEvmAddress,
				callingContract: notAssociatedCallerEvmAddress,
				validators:      validatorAddresses,
				suppliedGas:     uint64(1000000),
				readOnly:        true,
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot call distr precompile from staticcall",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testApp := testkeeper.EVMTestApp
			ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
			k := &testApp.EvmKeeper
			stateDb := state.NewDBImpl(ctx, k, true)
			evm := vm.EVM{
				StateDB: stateDb,
			}
			p, _ := distribution.NewPrecompile(tt.fields.distrKeeper, k)
			withdraw, err := p.ABI.MethodById(p.GetExecutor().(*distribution.PrecompileExecutor).WithdrawMultipleDelegationRewardsID)
			require.Nil(t, err)
			inputs, err := withdraw.Inputs.Pack(tt.args.validators)
			require.Nil(t, err)
			gotRet, gotRemainingGas, err := p.RunAndCalculateGas(&evm, tt.args.caller, tt.args.callingContract, append(p.GetExecutor().(*distribution.PrecompileExecutor).WithdrawMultipleDelegationRewardsID, inputs...), tt.args.suppliedGas, tt.args.value, nil, tt.args.readOnly)
			if (err != nil) != tt.wantErr {
				t.Errorf("RunAndCalculateGas() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Equal(t, tt.wantErrMsg, string(gotRet))
			} else if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("RunAndCalculateGas() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
			if gotRemainingGas != tt.wantRemainingGas {
				t.Errorf("RunAndCalculateGas() gotRemainingGas = %v, want %v", gotRemainingGas, tt.wantRemainingGas)
			}
		})
	}
}

func TestPrecompile_RunAndCalculateGas_SetWithdrawAddress(t *testing.T) {
	_, notAssociatedCallerEvmAddress := testkeeper.MockAddressPair()
	callerSeiAddress, callerEvmAddress := testkeeper.MockAddressPair()
	_, contractEvmAddress := testkeeper.MockAddressPair()

	type fields struct {
		Precompile                          pcommon.Precompile
		distrKeeper                         pcommon.DistributionKeeper
		evmKeeper                           pcommon.EVMKeeper
		address                             common.Address
		SetWithdrawAddrID                   []byte
		WithdrawDelegationRewardsID         []byte
		WithdrawMultipleDelegationRewardsID []byte
	}
	type args struct {
		evm             *vm.EVM
		addressToSet    common.Address
		caller          common.Address
		callingContract common.Address
		suppliedGas     uint64
		value           *big.Int
		readOnly        bool
	}
	tests := []struct {
		name             string
		fields           fields
		args             args
		wantRet          []byte
		wantRemainingGas uint64
		wantErr          bool
		wantErrMsg       string
	}{
		{
			name:   "fails if value is being sent",
			fields: fields{},
			args: args{
				addressToSet: notAssociatedCallerEvmAddress,
				value:        big.NewInt(10),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "sending funds to a non-payable function",
		},
		{
			name:   "fails if delegator is not passed",
			fields: fields{},
			args: args{
				addressToSet: notAssociatedCallerEvmAddress,
				suppliedGas:  uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       evmtypes.NewAssociationMissingErr("0x0000000000000000000000000000000000000000").Error(),
		},
		{
			name:   "fails if delegator is not associated",
			fields: fields{},
			args: args{
				addressToSet:    notAssociatedCallerEvmAddress,
				caller:          notAssociatedCallerEvmAddress,
				callingContract: notAssociatedCallerEvmAddress,
				suppliedGas:     uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       evmtypes.NewAssociationMissingErr(notAssociatedCallerEvmAddress.String()).Error(),
		},
		{
			name:   "fails if address is invalid",
			fields: fields{},
			args: args{
				addressToSet:    common.Address{},
				caller:          callerEvmAddress,
				callingContract: callerEvmAddress,
				suppliedGas:     uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "invalid addr",
		},
		{
			name:             "fails if no args passed",
			fields:           fields{},
			args:             args{},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "{ReadFlat}",
		},
		{
			name:   "fails if caller != callingContract",
			fields: fields{},
			args: args{
				addressToSet:    common.Address{},
				caller:          callerEvmAddress,
				callingContract: contractEvmAddress,
				suppliedGas:     uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot delegatecall distr",
		},
		{
			name:   "fails if caller != callingContract with callingContract not set",
			fields: fields{},
			args: args{
				addressToSet: common.Address{},
				caller:       callerEvmAddress,
				suppliedGas:  uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot delegatecall distr",
		},
		{
			name:   "fails if readOnly",
			fields: fields{},
			args: args{
				addressToSet:    common.Address{},
				caller:          callerEvmAddress,
				callingContract: callerEvmAddress,
				suppliedGas:     uint64(1000000),
				readOnly:        true,
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot call distr precompile from staticcall",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testApp := testkeeper.EVMTestApp
			ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
			k := &testApp.EvmKeeper
			k.SetAddressMapping(ctx, callerSeiAddress, callerEvmAddress)
			stateDb := state.NewDBImpl(ctx, k, true)
			evm := vm.EVM{
				StateDB:   stateDb,
				TxContext: vm.TxContext{Origin: callerEvmAddress},
			}
			p, _ := distribution.NewPrecompile(tt.fields.distrKeeper, k)
			setAddress, err := p.ABI.MethodById(p.GetExecutor().(*distribution.PrecompileExecutor).SetWithdrawAddrID)
			require.Nil(t, err)
			inputs, err := setAddress.Inputs.Pack(tt.args.addressToSet)
			require.Nil(t, err)
			gotRet, gotRemainingGas, err := p.RunAndCalculateGas(&evm, tt.args.caller, tt.args.callingContract, append(p.GetExecutor().(*distribution.PrecompileExecutor).SetWithdrawAddrID, inputs...), tt.args.suppliedGas, tt.args.value, nil, tt.args.readOnly)
			if (err != nil) != tt.wantErr {
				t.Errorf("RunAndCalculateGas() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Equal(t, tt.wantErrMsg, string(gotRet))
			} else if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("RunAndCalculateGas() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
			if gotRemainingGas != tt.wantRemainingGas {
				t.Errorf("RunAndCalculateGas() gotRemainingGas = %v, want %v", gotRemainingGas, tt.wantRemainingGas)
			}
		})
	}
}

type TestDistributionKeeper struct{}

func (tk *TestDistributionKeeper) SetWithdrawAddr(ctx sdk.Context, delegatorAddr sdk.AccAddress, withdrawAddr sdk.AccAddress) error {
	return nil
}

func (tk *TestDistributionKeeper) WithdrawDelegationRewards(ctx sdk.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) (sdk.Coins, error) {
	return nil, nil
}

func (tk *TestDistributionKeeper) DelegationTotalRewards(ctx context.Context, req *distrtypes.QueryDelegationTotalRewardsRequest) (*distrtypes.QueryDelegationTotalRewardsResponse, error) {
	uatomCoins := 1
	val1useiCoins := 5
	val2useiCoins := 7
	rewards := []distrtypes.DelegationDelegatorReward{
		{
			ValidatorAddress: "seivaloper1wuj3xg3yrw4ryxn9vygwuz0necs4klj7j9nay6",
			Reward: sdk.NewDecCoins(
				sdk.NewDecCoin("uatom", sdk.NewInt(int64(uatomCoins))),
				sdk.NewDecCoin("usei", sdk.NewInt(int64(val1useiCoins))),
			),
		},
		{
			ValidatorAddress: "seivaloper16znh8ktn33dwnxxc9q0jmxmjf6hsz4tl0s6vxh",
			Reward:           sdk.NewDecCoins(sdk.NewDecCoin("usei", sdk.NewInt(int64(val2useiCoins)))),
		},
	}

	allDecCoins := sdk.NewDecCoins(sdk.NewDecCoin("uatom", sdk.NewInt(int64(uatomCoins))),
		sdk.NewDecCoin("usei", sdk.NewInt(int64(val1useiCoins+val2useiCoins))))

	return &distrtypes.QueryDelegationTotalRewardsResponse{Rewards: rewards, Total: allDecCoins}, nil
}

type TestEmptyRewardsDistributionKeeper struct{}

func (tk *TestEmptyRewardsDistributionKeeper) SetWithdrawAddr(ctx sdk.Context, delegatorAddr sdk.AccAddress, withdrawAddr sdk.AccAddress) error {
	return nil
}

func (tk *TestEmptyRewardsDistributionKeeper) WithdrawDelegationRewards(ctx sdk.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) (sdk.Coins, error) {
	return nil, nil
}

func (tk *TestEmptyRewardsDistributionKeeper) DelegationTotalRewards(ctx context.Context, req *distrtypes.QueryDelegationTotalRewardsRequest) (*distrtypes.QueryDelegationTotalRewardsResponse, error) {
	rewards := []distrtypes.DelegationDelegatorReward{}
	allDecCoins := sdk.NewDecCoins()

	return &distrtypes.QueryDelegationTotalRewardsResponse{Rewards: rewards, Total: allDecCoins}, nil
}

func TestPrecompile_RunAndCalculateGas_Rewards(t *testing.T) {
	callerSeiAddress, callerEvmAddress := testkeeper.MockAddressPair()
	_, notAssociatedCallerEvmAddress := testkeeper.MockAddressPair()
	_, contractEvmAddress := testkeeper.MockAddressPair()
	pre, _ := distribution.NewPrecompile(nil, nil)
	rewardsMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*distribution.PrecompileExecutor).RewardsID)
	coin1 := distribution.Coin{
		Amount:   big.NewInt(1_000_000_000_000_000_000),
		Denom:    "uatom",
		Decimals: big.NewInt(18),
	}
	coin2 := distribution.Coin{
		Amount:   big.NewInt(5_000_000_000_000_000_000),
		Denom:    "usei",
		Decimals: big.NewInt(18),
	}

	coin3 := distribution.Coin{
		Amount:   big.NewInt(7_000_000_000_000_000_000),
		Denom:    "usei",
		Decimals: big.NewInt(18),
	}
	coinsVal1 := []distribution.Coin{coin1, coin2}
	coinsVal2 := []distribution.Coin{coin3}
	rewardVal1 := distribution.Reward{
		ValidatorAddress: "seivaloper1wuj3xg3yrw4ryxn9vygwuz0necs4klj7j9nay6",
		Coins:            coinsVal1,
	}
	rewardVal2 := distribution.Reward{
		ValidatorAddress: "seivaloper16znh8ktn33dwnxxc9q0jmxmjf6hsz4tl0s6vxh",
		Coins:            coinsVal2,
	}
	rewards := []distribution.Reward{rewardVal1, rewardVal2}
	coin2Amount, _ := new(big.Int).SetString("12000000000000000000", 10)
	totalCoins := []distribution.Coin{
		{
			Amount:   big.NewInt(1_000_000_000_000_000_000),
			Denom:    "uatom",
			Decimals: big.NewInt(18),
		},
		{
			Amount:   coin2Amount,
			Denom:    "usei",
			Decimals: big.NewInt(18),
		},
	}
	rewardsOutput := distribution.Rewards{
		Rewards: rewards,
		Total:   totalCoins,
	}

	happyPathPackedOutput, _ := rewardsMethod.Outputs.Pack(rewardsOutput)
	emptyCasePackedOutput, _ := rewardsMethod.Outputs.Pack(distribution.Rewards{
		Rewards: []distribution.Reward{},
		Total:   []distribution.Coin{},
	})
	type fields struct {
		Precompile                          pcommon.Precompile
		distrKeeper                         pcommon.DistributionKeeper
		evmKeeper                           pcommon.EVMKeeper
		address                             common.Address
		SetWithdrawAddrID                   []byte
		WithdrawDelegationRewardsID         []byte
		WithdrawMultipleDelegationRewardsID []byte
	}
	type args struct {
		evm              *vm.EVM
		delegatorAddress common.Address
		caller           common.Address
		callingContract  common.Address
		suppliedGas      uint64
		value            *big.Int
		readOnly         bool
	}
	tests := []struct {
		name             string
		fields           fields
		args             args
		wantRet          []byte
		wantRemainingGas uint64
		wantErr          bool
		wantErrMsg       string
	}{
		{
			name: "fails if delegator is not passed",
			fields: fields{
				distrKeeper: &TestDistributionKeeper{},
			},
			args: args{
				caller:          callerEvmAddress,
				callingContract: callerEvmAddress,
				suppliedGas:     uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "invalid addr",
		},
		{
			name: "fails if delegator not associated",
			fields: fields{
				distrKeeper: &TestDistributionKeeper{},
			},
			args: args{
				delegatorAddress: notAssociatedCallerEvmAddress,
				caller:           notAssociatedCallerEvmAddress,
				callingContract:  notAssociatedCallerEvmAddress,
				suppliedGas:      uint64(1000000),
			},
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot use an unassociated address as withdraw address",
		},
		{
			name: "fails if delegator address is invalid",
			fields: fields{
				distrKeeper: &TestDistributionKeeper{},
			},
			args: args{
				delegatorAddress: common.Address{},
				caller:           callerEvmAddress,
				callingContract:  callerEvmAddress,
				suppliedGas:      uint64(1000000),
			},
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "invalid addr",
		},
		{
			name: "fails if caller != callingContract",
			fields: fields{
				distrKeeper: &TestDistributionKeeper{},
			},
			args: args{
				delegatorAddress: common.Address{},
				caller:           callerEvmAddress,
				callingContract:  contractEvmAddress,
				suppliedGas:      uint64(1000000),
			},
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot delegatecall distr",
		},
		{
			name: "fails if caller != callingContract with callingContract not set",
			fields: fields{
				distrKeeper: &TestDistributionKeeper{},
			},
			args: args{
				delegatorAddress: common.Address{},
				caller:           callerEvmAddress,
				suppliedGas:      uint64(1000000),
			},
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       "cannot delegatecall distr",
		},
		{
			name: "should return empty delegator rewards if no rewards",
			fields: fields{
				distrKeeper: &TestEmptyRewardsDistributionKeeper{},
			},
			args: args{
				delegatorAddress: callerEvmAddress,
				readOnly:         true,
				caller:           callerEvmAddress,
				callingContract:  callerEvmAddress,
				suppliedGas:      uint64(1000000),
			},
			wantRet:          emptyCasePackedOutput,
			wantRemainingGas: 998877,
			wantErr:          false,
		},
		{
			name: "should return delegator rewards",
			fields: fields{
				distrKeeper: &TestDistributionKeeper{},
			},
			args: args{
				delegatorAddress: callerEvmAddress,
				readOnly:         true,
				caller:           callerEvmAddress,
				callingContract:  callerEvmAddress,
				suppliedGas:      uint64(1000000),
			},
			wantRet:          happyPathPackedOutput,
			wantRemainingGas: 998877,
			wantErr:          false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testApp := testkeeper.EVMTestApp
			ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
			k := &testApp.EvmKeeper
			k.SetAddressMapping(ctx, callerSeiAddress, callerEvmAddress)
			stateDb := state.NewDBImpl(ctx, k, true)
			evm := vm.EVM{
				StateDB:   stateDb,
				TxContext: vm.TxContext{Origin: callerEvmAddress},
			}
			p, _ := distribution.NewPrecompile(tt.fields.distrKeeper, k)
			rewards, err := p.ABI.MethodById(p.GetExecutor().(*distribution.PrecompileExecutor).RewardsID)
			require.Nil(t, err)
			inputs, err := rewards.Inputs.Pack(tt.args.delegatorAddress)
			require.Nil(t, err)
			gotRet, gotRemainingGas, err := p.RunAndCalculateGas(&evm, tt.args.caller, tt.args.callingContract, append(p.GetExecutor().(*distribution.PrecompileExecutor).RewardsID, inputs...), tt.args.suppliedGas, tt.args.value, nil, tt.args.readOnly)
			if (err != nil) != tt.wantErr {
				t.Errorf("RunAndCalculateGas() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Equal(t, tt.wantErrMsg, string(gotRet))
			} else if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("RunAndCalculateGas() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
			if gotRemainingGas != tt.wantRemainingGas {
				t.Errorf("RunAndCalculateGas() gotRemainingGas = %v, want %v", gotRemainingGas, tt.wantRemainingGas)
			}
		})
	}
}
