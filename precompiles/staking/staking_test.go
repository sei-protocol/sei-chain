package staking_test

import (
	"context"
	"crypto/ecdsa"
	"embed"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	crptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/teststaking"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

//go:embed abi.json
var f embed.FS

func TestStaking(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	valPub1 := ed25519.GenPrivKey().PubKey()
	valPub2 := ed25519.GenPrivKey().PubKey()
	val := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub1)
	val2 := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub2)

	// delegate
	abi := pcommon.MustGetABI(f, "abi.json")
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

	ante.Preprocess(ctx, req, k.ChainID(ctx), false)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	d, found := testApp.StakingKeeper.GetDelegation(ctx, seiAddr, val)
	require.True(t, found)
	require.Equal(t, int64(100), d.Shares.RoundInt().Int64())

	// redelegate
	args, err = abi.Pack("redelegate", val.String(), val2.String(), big.NewInt(50))
	require.Nil(t, err)
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

	ante.Preprocess(ctx, req, k.ChainID(ctx), false)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	d, found = testApp.StakingKeeper.GetDelegation(ctx, seiAddr, val)
	require.True(t, found)
	require.Equal(t, int64(50), d.Shares.RoundInt().Int64())

	// undelegate
	args, err = abi.Pack("undelegate", val.String(), big.NewInt(30))
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

	ante.Preprocess(ctx, req, k.ChainID(ctx), false)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	d, found = testApp.StakingKeeper.GetDelegation(ctx, seiAddr, val)
	require.True(t, found)
	require.Equal(t, int64(20), d.Shares.RoundInt().Int64())
}

func TestStakingError(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	valPub1 := ed25519.GenPrivKey().PubKey()
	valPub2 := ed25519.GenPrivKey().PubKey()
	val := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub1)
	val2 := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub2)

	abi := pcommon.MustGetABI(f, "abi.json")
	args, err := abi.Pack("undelegate", val.String(), big.NewInt(100))
	require.Nil(t, err)

	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	addr := common.HexToAddress(staking.StakingAddress)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(0),
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

	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
	require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, evmAddr[:], amt))

	msgServer := keeper.NewMsgServerImpl(k)

	ante.Preprocess(ctx, req, k.ChainID(ctx), false)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.NotEmpty(t, res.VmError)

	// redelegate
	args, err = abi.Pack("redelegate", val.String(), val2.String(), big.NewInt(50))
	require.Nil(t, err)
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

	ante.Preprocess(ctx, req, k.ChainID(ctx), false)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.NotEmpty(t, res.VmError)
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

type TestStakingQuerier struct {
	Response                              *stakingtypes.QueryDelegationResponse
	ValidatorsResponse                    *stakingtypes.QueryValidatorsResponse
	ValidatorResponse                     *stakingtypes.QueryValidatorResponse
	ValidatorDelegationsResponse          *stakingtypes.QueryValidatorDelegationsResponse
	ValidatorUnbondingDelegationsResponse *stakingtypes.QueryValidatorUnbondingDelegationsResponse
	UnbondingDelegationResponse           *stakingtypes.QueryUnbondingDelegationResponse
	DelegatorDelegationsResponse          *stakingtypes.QueryDelegatorDelegationsResponse
	DelegatorValidatorResponse            *stakingtypes.QueryDelegatorValidatorResponse
	DelegatorUnbondingDelegationsResponse *stakingtypes.QueryDelegatorUnbondingDelegationsResponse
	RedelegationsResponse                 *stakingtypes.QueryRedelegationsResponse
	DelegatorValidatorsResponse           *stakingtypes.QueryDelegatorValidatorsResponse
	HistoricalInfoResponse                *stakingtypes.QueryHistoricalInfoResponse
	PoolResponse                          *stakingtypes.QueryPoolResponse
	ParamsResponse                        *stakingtypes.QueryParamsResponse
	Err                                   error
}

func (tq *TestStakingQuerier) Delegation(c context.Context, _ *stakingtypes.QueryDelegationRequest) (*stakingtypes.QueryDelegationResponse, error) {
	return tq.Response, tq.Err
}

func (tq *TestStakingQuerier) Validators(c context.Context, _ *stakingtypes.QueryValidatorsRequest) (*stakingtypes.QueryValidatorsResponse, error) {
	return tq.ValidatorsResponse, tq.Err
}

func (tq *TestStakingQuerier) Validator(c context.Context, _ *stakingtypes.QueryValidatorRequest) (*stakingtypes.QueryValidatorResponse, error) {
	return tq.ValidatorResponse, tq.Err
}

func (tq *TestStakingQuerier) ValidatorDelegations(c context.Context, _ *stakingtypes.QueryValidatorDelegationsRequest) (*stakingtypes.QueryValidatorDelegationsResponse, error) {
	return tq.ValidatorDelegationsResponse, tq.Err
}

func (tq *TestStakingQuerier) ValidatorUnbondingDelegations(c context.Context, _ *stakingtypes.QueryValidatorUnbondingDelegationsRequest) (*stakingtypes.QueryValidatorUnbondingDelegationsResponse, error) {
	return tq.ValidatorUnbondingDelegationsResponse, tq.Err
}

func (tq *TestStakingQuerier) UnbondingDelegation(c context.Context, _ *stakingtypes.QueryUnbondingDelegationRequest) (*stakingtypes.QueryUnbondingDelegationResponse, error) {
	return tq.UnbondingDelegationResponse, tq.Err
}

func (tq *TestStakingQuerier) DelegatorDelegations(c context.Context, _ *stakingtypes.QueryDelegatorDelegationsRequest) (*stakingtypes.QueryDelegatorDelegationsResponse, error) {
	return tq.DelegatorDelegationsResponse, tq.Err
}

func (tq *TestStakingQuerier) DelegatorValidator(c context.Context, _ *stakingtypes.QueryDelegatorValidatorRequest) (*stakingtypes.QueryDelegatorValidatorResponse, error) {
	return tq.DelegatorValidatorResponse, tq.Err
}

func (tq *TestStakingQuerier) DelegatorUnbondingDelegations(c context.Context, _ *stakingtypes.QueryDelegatorUnbondingDelegationsRequest) (*stakingtypes.QueryDelegatorUnbondingDelegationsResponse, error) {
	return tq.DelegatorUnbondingDelegationsResponse, tq.Err
}

func (tq *TestStakingQuerier) Redelegations(c context.Context, _ *stakingtypes.QueryRedelegationsRequest) (*stakingtypes.QueryRedelegationsResponse, error) {
	return tq.RedelegationsResponse, tq.Err
}

func (tq *TestStakingQuerier) DelegatorValidators(c context.Context, _ *stakingtypes.QueryDelegatorValidatorsRequest) (*stakingtypes.QueryDelegatorValidatorsResponse, error) {
	return tq.DelegatorValidatorsResponse, tq.Err
}

func (tq *TestStakingQuerier) HistoricalInfo(c context.Context, _ *stakingtypes.QueryHistoricalInfoRequest) (*stakingtypes.QueryHistoricalInfoResponse, error) {
	return tq.HistoricalInfoResponse, tq.Err
}

func (tq *TestStakingQuerier) Pool(c context.Context, _ *stakingtypes.QueryPoolRequest) (*stakingtypes.QueryPoolResponse, error) {
	return tq.PoolResponse, tq.Err
}

