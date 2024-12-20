package confidentialtransfers_test

import (
	"encoding/hex"
	"github.com/coinbase/kryptology/pkg/core/curves"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/precompiles/confidentialtransfers"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	ctkeeper "github.com/sei-protocol/sei-chain/x/confidentialtransfers/keeper"
	cttypes "github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
	"math/big"
	"testing"
)

func TestPrecompileTransfer_Execute(t *testing.T) {
	testDenom := "usei"
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup sender addresses and environment
	senderPrivateKey := testkeeper.MockPrivateKey()
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(senderPrivateKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	// Setup receiver addresses and environment

	err := k.BankKeeper().MintCoins(
		ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(
		ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
	require.Nil(t, err)

	// setup sender and receiver ct accounts
	ctKeeper := k.CtKeeper()
	privHex := hex.EncodeToString(senderPrivateKey.Bytes())
	senderKey, _ := crypto.HexToECDSA(privHex)
	initSenderAccount, err := cttypes.NewInitializeAccount(senderAddr.String(), testDenom, *senderKey)
	require.NoError(t, err)
	teg := elgamal.NewTwistedElgamal()
	newSenderBalance, err := teg.AddScalar(initSenderAccount.AvailableBalance, big.NewInt(1000))
	senderAesKey, err := utils.GetAESKey(*senderKey, testDenom)
	sennderDecryptableBalance, err := encryption.EncryptAESGCM(big.NewInt(1000), senderAesKey)
	require.NoError(t, err)
	senderAccount := cttypes.Account{
		PublicKey:                   *initSenderAccount.Pubkey,
		PendingBalanceLo:            initSenderAccount.PendingBalanceLo,
		PendingBalanceHi:            initSenderAccount.PendingBalanceHi,
		PendingBalanceCreditCounter: 0,
		AvailableBalance:            newSenderBalance,
		DecryptableAvailableBalance: sennderDecryptableBalance,
	}
	err = ctKeeper.SetAccount(ctx, senderAddr.String(), testDenom, senderAccount)
	require.NoError(t, err)

	receiverAddr, receiverEVMAddr, receiverPubKey, err := setUpCtAccount(k, ctx, testDenom)
	require.NoError(t, err)
	p, err := confidentialtransfers.NewPrecompile(ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	transfer, err := p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferID)
	require.Nil(t, err)

	tr, err := cttypes.NewTransfer(
		senderKey,
		senderAddr.String(),
		receiverAddr.String(),
		testDenom,
		senderAccount.DecryptableAvailableBalance,
		senderAccount.AvailableBalance,
		100,
		receiverPubKey,
		nil)
	trProto := cttypes.NewMsgTransferProto(tr)
	fromAmountLo, _ := trProto.FromAmountLo.Marshal()
	fromAmountHi, _ := trProto.FromAmountHi.Marshal()
	toAmountLo, _ := trProto.ToAmountLo.Marshal()
	toAmountHi, _ := trProto.ToAmountHi.Marshal()
	remainingBalance, _ := trProto.RemainingBalance.Marshal()
	proofs, _ := trProto.Proofs.Marshal()

	transferPrecompile, _ := confidentialtransfers.NewPrecompile(nil, nil)
	transferMethod, _ := transferPrecompile.ABI.MethodById(transferPrecompile.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferID)
	expectedTrueResponse, _ := transferMethod.Outputs.Pack(true)

	type inputs struct {
		senderEVMAddr      common.Address
		receiverEVMAddr    common.Address
		Denom              string
		fromAmountLo       []byte
		fromAmountHi       []byte
		toAmountLo         []byte
		toAmountHi         []byte
		remainingBalance   []byte
		DecryptableBalance string
		proofs             []byte
	}

	type args struct {
		isReadOnly         bool
		isFromDelegateCall bool
		value              *big.Int
		setUp              func(in inputs) inputs
	}
	tests := []struct {
		name             string
		args             args
		wantRet          []byte
		wantRemainingGas uint64
		wantErr          bool
		wantErrMsg       string
	}{
		{
			name:             "precompile should return true of input is valid",
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0xec0b6,
			wantErr:          false,
		},
		{
			name: "precompile should return error if address is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.senderEVMAddr = common.Address{}
					return in
				}},
			wantErr:    true,
			wantErrMsg: "invalid addr",
		},
		{
			name: "precompile should return error if receiver address is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.receiverEVMAddr = common.Address{}
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid addr",
		},
		{
			name: "precompile should return error if denom is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.Denom = ""
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid denom",
		},
		{
			name: "precompile should return error if fromAmountLo is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.fromAmountLo = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if fromAmountHi is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.fromAmountHi = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if toAmountLo is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.toAmountLo = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if toAmountHi is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.toAmountHi = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if remaining balance is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.remainingBalance = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if decryptable balance is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.DecryptableBalance = ""
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid decryptable balance",
		},
		{
			name:       "precompile should return error if called from static call",
			args:       args{isReadOnly: true},
			wantErr:    true,
			wantErrMsg: "cannot call ct precompile from staticcall",
		},
		{
			name:       "precompile should return error if value is not nil",
			args:       args{value: big.NewInt(100)},
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := inputs{
				senderEVMAddr:      senderEVMAddr,
				receiverEVMAddr:    receiverEVMAddr,
				Denom:              testDenom,
				fromAmountLo:       fromAmountLo,
				fromAmountHi:       fromAmountHi,
				toAmountLo:         toAmountLo,
				toAmountHi:         toAmountHi,
				remainingBalance:   remainingBalance,
				DecryptableBalance: senderAccount.DecryptableAvailableBalance,
				proofs:             proofs,
			}
			if tt.args.setUp != nil {
				in = tt.args.setUp(in)
			}
			inputArgs, err := transfer.Inputs.Pack(
				in.senderEVMAddr,
				in.receiverEVMAddr,
				in.Denom,
				in.fromAmountLo,
				in.fromAmountHi,
				in.toAmountLo,
				in.toAmountHi,
				in.remainingBalance,
				in.DecryptableBalance,
				in.proofs)
			require.Nil(t, err)

			resp, remainingGas, err := p.RunAndCalculateGas(
				&evm,
				senderEVMAddr,
				senderEVMAddr,
				append(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferID, inputArgs...),
				2000000,
				tt.args.value,
				nil,
				tt.args.isReadOnly,
				tt.args.isFromDelegateCall)
			if tt.wantErr {
				require.NotNil(t, err)
				require.Equal(t, tt.wantErrMsg, string(resp))
				return
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantRet, resp)
				require.Equal(t, tt.wantRemainingGas, remainingGas)
			}

		})
	}
}

