package distribution_test

import (
	"encoding/hex"
	"fmt"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	crptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	abitypes "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/precompiles/distribution"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

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
	abi := staking.GetABI()
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
	abi = distribution.GetABI()
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
	require.Equal(t, uint64(68682), res.GasUsed)

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

	abi := staking.GetABI()
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
	abi := distribution.GetABI()
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
	require.Equal(t, uint64(152848), res.GasUsed)

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
			wantErrMsg:       "delegator 0x0000000000000000000000000000000000000000 is not associated",
		},
		{
			name:   "fails if delegator is not associated",
			fields: fields{},
			args: args{
				caller:      notAssociatedCallerEvmAddress,
				validator:   validatorAddress,
				suppliedGas: uint64(1000000),
			},
			wantRet:          nil,
			wantRemainingGas: 0,
			wantErr:          true,
			wantErrMsg:       fmt.Sprintf("delegator %s is not associated", notAssociatedCallerEvmAddress.String()),
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
			withdraw, err := p.ABI.MethodById(p.WithdrawDelegationRewardsID)
			require.Nil(t, err)
			inputs, err := withdraw.Inputs.Pack(tt.args.validator)
			require.Nil(t, err)
			gotRet, gotRemainingGas, err := p.RunAndCalculateGas(&evm, tt.args.caller, tt.args.callingContract, append(p.WithdrawDelegationRewardsID, inputs...), tt.args.suppliedGas, tt.args.value, nil, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("RunAndCalculateGas() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				require.Equal(t, tt.wantErrMsg, err.Error())
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("RunAndCalculateGas() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
			if gotRemainingGas != tt.wantRemainingGas {
				t.Errorf("RunAndCalculateGas() gotRemainingGas = %v, want %v", gotRemainingGas, tt.wantRemainingGas)
			}
		})
	}
}