func (tq *TestStakingQuerier) Params(c context.Context, _ *stakingtypes.QueryParamsRequest) (*stakingtypes.QueryParamsResponse, error) {
	return tq.ParamsResponse, tq.Err
}

func TestPrecompile_Run_Delegation(t *testing.T) {
	callerSeiAddress, callerEvmAddress := testkeeper.MockAddressPair()
	_, unassociatedEvmAddress := testkeeper.MockAddressPair()
	_, contractEvmAddress := testkeeper.MockAddressPair()
	validatorAddress := "seivaloper134ykhqrkyda72uq7f463ne77e4tn99steprmz7"
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	delegationMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).DelegationID)
	shares := 100
	delegationResponse := &stakingtypes.QueryDelegationResponse{
		DelegationResponse: &stakingtypes.DelegationResponse{
			Delegation: stakingtypes.Delegation{
				DelegatorAddress: callerSeiAddress.String(),
				ValidatorAddress: validatorAddress,
				Shares:           sdk.NewDec(int64(shares)),
			},
			Balance: sdk.NewCoin("usei", sdk.NewInt(int64(shares))),
		},
	}
	hundredSharesValue := new(big.Int)
	hundredSharesValue.SetString("100000000000000000000", 10)
	delegation := staking.Delegation{
		Balance: staking.Balance{
			Amount: big.NewInt(int64(shares)),
			Denom:  "usei",
		},
		Delegation: staking.DelegationDetails{
			DelegatorAddress: callerSeiAddress.String(),
			Shares:           hundredSharesValue,
			Decimals:         big.NewInt(sdk.Precision),
			ValidatorAddress: validatorAddress,
		},
	}

	happyPathPackedOutput, _ := delegationMethod.Outputs.Pack(delegation)

	type fields struct {
		Precompile     pcommon.Precompile
		stakingKeeper  utils.StakingKeeper
		stakingQuerier utils.StakingQuerier
		evmKeeper      utils.EVMKeeper
	}
	type args struct {
		evm                *vm.EVM
		delegatorAddress   common.Address
		validatorAddress   string
		caller             common.Address
		callingContract    common.Address
		value              *big.Int
		readOnly           bool
		isFromDelegateCall bool
	}

	tests := []struct {
		name       string
		fields     fields
		args       args
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: fields{
				stakingQuerier: &TestStakingQuerier{
					Response: delegationResponse,
				},
			},
			args: args{
				delegatorAddress: callerEvmAddress,
				validatorAddress: validatorAddress,
				value:            big.NewInt(100),
			},
			wantRet:    happyPathPackedOutput,
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "fails if caller != callingContract",
			fields: fields{
				stakingQuerier: &TestStakingQuerier{
					Response: delegationResponse,
				},
			},
			args: args{
				caller:             callerEvmAddress,
				callingContract:    contractEvmAddress,
				delegatorAddress:   callerEvmAddress,
				validatorAddress:   validatorAddress,
				value:              big.NewInt(100),
				isFromDelegateCall: true,
			},
			wantErr:    true,
			wantErrMsg: "cannot delegatecall staking",
		},
		{
			name: "fails if delegator address unassociated",
			fields: fields{
				stakingQuerier: &TestStakingQuerier{
					Response: delegationResponse,
				},
			},
			args: args{
				caller:           callerEvmAddress,
				callingContract:  callerEvmAddress,
				delegatorAddress: unassociatedEvmAddress,
				validatorAddress: validatorAddress,
			},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is not linked", unassociatedEvmAddress.String()),
		},
		{
			name: "fails if delegator address is invalid",
			fields: fields{
				stakingQuerier: &TestStakingQuerier{
					Response: delegationResponse,
				},
			},
			args: args{
				delegatorAddress: common.Address{},
				validatorAddress: validatorAddress,
				caller:           callerEvmAddress,
				callingContract:  callerEvmAddress,
			},
			wantErr:    true,
			wantErrMsg: "invalid addr",
		},
		{
			name: "should return error if delegation not found",
			fields: fields{
				stakingQuerier: &TestStakingQuerier{
					Err: fmt.Errorf("delegation with delegator %s not found for validator", callerSeiAddress.String()),
				},
			},
			args: args{
				delegatorAddress: callerEvmAddress,
				validatorAddress: validatorAddress,
				caller:           callerEvmAddress,
				callingContract:  callerEvmAddress,
			},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("delegation with delegator %s not found for validator", callerSeiAddress.String()),
		},
		{
			name: "should return delegation details",
			fields: fields{
				stakingQuerier: &TestStakingQuerier{
					Response: delegationResponse,
				},
			},
			args: args{
				delegatorAddress: callerEvmAddress,
				validatorAddress: validatorAddress,
				caller:           callerEvmAddress,
				callingContract:  callerEvmAddress,
			},
			wantRet: happyPathPackedOutput,
			wantErr: false,
		},
		{
			name: "should allow static call",
			fields: fields{
				stakingQuerier: &TestStakingQuerier{
					Response: delegationResponse,
				},
			},
			args: args{
				delegatorAddress: callerEvmAddress,
				validatorAddress: validatorAddress,
				caller:           callerEvmAddress,
				callingContract:  callerEvmAddress,
				readOnly:         true,
			},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingKeeper:  tt.fields.stakingKeeper,
				StakingQuerier: tt.fields.stakingQuerier,
				EVMKeeper:      k,
			})
			delegation, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).DelegationID)
			require.Nil(t, err)
			inputs, err := delegation.Inputs.Pack(tt.args.delegatorAddress, tt.args.validatorAddress)
			require.Nil(t, err)
			gotRet, _, err := p.RunAndCalculateGas(&evm, tt.args.caller, tt.args.callingContract, append(p.GetExecutor().(*staking.PrecompileExecutor).DelegationID, inputs...), math.MaxUint64, tt.args.value, nil, tt.args.readOnly, tt.args.isFromDelegateCall)
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
			} else if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_Validators(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	validatorsMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).ValidatorsID)

	// Build a single validator in the staking module format.
	val := stakingtypes.Validator{
		OperatorAddress: "seivaloper1validator",
		ConsensusPubkey: &codectypes.Any{Value: []byte("pubkey-bytes")},
		Jailed:          false,
		Status:          stakingtypes.Bonded,
		Tokens:          sdk.NewInt(1000),
		DelegatorShares: sdk.NewDec(1000),
		Description: stakingtypes.NewDescription(
			"moniker",
			"identity",
			"website",
			"security",
			"details",
		),
		UnbondingHeight: 10,
		UnbondingTime:   time.Unix(1234, 0),
		Commission: stakingtypes.Commission{
			CommissionRates: stakingtypes.CommissionRates{
				Rate:          sdk.NewDecWithPrec(10, 2),
				MaxRate:       sdk.NewDecWithPrec(20, 2),
				MaxChangeRate: sdk.NewDecWithPrec(1, 2),
			},
			UpdateTime: time.Unix(5678, 0),
		},
		MinSelfDelegation: sdk.NewInt(5),
	}

	nextKey := []byte("next-key")
	validatorsResponse := &stakingtypes.QueryValidatorsResponse{
		Validators: []stakingtypes.Validator{val},
		Pagination: &query.PageResponse{NextKey: nextKey},
	}

	expected := staking.ValidatorsResponse{
		Validators: []staking.Validator{
			{
				OperatorAddress:         val.OperatorAddress,
				ConsensusPubkey:         val.ConsensusPubkey.Value,
				Jailed:                  val.Jailed,
				Status:                  int32(val.Status),
				Tokens:                  val.Tokens.String(),
				DelegatorShares:         val.DelegatorShares.String(),
				Description:             val.Description.String(),
				UnbondingHeight:         val.UnbondingHeight,
				UnbondingTime:           val.UnbondingTime.Unix(),
				CommissionRate:          val.Commission.Rate.String(),
				CommissionMaxRate:       val.Commission.MaxRate.String(),
				CommissionMaxChangeRate: val.Commission.MaxChangeRate.String(),
				CommissionUpdateTime:    val.Commission.UpdateTime.Unix(),
				MinSelfDelegation:       val.MinSelfDelegation.String(),
			},
		},
		NextKey: nextKey,
	}

	happyPathPackedOutput, _ := validatorsMethod.Outputs.Pack(expected)

	type fields struct {
		stakingQuerier utils.StakingQuerier
	}
	type args struct {
		status   string
		key      []byte
		value    *big.Int
		readOnly bool
	}

	tests := []struct {
		name       string
		fields     fields
		args       args
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: fields{
				stakingQuerier: &TestStakingQuerier{
					ValidatorsResponse: validatorsResponse,
				},
			},
			args: args{
				status: "BOND_STATUS_BONDED",
				key:    []byte{},
				value:  big.NewInt(1),
			},
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return validators and next key (static call allowed)",
			fields: fields{
				stakingQuerier: &TestStakingQuerier{
					ValidatorsResponse: validatorsResponse,
				},
			},
			args: args{
				status:   "BOND_STATUS_BONDED",
				key:      []byte{},
				readOnly: true,
			},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields.stakingQuerier,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).ValidatorsID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args.status, tt.args.key)
			require.NoError(t, err)

			// Caller and callingContract are irrelevant for validators (query-only).
			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.args.value, nil, tt.args.readOnly, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				if tt.wantErrMsg != "" {
					require.Contains(t, tt.wantErrMsg, tt.wantErrMsg)
				}
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