func TestPrecompileTransferWithAuditor_Execute(t *testing.T) {
	testDenom := "usei"
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup sender addresses and environment
	senderPrivateKey := testkeeper.MockPrivateKey()
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(senderPrivateKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)

	err := k.BankKeeper().MintCoins(
		ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(
		ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
	require.Nil(t, err)

	// setup sender and receiver ct accounts
	ctKeeper := k.CtKeeper()
	privHex := hex.EncodeToString(senderPrivateKey.Bytes())
	senderKey, _ := crypto.HexToECDSA(privHex)
	initSenderAccount, err := cttypes.NewInitializeAccount(senderAddr.String(), testDenom, *senderKey)
	require.NoError(t, err)
	teg := elgamal.NewTwistedElgamal()
	newSenderBalance, err := teg.AddScalar(initSenderAccount.AvailableBalance, big.NewInt(1000))
	senderAesKey, err := utils.GetAESKey(*senderKey, testDenom)
	sennderDecryptableBalance, err := encryption.EncryptAESGCM(big.NewInt(1000), senderAesKey)
	require.NoError(t, err)
	senderAccount := cttypes.Account{
		PublicKey:                   *initSenderAccount.Pubkey,
		PendingBalanceLo:            initSenderAccount.PendingBalanceLo,
		PendingBalanceHi:            initSenderAccount.PendingBalanceHi,
		PendingBalanceCreditCounter: 0,
		AvailableBalance:            newSenderBalance,
		DecryptableAvailableBalance: sennderDecryptableBalance,
	}
	err = ctKeeper.SetAccount(ctx, senderAddr.String(), testDenom, senderAccount)
	require.NoError(t, err)

	receiverAddr, receiverEVMAddr, receiverPubKey, err := setUpCtAccount(k, ctx, testDenom)

	p, err := confidentialtransfers.NewPrecompile(ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	auditorOneAddr, _, auditorOenPubKey, err := setUpCtAccount(k, ctx, testDenom)
	auditorTwoAddr, _, auditorTwoPubKey, err := setUpCtAccount(k, ctx, testDenom)

	transferWithAuditorsMethod, err :=
		p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferWithAuditorsID)
	require.Nil(t, err)

	auditorsInput := []cttypes.AuditorInput{
		{
			Address: auditorOneAddr.String(),
			Pubkey:  auditorOenPubKey,
		},
		{
			Address: auditorTwoAddr.String(),
			Pubkey:  auditorTwoPubKey,
		},
	}

	tr, err := cttypes.NewTransfer(
		senderKey,
		senderAddr.String(),
		receiverAddr.String(),
		testDenom,
		senderAccount.DecryptableAvailableBalance,
		senderAccount.AvailableBalance,
		100,
		receiverPubKey,
		auditorsInput)

	trProto := cttypes.NewMsgTransferProto(tr)
	fromAmountLo, _ := trProto.FromAmountLo.Marshal()
	fromAmountHi, _ := trProto.FromAmountHi.Marshal()
	toAmountLo, _ := trProto.ToAmountLo.Marshal()
	toAmountHi, _ := trProto.ToAmountHi.Marshal()
	remainingBalance, _ := trProto.RemainingBalance.Marshal()
	proofs, _ := trProto.Proofs.Marshal()
	auditorsProto := trProto.Auditors

	transferPrecompile, _ := confidentialtransfers.NewPrecompile(nil, nil)
	transferMethod, _ := transferPrecompile.ABI.MethodById(transferPrecompile.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferID)
	expectedTrueResponse, _ := transferMethod.Outputs.Pack(true)

	type inputs struct {
		senderEVMAddr      common.Address
		receiverEVMAddr    common.Address
		Denom              string
		fromAmountLo       []byte
		fromAmountHi       []byte
		toAmountLo         []byte
		toAmountHi         []byte
		remainingBalance   []byte
		DecryptableBalance string
		proofs             []byte
		auditors           []cttypes.EvmAuditor
	}

	type args struct {
		isReadOnly         bool
		isFromDelegateCall bool
		value              *big.Int
		setUp              func(in inputs) inputs
	}
	tests := []struct {
		name             string
		args             args
		wantRet          []byte
		wantRemainingGas uint64
		wantErr          bool
		wantErrMsg       string
	}{
		{
			name:             "precompile should return true of input is valid",
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0xea100,
			wantErr:          false,
		},
		{
			name: "precompile should return error if auditor array is empty",
			args: args{
				setUp: func(in inputs) inputs {
					in.auditors = []cttypes.EvmAuditor{}
					return in
				}},
			wantErr:    true,
			wantErrMsg: "auditors array cannot be empty",
		},
		{
			name: "precompile should return error if auditor address is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.auditors[0].AuditorAddress = common.Address{}
					return in
				}},
			wantErr:    true,
			wantErrMsg: "invalid addr",
		},
		{
			name: "precompile should return error if auditor EncryptedTransferAmountLo is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.auditors[0].EncryptedTransferAmountLo = []byte("invalid")
					return in
				}},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if auditor EncryptedTransferAmountHi is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.auditors[0].EncryptedTransferAmountHi = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if auditor TransferAmountLoValidityProof is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.auditors[0].TransferAmountLoValidityProof = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if auditor TransferAmountHiValidityProof is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.auditors[0].TransferAmountHiValidityProof = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if auditor TransferAmountLoEqualityProof is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.auditors[0].TransferAmountLoEqualityProof = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if auditor TransferAmountHiEqualityProof is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.auditors[0].TransferAmountHiEqualityProof = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name:       "precompile should return error if called from static call",
			args:       args{isReadOnly: true},
			wantErr:    true,
			wantErrMsg: "cannot call ct precompile from staticcall",
		},
		{
			name:       "precompile should return error if value is not nil",
			args:       args{value: big.NewInt(100)},
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var auditors []cttypes.EvmAuditor

			for _, auditorProto := range auditorsProto {
				encryptedTransferAmountLo, _ := auditorProto.EncryptedTransferAmountLo.Marshal()
				encryptedTransferAmountHi, _ := auditorProto.EncryptedTransferAmountHi.Marshal()
				transferAmountLoValidityProof, _ := auditorProto.TransferAmountLoValidityProof.Marshal()
				transferAmountHiValidityProof, _ := auditorProto.TransferAmountHiValidityProof.Marshal()
				transferAmountLoEqualityProof, _ := auditorProto.TransferAmountLoEqualityProof.Marshal()
				transferAmountHiEqualityProof, _ := auditorProto.TransferAmountHiEqualityProof.Marshal()
				evmAddress, _ := k.GetEVMAddress(ctx, sdk.MustAccAddressFromBech32(auditorProto.AuditorAddress))
				auditor := cttypes.EvmAuditor{
					AuditorAddress:                evmAddress,
					EncryptedTransferAmountLo:     encryptedTransferAmountLo,
					EncryptedTransferAmountHi:     encryptedTransferAmountHi,
					TransferAmountLoValidityProof: transferAmountLoValidityProof,
					TransferAmountHiValidityProof: transferAmountHiValidityProof,
					TransferAmountLoEqualityProof: transferAmountLoEqualityProof,
					TransferAmountHiEqualityProof: transferAmountHiEqualityProof,
				}
				auditors = append(auditors, auditor)
			}

			in := inputs{
				senderEVMAddr:      senderEVMAddr,
				receiverEVMAddr:    receiverEVMAddr,
				Denom:              testDenom,
				fromAmountLo:       fromAmountLo,
				fromAmountHi:       fromAmountHi,
				toAmountLo:         toAmountLo,
				toAmountHi:         toAmountHi,
				remainingBalance:   remainingBalance,
				DecryptableBalance: senderAccount.DecryptableAvailableBalance,
				proofs:             proofs,
				auditors:           auditors,
			}
			if tt.args.setUp != nil {
				in = tt.args.setUp(in)
			}
			inputArgs, err := transferWithAuditorsMethod.Inputs.Pack(
				in.senderEVMAddr,
				in.receiverEVMAddr,
				in.Denom,
				in.fromAmountLo,
				in.fromAmountHi,
				in.toAmountLo,
				in.toAmountHi,
				in.remainingBalance,
				in.DecryptableBalance,
				in.proofs,
				in.auditors)
			require.Nil(t, err)

			resp, remainingGas, err := p.RunAndCalculateGas(
				&evm,
				senderEVMAddr,
				senderEVMAddr,
				append(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferWithAuditorsID, inputArgs...),
				2000000,
				tt.args.value,
				nil,
				tt.args.isReadOnly,
				tt.args.isFromDelegateCall)
			if tt.wantErr {
				require.NotNil(t, err)
				require.Equal(t, tt.wantErrMsg, string(resp))
				return
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantRet, resp)
				require.Equal(t, tt.wantRemainingGas, remainingGas)
			}

		})
	}
}