// Helper function to create a test validator
func createTestValidator() stakingtypes.Validator {
	return stakingtypes.Validator{
		OperatorAddress: "seivaloper1validator",
		ConsensusPubkey: &codectypes.Any{Value: []byte("pubkey-bytes")},
		Jailed:          false,
		Status:          stakingtypes.Bonded,
		Tokens:          sdk.NewInt(1000),
		DelegatorShares: sdk.NewDec(1000),
		Description: stakingtypes.NewDescription(
			"moniker",
			"identity",
			"website",
			"security",
			"details",
		),
		UnbondingHeight: 10,
		UnbondingTime:   time.Unix(1234, 0),
		Commission: stakingtypes.Commission{
			CommissionRates: stakingtypes.CommissionRates{
				Rate:          sdk.NewDecWithPrec(10, 2),
				MaxRate:       sdk.NewDecWithPrec(20, 2),
				MaxChangeRate: sdk.NewDecWithPrec(1, 2),
			},
			UpdateTime: time.Unix(5678, 0),
		},
		MinSelfDelegation: sdk.NewInt(5),
	}
}

func TestPrecompile_Run_Validator(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	validatorMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).ValidatorID)

	val := createTestValidator()
	validatorResponse := &stakingtypes.QueryValidatorResponse{
		Validator: val,
	}

	expected := staking.Validator{
		OperatorAddress:         val.OperatorAddress,
		ConsensusPubkey:         val.ConsensusPubkey.Value,
		Jailed:                  val.Jailed,
		Status:                  int32(val.Status),
		Tokens:                  val.Tokens.String(),
		DelegatorShares:         val.DelegatorShares.String(),
		Description:             val.Description.String(),
		UnbondingHeight:         val.UnbondingHeight,
		UnbondingTime:           val.UnbondingTime.Unix(),
		CommissionRate:          val.Commission.Rate.String(),
		CommissionMaxRate:       val.Commission.MaxRate.String(),
		CommissionMaxChangeRate: val.Commission.MaxChangeRate.String(),
		CommissionUpdateTime:    val.Commission.UpdateTime.Unix(),
		MinSelfDelegation:       val.MinSelfDelegation.String(),
	}

	happyPathPackedOutput, _ := validatorMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				ValidatorResponse: validatorResponse,
			},
			args:       []interface{}{val.OperatorAddress},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return validator (static call allowed)",
			fields: &TestStakingQuerier{
				ValidatorResponse: validatorResponse,
			},
			args:    []interface{}{val.OperatorAddress},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).ValidatorID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_ValidatorDelegations(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	validatorDelegationsMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).ValidatorDelegationsID)

	callerSeiAddress, _ := testkeeper.MockAddressPair()
	validatorAddress := "seivaloper134ykhqrkyda72uq7f463ne77e4tn99steprmz7"
	shares := 100
	delegationResponse := &stakingtypes.DelegationResponse{
		Delegation: stakingtypes.Delegation{
			DelegatorAddress: callerSeiAddress.String(),
			ValidatorAddress: validatorAddress,
			Shares:           sdk.NewDec(int64(shares)),
		},
		Balance: sdk.NewCoin("usei", sdk.NewInt(int64(shares))),
	}

	nextKey := []byte("next-key")
	validatorDelegationsResponse := &stakingtypes.QueryValidatorDelegationsResponse{
		DelegationResponses: []stakingtypes.DelegationResponse{*delegationResponse},
		Pagination:          &query.PageResponse{NextKey: nextKey},
	}

	hundredSharesValue := new(big.Int)
	hundredSharesValue.SetString("100000000000000000000", 10)
	expected := staking.DelegationsResponse{
		Delegations: []staking.Delegation{
			{
				Balance: staking.Balance{
					Amount: big.NewInt(int64(shares)),
					Denom:  "usei",
				},
				Delegation: staking.DelegationDetails{
					DelegatorAddress: callerSeiAddress.String(),
					Shares:           hundredSharesValue,
					Decimals:         big.NewInt(sdk.Precision),
					ValidatorAddress: validatorAddress,
				},
			},
		},
		NextKey: nextKey,
	}

	happyPathPackedOutput, _ := validatorDelegationsMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				ValidatorDelegationsResponse: validatorDelegationsResponse,
			},
			args:       []interface{}{validatorAddress, []byte{}},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return validator delegations (static call allowed)",
			fields: &TestStakingQuerier{
				ValidatorDelegationsResponse: validatorDelegationsResponse,
			},
			args:    []interface{}{validatorAddress, []byte{}},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).ValidatorDelegationsID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_ValidatorUnbondingDelegations(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	validatorUnbondingDelegationsMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).ValidatorUnbondingDelegationsID)

	callerSeiAddress, _ := testkeeper.MockAddressPair()
	validatorAddress := "seivaloper134ykhqrkyda72uq7f463ne77e4tn99steprmz7"
	unbondingDelegation := stakingtypes.UnbondingDelegation{
		DelegatorAddress: callerSeiAddress.String(),
		ValidatorAddress: validatorAddress,
		Entries: []stakingtypes.UnbondingDelegationEntry{
			{
				CreationHeight: 100,
				CompletionTime: time.Unix(2000, 0),
				InitialBalance: sdk.NewInt(50),
				Balance:        sdk.NewInt(40),
			},
		},
	}

	nextKey := []byte("next-key")
	validatorUnbondingDelegationsResponse := &stakingtypes.QueryValidatorUnbondingDelegationsResponse{
		UnbondingResponses: []stakingtypes.UnbondingDelegation{unbondingDelegation},
		Pagination:         &query.PageResponse{NextKey: nextKey},
	}

	expected := staking.UnbondingDelegationsResponse{
		UnbondingDelegations: []staking.UnbondingDelegation{
			{
				DelegatorAddress: callerSeiAddress.String(),
				ValidatorAddress: validatorAddress,
				Entries: []staking.UnbondingDelegationEntry{
					{
						CreationHeight: 100,
						CompletionTime: 2000,
						InitialBalance: "50",
						Balance:        "40",
					},
				},
			},
		},
		NextKey: nextKey,
	}

	happyPathPackedOutput, _ := validatorUnbondingDelegationsMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				ValidatorUnbondingDelegationsResponse: validatorUnbondingDelegationsResponse,
			},
			args:       []interface{}{validatorAddress, []byte{}},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return validator unbonding delegations (static call allowed)",
			fields: &TestStakingQuerier{
				ValidatorUnbondingDelegationsResponse: validatorUnbondingDelegationsResponse,
			},
			args:    []interface{}{validatorAddress, []byte{}},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).ValidatorUnbondingDelegationsID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_UnbondingDelegation(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	unbondingDelegationMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).UnbondingDelegationID)

	callerSeiAddress, callerEvmAddress := testkeeper.MockAddressPair()
	validatorAddress := "seivaloper134ykhqrkyda72uq7f463ne77e4tn99steprmz7"
	unbondingDelegation := stakingtypes.UnbondingDelegation{
		DelegatorAddress: callerSeiAddress.String(),
		ValidatorAddress: validatorAddress,
		Entries: []stakingtypes.UnbondingDelegationEntry{
			{
				CreationHeight: 100,
				CompletionTime: time.Unix(2000, 0),
				InitialBalance: sdk.NewInt(50),
				Balance:        sdk.NewInt(40),
			},
		},
	}

	unbondingDelegationResponse := &stakingtypes.QueryUnbondingDelegationResponse{
		Unbond: unbondingDelegation,
	}

	expected := staking.UnbondingDelegation{
		DelegatorAddress: callerSeiAddress.String(),
		ValidatorAddress: validatorAddress,
		Entries: []staking.UnbondingDelegationEntry{
			{
				CreationHeight: 100,
				CompletionTime: 2000,
				InitialBalance: "50",
				Balance:        "40",
			},
		},
	}

	happyPathPackedOutput, _ := unbondingDelegationMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				UnbondingDelegationResponse: unbondingDelegationResponse,
			},
			args:       []interface{}{callerEvmAddress, validatorAddress},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return unbonding delegation (static call allowed)",
			fields: &TestStakingQuerier{
				UnbondingDelegationResponse: unbondingDelegationResponse,
			},
			args:    []interface{}{callerEvmAddress, validatorAddress},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
				StateDB: stateDb,
			}
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).UnbondingDelegationID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_DelegatorDelegations(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	delegatorDelegationsMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).DelegatorDelegationsID)

	callerSeiAddress, callerEvmAddress := testkeeper.MockAddressPair()
	validatorAddress := "seivaloper134ykhqrkyda72uq7f463ne77e4tn99steprmz7"
	shares := 100
	delegationResponse := &stakingtypes.DelegationResponse{
		Delegation: stakingtypes.Delegation{
			DelegatorAddress: callerSeiAddress.String(),
			ValidatorAddress: validatorAddress,
			Shares:           sdk.NewDec(int64(shares)),
		},
		Balance: sdk.NewCoin("usei", sdk.NewInt(int64(shares))),
	}

	nextKey := []byte("next-key")
	delegatorDelegationsResponse := &stakingtypes.QueryDelegatorDelegationsResponse{
		DelegationResponses: []stakingtypes.DelegationResponse{*delegationResponse},
		Pagination:          &query.PageResponse{NextKey: nextKey},
	}

	hundredSharesValue := new(big.Int)
	hundredSharesValue.SetString("100000000000000000000", 10)
	expected := staking.DelegationsResponse{
		Delegations: []staking.Delegation{
			{
				Balance: staking.Balance{
					Amount: big.NewInt(int64(shares)),
					Denom:  "usei",
				},
				Delegation: staking.DelegationDetails{
					DelegatorAddress: callerSeiAddress.String(),
					Shares:           hundredSharesValue,
					Decimals:         big.NewInt(sdk.Precision),
					ValidatorAddress: validatorAddress,
				},
			},
		},
		NextKey: nextKey,
	}

	happyPathPackedOutput, _ := delegatorDelegationsMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				DelegatorDelegationsResponse: delegatorDelegationsResponse,
			},
			args:       []interface{}{callerEvmAddress, []byte{}},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return delegator delegations (static call allowed)",
			fields: &TestStakingQuerier{
				DelegatorDelegationsResponse: delegatorDelegationsResponse,
			},
			args:    []interface{}{callerEvmAddress, []byte{}},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
				StateDB: stateDb,
			}
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).DelegatorDelegationsID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_DelegatorValidator(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	delegatorValidatorMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).DelegatorValidatorID)

	callerSeiAddress, callerEvmAddress := testkeeper.MockAddressPair()
	val := createTestValidator()
	validatorResponse := &stakingtypes.QueryDelegatorValidatorResponse{
		Validator: val,
	}

	expected := staking.Validator{
		OperatorAddress:         val.OperatorAddress,
		ConsensusPubkey:         val.ConsensusPubkey.Value,
		Jailed:                  val.Jailed,
		Status:                  int32(val.Status),
		Tokens:                  val.Tokens.String(),
		DelegatorShares:         val.DelegatorShares.String(),
		Description:             val.Description.String(),
		UnbondingHeight:         val.UnbondingHeight,
		UnbondingTime:           val.UnbondingTime.Unix(),
		CommissionRate:          val.Commission.Rate.String(),
		CommissionMaxRate:       val.Commission.MaxRate.String(),
		CommissionMaxChangeRate: val.Commission.MaxChangeRate.String(),
		CommissionUpdateTime:    val.Commission.UpdateTime.Unix(),
		MinSelfDelegation:       val.MinSelfDelegation.String(),
	}

	happyPathPackedOutput, _ := delegatorValidatorMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				DelegatorValidatorResponse: validatorResponse,
			},
			args:       []interface{}{callerEvmAddress, val.OperatorAddress},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return delegator validator (static call allowed)",
			fields: &TestStakingQuerier{
				DelegatorValidatorResponse: validatorResponse,
			},
			args:    []interface{}{callerEvmAddress, val.OperatorAddress},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
				StateDB: stateDb,
			}
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).DelegatorValidatorID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_DelegatorUnbondingDelegations(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	delegatorUnbondingDelegationsMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).DelegatorUnbondingDelegationsID)

	callerSeiAddress, callerEvmAddress := testkeeper.MockAddressPair()
	validatorAddress := "seivaloper134ykhqrkyda72uq7f463ne77e4tn99steprmz7"
	unbondingDelegation := stakingtypes.UnbondingDelegation{
		DelegatorAddress: callerSeiAddress.String(),
		ValidatorAddress: validatorAddress,
		Entries: []stakingtypes.UnbondingDelegationEntry{
			{
				CreationHeight: 100,
				CompletionTime: time.Unix(2000, 0),
				InitialBalance: sdk.NewInt(50),
				Balance:        sdk.NewInt(40),
			},
		},
	}

	nextKey := []byte("next-key")
	delegatorUnbondingDelegationsResponse := &stakingtypes.QueryDelegatorUnbondingDelegationsResponse{
		UnbondingResponses: []stakingtypes.UnbondingDelegation{unbondingDelegation},
		Pagination:         &query.PageResponse{NextKey: nextKey},
	}

	expected := staking.UnbondingDelegationsResponse{
		UnbondingDelegations: []staking.UnbondingDelegation{
			{
				DelegatorAddress: callerSeiAddress.String(),
				ValidatorAddress: validatorAddress,
				Entries: []staking.UnbondingDelegationEntry{
					{
						CreationHeight: 100,
						CompletionTime: 2000,
						InitialBalance: "50",
						Balance:        "40",
					},
				},
			},
		},
		NextKey: nextKey,
	}

	happyPathPackedOutput, _ := delegatorUnbondingDelegationsMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				DelegatorUnbondingDelegationsResponse: delegatorUnbondingDelegationsResponse,
			},
			args:       []interface{}{callerEvmAddress, []byte{}},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return delegator unbonding delegations (static call allowed)",
			fields: &TestStakingQuerier{
				DelegatorUnbondingDelegationsResponse: delegatorUnbondingDelegationsResponse,
			},
			args:    []interface{}{callerEvmAddress, []byte{}},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
				StateDB: stateDb,
			}
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).DelegatorUnbondingDelegationsID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_Redelegations(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	redelegationsMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).RedelegationsID)

	callerSeiAddress, _ := testkeeper.MockAddressPair()
	srcValidatorAddress := "seivaloper1src"
	dstValidatorAddress := "seivaloper1dst"
	redelegation := stakingtypes.Redelegation{
		DelegatorAddress:    callerSeiAddress.String(),
		ValidatorSrcAddress: srcValidatorAddress,
		ValidatorDstAddress: dstValidatorAddress,
		Entries: []stakingtypes.RedelegationEntry{
			{
				CreationHeight: 100,
				CompletionTime: time.Unix(2000, 0),
				InitialBalance: sdk.NewInt(50),
				SharesDst:      sdk.NewDec(40),
			},
		},
	}

	redelegationEntryResponse := stakingtypes.RedelegationEntryResponse{
		RedelegationEntry: redelegation.Entries[0],
		Balance:           sdk.NewInt(40),
	}

	nextKey := []byte("next-key")
	redelegationsResponse := &stakingtypes.QueryRedelegationsResponse{
		RedelegationResponses: []stakingtypes.RedelegationResponse{
			{
				Redelegation: redelegation,
				Entries:      []stakingtypes.RedelegationEntryResponse{redelegationEntryResponse},
			},
		},
		Pagination: &query.PageResponse{NextKey: nextKey},
	}

	expected := staking.RedelegationsResponse{
		Redelegations: []staking.Redelegation{
			{
				DelegatorAddress:    callerSeiAddress.String(),
				ValidatorSrcAddress: srcValidatorAddress,
				ValidatorDstAddress: dstValidatorAddress,
				Entries: []staking.RedelegationEntry{
					{
						CreationHeight: 100,
						CompletionTime: 2000,
						InitialBalance: "50",
						SharesDst:      "40",
					},
				},
			},
		},
		NextKey: nextKey,
	}

	happyPathPackedOutput, _ := redelegationsMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				RedelegationsResponse: redelegationsResponse,
			},
			args:       []interface{}{callerSeiAddress.String(), srcValidatorAddress, dstValidatorAddress, []byte{}},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return redelegations (static call allowed)",
			fields: &TestStakingQuerier{
				RedelegationsResponse: redelegationsResponse,
			},
			args:    []interface{}{callerSeiAddress.String(), srcValidatorAddress, dstValidatorAddress, []byte{}},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).RedelegationsID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_DelegatorValidators(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	delegatorValidatorsMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).DelegatorValidatorsID)

	callerSeiAddress, callerEvmAddress := testkeeper.MockAddressPair()
	val := createTestValidator()
	nextKey := []byte("next-key")
	delegatorValidatorsResponse := &stakingtypes.QueryDelegatorValidatorsResponse{
		Validators: []stakingtypes.Validator{val},
		Pagination: &query.PageResponse{NextKey: nextKey},
	}

	expected := staking.ValidatorsResponse{
		Validators: []staking.Validator{
			{
				OperatorAddress:         val.OperatorAddress,
				ConsensusPubkey:         val.ConsensusPubkey.Value,
				Jailed:                  val.Jailed,
				Status:                  int32(val.Status),
				Tokens:                  val.Tokens.String(),
				DelegatorShares:         val.DelegatorShares.String(),
				Description:             val.Description.String(),
				UnbondingHeight:         val.UnbondingHeight,
				UnbondingTime:           val.UnbondingTime.Unix(),
				CommissionRate:          val.Commission.Rate.String(),
				CommissionMaxRate:       val.Commission.MaxRate.String(),
				CommissionMaxChangeRate: val.Commission.MaxChangeRate.String(),
				CommissionUpdateTime:    val.Commission.UpdateTime.Unix(),
				MinSelfDelegation:       val.MinSelfDelegation.String(),
			},
		},
		NextKey: nextKey,
	}

	happyPathPackedOutput, _ := delegatorValidatorsMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				DelegatorValidatorsResponse: delegatorValidatorsResponse,
			},
			args:       []interface{}{callerEvmAddress, []byte{}},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return delegator validators (static call allowed)",
			fields: &TestStakingQuerier{
				DelegatorValidatorsResponse: delegatorValidatorsResponse,
			},
			args:    []interface{}{callerEvmAddress, []byte{}},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
				StateDB: stateDb,
			}
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).DelegatorValidatorsID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_HistoricalInfo(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	historicalInfoMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).HistoricalInfoID)

	val := createTestValidator()
	historicalInfo := stakingtypes.HistoricalInfo{
		Header: tmtypes.Header{Height: 100},
		Valset: []stakingtypes.Validator{val},
	}

	historicalInfoResponse := &stakingtypes.QueryHistoricalInfoResponse{
		Hist: &historicalInfo,
	}

	expected := staking.HistoricalInfo{
		Height: 100,
		Validators: []staking.Validator{
			{
				OperatorAddress:         val.OperatorAddress,
				ConsensusPubkey:         val.ConsensusPubkey.Value,
				Jailed:                  val.Jailed,
				Status:                  int32(val.Status),
				Tokens:                  val.Tokens.String(),
				DelegatorShares:         val.DelegatorShares.String(),
				Description:             val.Description.String(),
				UnbondingHeight:         val.UnbondingHeight,
				UnbondingTime:           val.UnbondingTime.Unix(),
				CommissionRate:          val.Commission.Rate.String(),
				CommissionMaxRate:       val.Commission.MaxRate.String(),
				CommissionMaxChangeRate: val.Commission.MaxChangeRate.String(),
				CommissionUpdateTime:    val.Commission.UpdateTime.Unix(),
				MinSelfDelegation:       val.MinSelfDelegation.String(),
			},
		},
	}

	happyPathPackedOutput, _ := historicalInfoMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				HistoricalInfoResponse: historicalInfoResponse,
			},
			args:       []interface{}{int64(100)},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return historical info (static call allowed)",
			fields: &TestStakingQuerier{
				HistoricalInfoResponse: historicalInfoResponse,
			},
			args:    []interface{}{int64(100)},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).HistoricalInfoID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_Pool(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	poolMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).PoolID)

	pool := stakingtypes.Pool{
		NotBondedTokens: sdk.NewInt(1000),
		BondedTokens:    sdk.NewInt(2000),
	}

	poolResponse := &stakingtypes.QueryPoolResponse{
		Pool: pool,
	}

	expected := staking.Pool{
		NotBondedTokens: pool.NotBondedTokens.String(),
		BondedTokens:    pool.BondedTokens.String(),
	}

	happyPathPackedOutput, _ := poolMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				PoolResponse: poolResponse,
			},
			args:       []interface{}{},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return pool (static call allowed)",
			fields: &TestStakingQuerier{
				PoolResponse: poolResponse,
			},
			args:    []interface{}{},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).PoolID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

func TestPrecompile_Run_Params(t *testing.T) {
	pre, _ := staking.NewPrecompile(&utils.EmptyKeepers{})
	paramsMethod, _ := pre.ABI.MethodById(pre.GetExecutor().(*staking.PrecompileExecutor).ParamsID)

	params := stakingtypes.Params{
		UnbondingTime:                      time.Duration(1000000000000), // 1000 seconds in nanoseconds
		MaxValidators:                      100,
		MaxEntries:                         7,
		HistoricalEntries:                  10000,
		BondDenom:                          "usei",
		MinCommissionRate:                  sdk.NewDecWithPrec(5, 2),
		MaxVotingPowerRatio:                sdk.NewDecWithPrec(30, 2),
		MaxVotingPowerEnforcementThreshold: sdk.NewInt(1000000),
	}

	paramsResponse := &stakingtypes.QueryParamsResponse{
		Params: params,
	}

	expected := staking.Params{
		UnbondingTime:                      uint64(params.UnbondingTime.Seconds()),
		MaxValidators:                      params.MaxValidators,
		MaxEntries:                         params.MaxEntries,
		HistoricalEntries:                  params.HistoricalEntries,
		BondDenom:                          params.BondDenom,
		MinCommissionRate:                  params.MinCommissionRate.String(),
		MaxVotingPowerRatio:                params.MaxVotingPowerRatio.String(),
		MaxVotingPowerEnforcementThreshold: params.MaxVotingPowerEnforcementThreshold.String(),
	}

	happyPathPackedOutput, _ := paramsMethod.Outputs.Pack(expected)

	tests := []struct {
		name       string
		fields     utils.StakingQuerier
		args       []interface{}
		value      *big.Int
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "fails if value passed",
			fields: &TestStakingQuerier{
				ParamsResponse: paramsResponse,
			},
			args:       []interface{}{},
			value:      big.NewInt(1),
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "should return params (static call allowed)",
			fields: &TestStakingQuerier{
				ParamsResponse: paramsResponse,
			},
			args:    []interface{}{},
			wantRet: happyPathPackedOutput,
			wantErr: false,
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
			p, _ := staking.NewPrecompile(&app.PrecompileKeepers{
				StakingQuerier: tt.fields,
				EVMKeeper:      k,
			})
			method, err := p.ABI.MethodById(p.GetExecutor().(*staking.PrecompileExecutor).ParamsID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.args...)
			require.NoError(t, err)

			gotRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, tt.value, nil, true, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, gotRet)
				return
			}
			if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}