func setUpCtAccount(k *evmkeeper.Keeper, ctx sdk.Context, testDenom string) (sdk.AccAddress, common.Address, *curves.Point, error) {
	privateKey := testkeeper.MockPrivateKey()
	addr, EVMAddr := testkeeper.PrivateKeyToAddresses(privateKey)
	k.SetAddressMapping(ctx, addr, EVMAddr)
	privHex := hex.EncodeToString(privateKey.Bytes())
	receiverKey, _ := crypto.HexToECDSA(privHex)
	initializeAccount, err := cttypes.NewInitializeAccount(addr.String(), testDenom, *receiverKey)
	if err != nil {
		return nil, common.Address{}, nil, err
	}
	account := cttypes.Account{
		PublicKey:                   *initializeAccount.Pubkey,
		PendingBalanceLo:            initializeAccount.PendingBalanceLo,
		PendingBalanceHi:            initializeAccount.PendingBalanceHi,
		PendingBalanceCreditCounter: 0,
		AvailableBalance:            initializeAccount.AvailableBalance,
		DecryptableAvailableBalance: initializeAccount.DecryptableBalance,
	}
	err = k.CtKeeper().SetAccount(ctx, addr.String(), testDenom, account)
	if err != nil {
		return nil, common.Address{}, nil, err
	}
	return addr, EVMAddr, initializeAccount.Pubkey, nil
}