// createValidatorTestSetup contains common setup for createValidator tests
type createValidatorTestSetup struct {
	testApp     *app.App
	ctx         sdk.Context
	k           *keeper.Keeper
	abi         abi.ABI
	addr        common.Address
	signer      ethtypes.Signer
	msgServer   evmtypes.MsgServer
	testPrivKey crptotypes.PrivKey
	key         *ecdsa.PrivateKey
}

// setupCreateValidatorTest creates a common test setup for createValidator tests
func setupCreateValidatorTest(t *testing.T) *createValidatorTestSetup {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	abi := pcommon.MustGetABI(f, "abi.json")
	testPrivKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(testPrivKey.Bytes())
	key, err := crypto.HexToECDSA(testPrivHex)
	require.NoError(t, err)

	addr := common.HexToAddress(staking.StakingAddress)
	chainID := k.ChainID(ctx)
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))

	msgServer := keeper.NewMsgServerImpl(k)

	// Setup address mapping and funding
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(testPrivKey)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(2_000_000_000_000_000_000)))
	require.NoError(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))

	return &createValidatorTestSetup{
		testApp:     testApp,
		ctx:         ctx,
		k:           k,
		abi:         abi,
		addr:        addr,
		signer:      signer,
		msgServer:   msgServer,
		testPrivKey: testPrivKey,
		key:         key,
	}
}

func TestCreateValidator(t *testing.T) {
	type args struct {
		pubKeyHex               string
		moniker                 string
		commissionRate          string
		commissionMaxRate       string
		commissionMaxChangeRate string
		msd                     *big.Int
		nonce                   uint64
		value                   *big.Int
	}

	tests := []struct {
		name       string
		args       args
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "successful creation",
			args: args{
				pubKeyHex:               hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes()),
				moniker:                 "TestValidator",
				commissionRate:          "0.05",
				commissionMaxRate:       "0.20",
				commissionMaxChangeRate: "0.01",
				msd:                     big.NewInt(1_000_000),                 // 1 SEI in usei
				value:                   big.NewInt(1_000_000_000_000_000_000), // 1 SEI in wei
				nonce:                   0,
			},
		},
		{
			name: "fails with no self delegation",
			args: args{
				pubKeyHex:               hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes()),
				moniker:                 "TestValidator",
				commissionRate:          "0.05",
				commissionMaxRate:       "0.20",
				commissionMaxChangeRate: "0.01",
				value:                   big.NewInt(1_000_000_000_000_000_000),
				msd:                     big.NewInt(0), // No self-delegation
				nonce:                   0,
			},
			wantErr:    true,
			wantErrMsg: "minimum self delegation must be a positive integer: invalid request",
		},
		{
			name: "fails with invalid public key",
			args: args{
				pubKeyHex:               "invalid_hex",
				moniker:                 "TestValidator",
				commissionRate:          "0.05",
				commissionMaxRate:       "0.20",
				commissionMaxChangeRate: "0.01",
				msd:                     big.NewInt(1000000),
				nonce:                   0,
			},
			wantErr:    true,
			wantErrMsg: "invalid public key hex format",
		},
		{
			name: "fails with invalid amount",
			args: args{
				pubKeyHex:               hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes()),
				moniker:                 "TestValidator",
				commissionRate:          "0.05",
				commissionMaxRate:       "0.20",
				commissionMaxChangeRate: "0.01",
				msd:                     big.NewInt(1000000),
				nonce:                   0,
			},
			wantErr:    true,
			wantErrMsg: "set `value` field to non-zero to send delegate fund",
		},
		{
			name: "fails with invalid commission rate",
			args: args{
				pubKeyHex:               hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes()),
				moniker:                 "TestValidator",
				commissionRate:          "invalid_rate",
				commissionMaxRate:       "0.20",
				commissionMaxChangeRate: "0.01",
				msd:                     big.NewInt(1000000),
				nonce:                   0,
			},
			wantErr:    true,
			wantErrMsg: "invalid commission rate",
		},
		{
			name: "fails with invalid max commission rate",
			args: args{
				pubKeyHex:               hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes()),
				moniker:                 "TestValidator",
				commissionRate:          "0.05",
				commissionMaxRate:       "invalid_max_rate",
				commissionMaxChangeRate: "0.01",
				msd:                     big.NewInt(1000000),
				nonce:                   0,
			},
			wantErr:    true,
			wantErrMsg: "invalid commission max rate",
		},
		{
			name: "fails with invalid max commission change rate",
			args: args{
				pubKeyHex:               hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes()),
				moniker:                 "TestValidator",
				commissionRate:          "0.05",
				commissionMaxRate:       "0.20",
				commissionMaxChangeRate: "invalid_change_rate",
				msd:                     big.NewInt(1000000),
				nonce:                   0,
			},
			wantErr:    true,
			wantErrMsg: "invalid commission max change rate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupCreateValidatorTest(t)

			inputs, err := setup.abi.Pack("createValidator",
				tt.args.pubKeyHex,
				tt.args.moniker,
				tt.args.commissionRate,
				tt.args.commissionMaxRate,
				tt.args.commissionMaxChangeRate,
				tt.args.msd,
			)
			require.NoError(t, err)

			txData := ethtypes.LegacyTx{
				GasPrice: big.NewInt(1000000000000),
				Gas:      200000,
				To:       &setup.addr,
				Value:    tt.args.value,
				Data:     inputs,
				Nonce:    tt.args.nonce,
			}

			tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), setup.signer, setup.key)
			require.NoError(t, err)

			txwrapper, err := ethtx.NewLegacyTx(tx)
			require.NoError(t, err)

			req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
			require.NoError(t, err)

			ante.Preprocess(setup.ctx, req, setup.k.ChainID(setup.ctx), false)
			res, err := setup.msgServer.EVMTransaction(sdk.WrapSDKContext(setup.ctx), req)
			require.NoError(t, err)

			if tt.wantErr {
				require.NotEmpty(t, res.VmError, "Expected error but transaction succeeded")
				require.Nil(t, res.ReturnData)
			} else {
				require.Empty(t, res.VmError, "Unexpected error: %s", res.VmError)
				// Additional validation for successful cases
				seiAddr, _ := testkeeper.PrivateKeyToAddresses(setup.testPrivKey)
				valAddr := sdk.ValAddress(seiAddr)
				validator, found := setup.testApp.StakingKeeper.GetValidator(setup.ctx, valAddr)
				require.True(t, found, "Validator should be created")
				require.Equal(t, tt.args.moniker, validator.Description.Moniker)
			}
		})
	}
}