func TestPrecompileInitializeAccount_Execute(t *testing.T) {
	testDenom := "usei"
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	err := k.BankKeeper().MintCoins(
		ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
	require.Nil(t, err)

	userPrivateKey := testkeeper.MockPrivateKey()
	userAddr, userEVMAddr := testkeeper.PrivateKeyToAddresses(userPrivateKey)
	k.SetAddressMapping(ctx, userAddr, userEVMAddr)

	privHex := hex.EncodeToString(userPrivateKey.Bytes())
	userKey, _ := crypto.HexToECDSA(privHex)

	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: userEVMAddr},
	}

	p, err := confidentialtransfers.NewPrecompile(ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
	require.Nil(t, err)

	initAccount, err := cttypes.NewInitializeAccount(
		userAddr.String(),
		testDenom,
		*userKey)

	iaProto := cttypes.NewMsgInitializeAccountProto(initAccount)
	pendingBalanceLo, _ := iaProto.PendingBalanceLo.Marshal()
	pendingBalanceHi, _ := iaProto.PendingBalanceHi.Marshal()
	availableBalance, _ := iaProto.AvailableBalance.Marshal()
	proofs, _ := iaProto.Proofs.Marshal()

	InitializeAccountMethod, _ := p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).InitializeAccountID)
	expectedTrueResponse, _ := InitializeAccountMethod.Outputs.Pack(true)

	type inputs struct {
		UserAddress        string
		Denom              string
		PublicKey          []byte
		DecryptableBalance string
		PendingBalanceLo   []byte
		PendingBalanceHi   []byte
		AvailableBalance   []byte
		proofs             []byte
	}

	type args struct {
		isReadOnly         bool
		isFromDelegateCall bool
		value              *big.Int
		setUp              func(in inputs) inputs
	}
	tests := []struct {
		name             string
		args             args
		wantRet          []byte
		wantRemainingGas uint64
		wantErr          bool
		wantErrMsg       string
	}{
		{
			name:             "precompile should return true of input is valid",
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0x1e47ed,
			wantErr:          false,
		},
		{
			name: "precompile should return error if address is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.UserAddress = ""
					return in
				}},
			wantErr:    true,
			wantErrMsg: "invalid address : empty address string is not allowed",
		},
		{
			name: "precompile should return error if denom is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.Denom = ""
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid denom",
		},
		{
			name: "precompile should return error if decryptableBalance is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.DecryptableBalance = ""
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid decryptable balance",
		},
		{
			name: "precompile should return error if pendingBalanceLo is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.PendingBalanceLo = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if pendingBalanceHi is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.PendingBalanceHi = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if availableBalance is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.AvailableBalance = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if proofs is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.proofs = []byte("invalid")
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name:       "precompile should return error if called from static call",
			args:       args{isReadOnly: true},
			wantErr:    true,
			wantErrMsg: "cannot call ct precompile from staticcall",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := inputs{
				UserAddress:        userAddr.String(),
				Denom:              testDenom,
				PublicKey:          iaProto.PublicKey,
				DecryptableBalance: iaProto.DecryptableBalance,
				PendingBalanceLo:   pendingBalanceLo,
				PendingBalanceHi:   pendingBalanceHi,
				AvailableBalance:   availableBalance,
				proofs:             proofs,
			}
			if tt.args.setUp != nil {
				in = tt.args.setUp(in)
			}

			inputArgs, err := InitializeAccountMethod.Inputs.Pack(
				in.UserAddress,
				in.Denom,
				in.PublicKey,
				in.DecryptableBalance,
				in.PendingBalanceLo,
				in.PendingBalanceHi,
				in.AvailableBalance,
				in.proofs)

			require.Nil(t, err)

			resp, remainingGas, err := p.RunAndCalculateGas(
				&evm,
				userEVMAddr,
				userEVMAddr,
				append(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).InitializeAccountID, inputArgs...),
				2000000,
				tt.args.value,
				nil,
				tt.args.isReadOnly,
				tt.args.isFromDelegateCall)
			if tt.wantErr {
				require.NotNil(t, err)
				require.Equal(t, tt.wantErrMsg, string(resp))
				return
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantRet, resp)
				require.Equal(t, tt.wantRemainingGas, remainingGas)
			}
		})
	}
}