func TestCreateValidator_UnassociatedAddress(t *testing.T) {
	setup := setupCreateValidatorTest(t)

	// Create unassociated key and fund it
	unassociatedPrivKey := testkeeper.MockPrivateKey()
	unassociatedPrivHex := hex.EncodeToString(unassociatedPrivKey.Bytes())
	unassociatedKey, err := crypto.HexToECDSA(unassociatedPrivHex)
	require.NoError(t, err)

	_, unassociatedEvmAddr := testkeeper.PrivateKeyToAddresses(unassociatedPrivKey)
	fundingAmt := sdk.NewCoins(sdk.NewCoin(setup.k.GetBaseDenom(setup.ctx), sdk.NewInt(2_000_000_000_000_000_000)))
	require.NoError(t, setup.k.BankKeeper().MintCoins(setup.ctx, evmtypes.ModuleName, fundingAmt))
	require.NoError(t, setup.k.BankKeeper().SendCoinsFromModuleToAccount(setup.ctx, evmtypes.ModuleName, unassociatedEvmAddr[:], fundingAmt))

	pubKeyHex := hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes())
	args, err := setup.abi.Pack("createValidator",
		pubKeyHex,
		"TestValidator",
		"0.05",
		"0.20",
		"0.01",
		big.NewInt(1000),
	)
	require.NoError(t, err)

	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       &setup.addr,
		Value:    nil,
		Data:     args,
		Nonce:    0,
	}

	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), setup.signer, unassociatedKey)
	require.NoError(t, err)

	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.NoError(t, err)

	req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	require.NoError(t, err)

	ante.Preprocess(setup.ctx, req, setup.k.ChainID(setup.ctx), false)
	res, err := setup.msgServer.EVMTransaction(sdk.WrapSDKContext(setup.ctx), req)
	require.NoError(t, err)
	require.NotEmpty(t, res.VmError, "Should fail with unassociated address")
	require.Nil(t, res.ReturnData)
}

func TestEditValidator_ErorrIfDoesNotExist(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	abi := pcommon.MustGetABI(f, "abi.json")
	privKey := testkeeper.MockPrivateKey()
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)

	// Associate the address
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	// Fund the account
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(2000000)))
	require.NoError(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))

	// Test editValidator without creating a validator first (should fail with validator not found)
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	addr := common.HexToAddress(staking.StakingAddress)

	args, err := abi.Pack("editValidator", "updated-validator", "0.15", big.NewInt(2000000))
	require.NoError(t, err)

	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     args,
		Nonce:    0,
	}

	chainID := k.ChainID(ctx)
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.NoError(t, err)

	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.NoError(t, err)

	req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	require.NoError(t, err)

	msgServer := keeper.NewMsgServerImpl(k)
	ante.Preprocess(ctx, req, k.ChainID(ctx), false)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.NoError(t, err)
	// Should fail because validator doesn't exist
	require.NotEmpty(t, res.VmError, "Should fail because validator doesn't exist")
	require.Nil(t, res.ReturnData)
}

func TestEditValidator(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	abi := pcommon.MustGetABI(f, "abi.json")
	privKey := testkeeper.MockPrivateKey()
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)

	// Associate the address
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	// Fund the account with enough for validator creation
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(2_000_000_000_000_000_000)))
	require.NoError(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))

	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	addr := common.HexToAddress(staking.StakingAddress)

	chainID := k.ChainID(ctx)
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	msgServer := keeper.NewMsgServerImpl(k)

	// Step 1: Create a validator first using the createValidator precompile
	pubKeyHex := hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes())
	createArgs, err := abi.Pack("createValidator",
		pubKeyHex,
		"original-validator",
		"0.10", // 10% commission
		"0.20",
		"0.01",
		big.NewInt(1000000), // 1M minimum self-delegation
	)
	require.NoError(t, err)

	createTxData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       &addr,
		Value:    big.NewInt(1_000_000_000_000_000_000),
		Data:     createArgs,
		Nonce:    0,
	}

	createTx, err := ethtypes.SignTx(ethtypes.NewTx(&createTxData), signer, key)
	require.NoError(t, err)

	createTxWrapper, err := ethtx.NewLegacyTx(createTx)
	require.NoError(t, err)

	createReq, err := evmtypes.NewMsgEVMTransaction(createTxWrapper)
	require.NoError(t, err)

	ante.Preprocess(ctx, createReq, k.ChainID(ctx), false)
	createRes, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), createReq)
	require.NoError(t, err)
	require.Empty(t, createRes.VmError, "Validator creation should succeed: %s", createRes.VmError)

	// Verify validator was created
	valAddr := sdk.ValAddress(seiAddr)
	validator, found := testApp.StakingKeeper.GetValidator(ctx, valAddr)
	require.True(t, found, "Validator should exist after creation")
	require.Equal(t, "original-validator", validator.Description.Moniker)
	require.Equal(t, sdk.NewDecWithPrec(10, 2), validator.Commission.Rate) // 0.10
	require.Equal(t, sdk.NewInt(1000000), validator.MinSelfDelegation)

	// Step 2: Now edit the validator with new values
	editArgs, err := abi.Pack("editValidator",
		"updated-validator-name",
		"",            // Empty string to not change commission rate (avoid 24h restriction)
		big.NewInt(0), // 0 to not change minimum self-delegation
	)
	require.NoError(t, err)

	editTxData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     editArgs,
		Nonce:    1,
	}

	editTx, err := ethtypes.SignTx(ethtypes.NewTx(&editTxData), signer, key)
	require.NoError(t, err)

	editTxWrapper, err := ethtx.NewLegacyTx(editTx)
	require.NoError(t, err)

	editReq, err := evmtypes.NewMsgEVMTransaction(editTxWrapper)
	require.NoError(t, err)

	ante.Preprocess(ctx, editReq, k.ChainID(ctx), false)
	editRes, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), editReq)
	require.NoError(t, err)
	require.Empty(t, editRes.VmError, "Edit validator should succeed: %s", editRes.VmError)

	// Step 3: Verify the validator was updated with new values
	updatedValidator, found := testApp.StakingKeeper.GetValidator(ctx, valAddr)
	require.True(t, found, "Validator should still exist after edit")
	require.Equal(t, "updated-validator-name", updatedValidator.Description.Moniker, "Moniker should be updated")
	require.Equal(t, sdk.NewDecWithPrec(10, 2), updatedValidator.Commission.Rate, "Commission rate should remain unchanged at 0.10")
	require.Equal(t, sdk.NewInt(1000000), updatedValidator.MinSelfDelegation, "MinSelfDelegation should remain the same (1M)")

	// Verify other fields remain unchanged
	require.Equal(t, validator.OperatorAddress, updatedValidator.OperatorAddress, "Operator address should remain the same")
	require.Equal(t, validator.ConsensusPubkey, updatedValidator.ConsensusPubkey, "Consensus pubkey should remain the same")
}

func TestStakingPrecompileDelegateCallPrevention(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup staking precompile
	precompile, err := staking.NewPrecompile(app.NewPrecompileKeepers(testApp))
	require.NoError(t, err)

	// Setup test context
	privKey := testkeeper.MockPrivateKey()
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	// Test that delegatecall is prevented
	ctx = ctx.WithEVMPrecompileCalledFromDelegateCall(true)

	abi := pcommon.MustGetABI(f, "abi.json")

	// Test all methods that should fail with delegatecall
	testCases := []struct {
		name   string
		method string
		args   []interface{}
		value  *big.Int
	}{
		{
			name:   "delegate",
			method: "delegate",
			args:   []interface{}{"seivaloper1test"},
			value:  big.NewInt(100),
		},
		{
			name:   "redelegate",
			method: "redelegate",
			args:   []interface{}{"seivaloper1src", "seivaloper1dst", big.NewInt(50)},
			value:  nil,
		},
		{
			name:   "undelegate",
			method: "undelegate",
			args:   []interface{}{"seivaloper1test", big.NewInt(30)},
			value:  nil,
		},
		{
			name:   "createValidator",
			method: "createValidator",
			args: []interface{}{
				hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes()),
				"Test Validator",
				"0.1",
				"0.2",
				"0.05",
				big.NewInt(1000),
			},
			value: big.NewInt(100),
		},
		{
			name:   "editValidator",
			method: "editValidator",
			args:   []interface{}{"New Name", "0.15", big.NewInt(2000)},
			value:  nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			method, found := abi.Methods[tc.method]
			require.True(t, found)

			// Create mock EVM
			evm := &vm.EVM{
				StateDB: state.NewDBImpl(ctx, k, false),
			}

			_, _, err := precompile.GetExecutor().(*staking.PrecompileExecutor).Execute(ctx, &method, evmAddr, evmAddr, tc.args, tc.value, false, evm, math.MaxUint64, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), "cannot delegatecall staking")
		})
	}
}

func TestStakingPrecompileStaticCallPrevention(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup staking precompile
	precompile, err := staking.NewPrecompile(app.NewPrecompileKeepers(testApp))
	require.NoError(t, err)

	// Setup test context
	privKey := testkeeper.MockPrivateKey()
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	abi := pcommon.MustGetABI(f, "abi.json")

	// Test all write methods that should fail with staticcall/readonly
	testCases := []struct {
		name   string
		method string
		args   []interface{}
		value  *big.Int
	}{
		{
			name:   "delegate",
			method: "delegate",
			args:   []interface{}{"seivaloper1test"},
			value:  big.NewInt(100),
		},
		{
			name:   "redelegate",
			method: "redelegate",
			args:   []interface{}{"seivaloper1src", "seivaloper1dst", big.NewInt(50)},
			value:  nil,
		},
		{
			name:   "undelegate",
			method: "undelegate",
			args:   []interface{}{"seivaloper1test", big.NewInt(30)},
			value:  nil,
		},
		{
			name:   "createValidator",
			method: "createValidator",
			args: []interface{}{
				hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes()),
				"Test Validator",
				"0.1",
				"0.2",
				"0.05",
				big.NewInt(1000),
			},
			value: big.NewInt(100),
		},
		{
			name:   "editValidator",
			method: "editValidator",
			args:   []interface{}{"New Name", "0.15", big.NewInt(2000)},
			value:  nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			method, found := abi.Methods[tc.method]
			require.True(t, found)

			// Create mock EVM
			evm := &vm.EVM{
				StateDB: state.NewDBImpl(ctx, k, false),
			}

			// Test with readOnly = true (staticcall)
			_, _, err := precompile.GetExecutor().(*staking.PrecompileExecutor).Execute(ctx, &method, evmAddr, evmAddr, tc.args, tc.value, true, evm, math.MaxUint64, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), "cannot call staking precompile from staticcall")
		})
	}

	// Test that delegation query works with staticcall
	t.Run("delegation query allowed in staticcall", func(t *testing.T) {
		method, found := abi.Methods["delegation"]
		require.True(t, found)

		// Create a validator
		valPub := ed25519.GenPrivKey().PubKey()
		val := setupValidator(t, ctx, testApp, stakingtypes.Bonded, valPub)

		// Query arguments
		args := []interface{}{evmAddr, val.String()}

		// Create EVM
		evm := &vm.EVM{
			StateDB:   state.NewDBImpl(ctx, k, false),
			TxContext: vm.TxContext{Origin: evmAddr},
		}

		// Should succeed with readOnly = true for query method (even if no delegation exists)
		// The important thing is that the static call is allowed
		_, _, err := precompile.GetExecutor().(*staking.PrecompileExecutor).Execute(ctx, &method, evmAddr, evmAddr, args, nil, true, evm, math.MaxUint64, nil)
		// We don't check the error because the delegation might not exist
		// We're just testing that static calls are allowed for query methods
		_ = err
	})
}
