package confidentialtransfers_test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

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
)

func TestPrecompileTransfer_Execute(t *testing.T) {
	transferPrecompile, _ := confidentialtransfers.NewPrecompile(nil, nil, nil)
	transferMethod, _ := transferPrecompile.ABI.MethodById(transferPrecompile.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferID)
	expectedTrueResponse, _ := transferMethod.Outputs.Pack(true)
	var senderAddr, receiverAddr, otherSenderAddr sdk.AccAddress
	var senderEVMAddr, receiverEVMAddr, otherSenderEVMAddr common.Address
	var receiverPubKey *curves.Point

	notAssociatedUserPrivateKey := testkeeper.MockPrivateKey()
	_, notAssociatedEVMAddr := testkeeper.PrivateKeyToAddresses(notAssociatedUserPrivateKey)

	type inputs struct {
		receiverAddr       string
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
		caller             *common.Address
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
			name:             "precompile should return true if input is valid",
			args:             args{caller: &senderEVMAddr},
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0xec0b6,
			wantErr:          false,
		},
		{
			name:       "precompile should return error if caller did not create call data",
			args:       args{caller: &otherSenderEVMAddr},
			wantErr:    true,
			wantErrMsg: "failed to verify remaining balance commitment: invalid request",
		},
		{
			name:       "precompile should return error if Sei address is not associated with an EVM address",
			args:       args{caller: &notAssociatedEVMAddr},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is not associated", notAssociatedEVMAddr),
		},
		{
			name: "precompile should return true if input is valid and receiver is Sei address",
			args: args{
				caller: &senderEVMAddr,
				setUp: func(in inputs) inputs {
					in.receiverAddr = receiverAddr.String()
					return in
				}},
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0xec519,
			wantErr:          false,
		},
		{
			name: "precompile should return error if receiver address is invalid",
			args: args{
				caller: &senderEVMAddr,
				setUp: func(in inputs) inputs {
					in.receiverAddr = ""
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid to addr",
		},
		{
			name: "precompile should return error if denom is invalid",
			args: args{
				caller: &senderEVMAddr,
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
				caller: &senderEVMAddr,
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
				caller: &senderEVMAddr,
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
				caller: &senderEVMAddr,
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
				caller: &senderEVMAddr,
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
				caller: &senderEVMAddr,
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
				caller: &senderEVMAddr,
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
			args:       args{caller: &senderEVMAddr, isReadOnly: true},
			wantErr:    true,
			wantErrMsg: "cannot call ct precompile from staticcall",
		},
		{
			name:       "precompile should return error if value is not nil",
			args:       args{caller: &senderEVMAddr, value: big.NewInt(100)},
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
	}
	for _, tt := range tests {
		testDenom := "usei"
		testApp := testkeeper.EVMTestApp
		ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
		k := &testApp.EvmKeeper

		// Setup sender addresses and environment
		senderPrivateKey := testkeeper.MockPrivateKey()
		senderAddr, senderEVMAddr = testkeeper.PrivateKeyToAddresses(senderPrivateKey)
		otherSenderAddr, otherSenderEVMAddr, _, _ = setUpCtAccount(k, ctx, testDenom)
		k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
		k.SetAddressMapping(ctx, otherSenderAddr, otherSenderEVMAddr)

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

		var account *cttypes.Account
		receiverAddr, receiverEVMAddr, account, err = setUpCtAccount(k, ctx, testDenom)
		require.NoError(t, err)
		receiverPubKey = &account.PublicKey
		p, err := confidentialtransfers.NewPrecompile(k.CtKeeper(), ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
		require.Nil(t, err)
		statedb := state.NewDBImpl(ctx, k, true)
		evm := vm.EVM{
			StateDB:   statedb,
			TxContext: vm.TxContext{Origin: senderEVMAddr},
		}

		transfer, err := p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferID)
		require.Nil(t, err)

		tr, _ := cttypes.NewTransfer(
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

		t.Run(tt.name, func(t *testing.T) {
			in := inputs{
				receiverAddr:       receiverEVMAddr.String(),
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
				in.receiverAddr,
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
				*tt.args.caller,
				senderEVMAddr,
				append(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferID, inputArgs...),
				4000000,
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
	var auditorOneAddr, auditorTwoAddr sdk.AccAddress
	transferPrecompile, _ := confidentialtransfers.NewPrecompile(nil, nil, nil)
	transferMethod, _ := transferPrecompile.ABI.MethodById(transferPrecompile.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferID)
	expectedTrueResponse, _ := transferMethod.Outputs.Pack(true)

	type inputs struct {
		receiverAddr       string
		Denom              string
		fromAmountLo       []byte
		fromAmountHi       []byte
		toAmountLo         []byte
		toAmountHi         []byte
		remainingBalance   []byte
		DecryptableBalance string
		proofs             []byte
		auditors           []cttypes.CtAuditor
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
			name: "precompile should return true of input is valid and auditor is Sei address",
			args: args{
				setUp: func(in inputs) inputs {
					in.auditors[0].AuditorAddress = auditorOneAddr.String()
					in.auditors[1].AuditorAddress = auditorTwoAddr.String()
					return in
				}},
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0xea9c6,
			wantErr:          false,
		},
		{
			name: "precompile should return error if auditor array is empty",
			args: args{
				setUp: func(in inputs) inputs {
					in.auditors = []cttypes.CtAuditor{}
					return in
				}},
			wantErr:    true,
			wantErrMsg: "auditors array cannot be empty",
		},
		{
			name: "precompile should return error if auditor address is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.auditors[0].AuditorAddress = ""
					return in
				}},
			wantErr:    true,
			wantErrMsg: "invalid address : empty address string is not allowed",
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

		var receiverAccount *cttypes.Account
		receiverAddr, receiverEVMAddr, receiverAccount, err := setUpCtAccount(k, ctx, testDenom)
		require.NoError(t, err)

		p, err := confidentialtransfers.NewPrecompile(k.CtKeeper(), ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
		require.Nil(t, err)
		statedb := state.NewDBImpl(ctx, k, true)
		evm := vm.EVM{
			StateDB:   statedb,
			TxContext: vm.TxContext{Origin: senderEVMAddr},
		}
		var auditorOenPubKey, auditorTwoPubKey *curves.Point
		var auditorOneAccount, auditorTwoAccount *cttypes.Account
		auditorOneAddr, _, auditorOneAccount, err = setUpCtAccount(k, ctx, testDenom)
		require.NoError(t, err)
		auditorTwoAddr, _, auditorTwoAccount, err = setUpCtAccount(k, ctx, testDenom)
		require.NoError(t, err)
		auditorOenPubKey = &auditorOneAccount.PublicKey
		auditorTwoPubKey = &auditorTwoAccount.PublicKey

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
			&receiverAccount.PublicKey,
			auditorsInput)

		trProto := cttypes.NewMsgTransferProto(tr)
		fromAmountLo, _ := trProto.FromAmountLo.Marshal()
		fromAmountHi, _ := trProto.FromAmountHi.Marshal()
		toAmountLo, _ := trProto.ToAmountLo.Marshal()
		toAmountHi, _ := trProto.ToAmountHi.Marshal()
		remainingBalance, _ := trProto.RemainingBalance.Marshal()
		proofs, _ := trProto.Proofs.Marshal()
		auditorsProto := trProto.Auditors

		t.Run(tt.name, func(t *testing.T) {
			var auditors []cttypes.CtAuditor

			for _, auditorProto := range auditorsProto {
				encryptedTransferAmountLo, _ := auditorProto.EncryptedTransferAmountLo.Marshal()
				encryptedTransferAmountHi, _ := auditorProto.EncryptedTransferAmountHi.Marshal()
				transferAmountLoValidityProof, _ := auditorProto.TransferAmountLoValidityProof.Marshal()
				transferAmountHiValidityProof, _ := auditorProto.TransferAmountHiValidityProof.Marshal()
				transferAmountLoEqualityProof, _ := auditorProto.TransferAmountLoEqualityProof.Marshal()
				transferAmountHiEqualityProof, _ := auditorProto.TransferAmountHiEqualityProof.Marshal()
				evmAddress, _ := k.GetEVMAddress(ctx, sdk.MustAccAddressFromBech32(auditorProto.AuditorAddress))
				auditor := cttypes.CtAuditor{
					AuditorAddress:                evmAddress.String(),
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
				receiverAddr:       receiverEVMAddr.String(),
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
				in.receiverAddr,
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
				4000000,
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

func setUpCtAccount(k *evmkeeper.Keeper, ctx sdk.Context, testDenom string) (sdk.AccAddress, common.Address, *cttypes.Account, error) {
	privateKey := testkeeper.MockPrivateKey()
	addr, EVMAddr := testkeeper.PrivateKeyToAddresses(privateKey)
	k.SetAddressMapping(ctx, addr, EVMAddr)
	privHex := hex.EncodeToString(privateKey.Bytes())
	receiverKey, _ := crypto.HexToECDSA(privHex)
	initializeAccount, err := cttypes.NewInitializeAccount(addr.String(), testDenom, *receiverKey)
	if err != nil {
		return nil, common.Address{}, nil, err
	}
	account := &cttypes.Account{
		PublicKey:                   *initializeAccount.Pubkey,
		PendingBalanceLo:            initializeAccount.PendingBalanceLo,
		PendingBalanceHi:            initializeAccount.PendingBalanceHi,
		PendingBalanceCreditCounter: 0,
		AvailableBalance:            initializeAccount.AvailableBalance,
		DecryptableAvailableBalance: initializeAccount.DecryptableBalance,
	}
	err = k.CtKeeper().SetAccount(ctx, addr.String(), testDenom, *account)
	if err != nil {
		return nil, common.Address{}, nil, err
	}
	return addr, EVMAddr, account, nil
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
	notAssociatedUserPrivateKey := testkeeper.MockPrivateKey()
	notAssociatedUserAddr, notAssociatedUserEVMAddr := testkeeper.PrivateKeyToAddresses(notAssociatedUserPrivateKey)
	otherUserPrivateKey := testkeeper.MockPrivateKey()
	otherUserAddr, otherUserEVMAddr := testkeeper.PrivateKeyToAddresses(otherUserPrivateKey)
	k.SetAddressMapping(ctx, userAddr, userEVMAddr)
	k.SetAddressMapping(ctx, otherUserAddr, otherUserEVMAddr)

	privHex := hex.EncodeToString(userPrivateKey.Bytes())
	userKey, _ := crypto.HexToECDSA(privHex)

	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: userEVMAddr},
	}

	p, err := confidentialtransfers.NewPrecompile(k.CtKeeper(), ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
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
			name:             "precompile should return true if input is valid",
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0x1e438a,
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
			name: "precompile should return error if Sei address is not associated with an EVM address",
			args: args{
				setUp: func(in inputs) inputs {
					in.UserAddress = notAssociatedUserAddr.String()
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is not associated", notAssociatedUserAddr.String()),
		},
		{
			name: "precompile should return error if EVM address is not associated with a Sei address",
			args: args{
				setUp: func(in inputs) inputs {
					in.UserAddress = notAssociatedUserEVMAddr.String()
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is not associated", notAssociatedUserEVMAddr.String()),
		},
		{
			name: "precompile should return error if caller is not the same as the user",
			args: args{
				setUp: func(in inputs) inputs {
					in.UserAddress = otherUserAddr.String()
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "caller is not the same as the user address",
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
				common.Address{},
				append(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).InitializeAccountID, inputArgs...),
				4000000,
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

func TestPrecompileDeposit_Execute(t *testing.T) {
	testDenom := "usei"
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	userAddr, userEVMAddr, _, _ := setUpCtAccount(k, ctx, testDenom)

	k.SetAddressMapping(ctx, userAddr, userEVMAddr)

	notAssociatedUserPrivateKey := testkeeper.MockPrivateKey()
	_, notAssociatedEVMAddr := testkeeper.PrivateKeyToAddresses(notAssociatedUserPrivateKey)

	otherUserPrivateKey := testkeeper.MockPrivateKey()
	otherUserAddr, otherUserEVMAddr := testkeeper.PrivateKeyToAddresses(otherUserPrivateKey)
	k.SetAddressMapping(ctx, otherUserAddr, otherUserEVMAddr)

	err := k.BankKeeper().MintCoins(
		ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(
		ctx, types.ModuleName, userAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
	require.Nil(t, err)

	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: userEVMAddr},
	}

	require.NoError(t, err)

	p, err := confidentialtransfers.NewPrecompile(k.CtKeeper(), ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
	require.Nil(t, err)
	DepositMethod, _ := p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).DepositID)
	expectedTrueResponse, _ := DepositMethod.Outputs.Pack(true)

	type inputs struct {
		Denom  string
		Amount uint64
	}

	type args struct {
		caller             common.Address
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
			name: "precompile should return true if input is valid",
			args: args{
				caller: userEVMAddr,
			},
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0x1e0fb5,
			wantErr:          false,
		},
		{
			name: "precompile should return error if Sei address is not associated with an EVM address",
			args: args{
				caller: notAssociatedEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is not associated", notAssociatedEVMAddr.String()),
		},
		{
			name: "precompile should return error if denom is invalid",
			args: args{
				caller: userEVMAddr,
				setUp: func(in inputs) inputs {
					in.Denom = ""
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid denom",
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
				Denom:  testDenom,
				Amount: 100,
			}
			if tt.args.setUp != nil {
				in = tt.args.setUp(in)
			}

			inputArgs, err := DepositMethod.Inputs.Pack(
				in.Denom,
				in.Amount)

			require.Nil(t, err)

			resp, remainingGas, err := p.RunAndCalculateGas(
				&evm,
				tt.args.caller,
				common.Address{},
				append(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).DepositID, inputArgs...),
				4000000,
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

func TestPrecompileApplyPendingBalance_Execute(t *testing.T) {
	testDenom := "usei"
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	p, err := confidentialtransfers.NewPrecompile(k.CtKeeper(), ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
	require.Nil(t, err)
	ApplyPendingBalanceMethod, _ := p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).ApplyPendingBalanceID)
	expectedTrueResponse, _ := ApplyPendingBalanceMethod.Outputs.Pack(true)
	var senderAddr, otherAddr sdk.AccAddress
	var senderEVMAddr, otherEVMAddr common.Address
	notAssociatedUserPrivateKey := testkeeper.MockPrivateKey()
	_, notAssociatedEVMAddr := testkeeper.PrivateKeyToAddresses(notAssociatedUserPrivateKey)

	type inputs struct {
		Denom                 string
		pendingBalanceCounter uint32
		availableBalance      []byte
		DecryptableBalance    string
	}

	type args struct {
		isReadOnly         bool
		isFromDelegateCall bool
		value              *big.Int
		setUp              func(in inputs) inputs
		caller             *common.Address
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
			name: "precompile should return true if input is valid",
			args: args{
				caller: &senderEVMAddr,
			},
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0x1e43df,
			wantErr:          false,
		},
		// Technically this is possible, although both accounts would have to have the same pendingBalanceCreditCounter and AvailableBalance, which is highly improbable.
		{
			name: "precompile should return error if calldata was not created by the sender",
			args: args{
				caller: &otherEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "available balance mismatch: invalid request",
		},
		{
			name:       "precompile should return error if Sei address is not associated with an EVM address",
			args:       args{caller: &notAssociatedEVMAddr},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is not associated", notAssociatedEVMAddr),
		},
		{
			name: "precompile should return error if denom is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.Denom = ""
					return in
				},
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "invalid denom",
		},
		{
			name: "precompile should return error if availableBalance is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.availableBalance = []byte("invalid")
					return in
				},
				caller: &senderEVMAddr,
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
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "invalid decryptable balance",
		},
		{
			name: "precompile should return error if called from static call",
			args: args{
				isReadOnly: true,
				caller:     &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "cannot call ct precompile from staticcall",
		},
		{
			name: "precompile should return error if value is not nil",
			args: args{
				value:  big.NewInt(100),
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
	}
	for _, tt := range tests {
		// Setup sender addresses and environment
		senderPrivateKey := testkeeper.MockPrivateKey()
		senderAddr, senderEVMAddr = testkeeper.PrivateKeyToAddresses(senderPrivateKey)
		otherAddr, otherEVMAddr, _, _ = setUpCtAccount(k, ctx, testDenom)
		k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
		k.SetAddressMapping(ctx, otherAddr, otherEVMAddr)

		err := k.BankKeeper().MintCoins(
			ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
		require.Nil(t, err)
		err = k.BankKeeper().SendCoinsFromModuleToAccount(
			ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
		require.Nil(t, err)

		// setup sender ct account
		ctKeeper := k.CtKeeper()
		privHex := hex.EncodeToString(senderPrivateKey.Bytes())
		senderKey, _ := crypto.HexToECDSA(privHex)
		initSenderAccount, err := cttypes.NewInitializeAccount(senderAddr.String(), testDenom, *senderKey)
		require.NoError(t, err)
		teg := elgamal.NewTwistedElgamal()
		senderAvailableBalance, err := teg.AddScalar(initSenderAccount.AvailableBalance, big.NewInt(1000))
		senderPendingBalanceLo, err := teg.AddScalar(initSenderAccount.PendingBalanceLo, big.NewInt(2000))
		senderPendingBalanceHi, err := teg.AddScalar(initSenderAccount.PendingBalanceHi, big.NewInt(3000))
		senderAesKey, err := utils.GetAESKey(*senderKey, testDenom)
		senderDecryptableBalance, err := encryption.EncryptAESGCM(big.NewInt(1000), senderAesKey)
		require.NoError(t, err)
		senderAccount := cttypes.Account{
			PublicKey:                   *initSenderAccount.Pubkey,
			PendingBalanceLo:            senderPendingBalanceLo,
			PendingBalanceHi:            senderPendingBalanceHi,
			PendingBalanceCreditCounter: 3,
			AvailableBalance:            senderAvailableBalance,
			DecryptableAvailableBalance: senderDecryptableBalance,
		}
		err = ctKeeper.SetAccount(ctx, senderAddr.String(), testDenom, senderAccount)
		require.NoError(t, err)

		otherAccount, _ := k.CtKeeper().GetAccount(ctx, otherAddr.String(), testDenom)
		otherAccount.PendingBalanceLo, _ = teg.AddScalar(otherAccount.PendingBalanceLo, big.NewInt(1000))
		otherAccount.PendingBalanceCreditCounter = 3
		err = ctKeeper.SetAccount(ctx, otherAddr.String(), testDenom, otherAccount)

		p, err := confidentialtransfers.NewPrecompile(k.CtKeeper(), ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
		require.Nil(t, err)
		statedb := state.NewDBImpl(ctx, k, true)
		evm := vm.EVM{
			StateDB:   statedb,
			TxContext: vm.TxContext{Origin: senderEVMAddr},
		}

		applyPendingBalance, err := p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).ApplyPendingBalanceID)
		require.Nil(t, err)

		applyBalance, _ := cttypes.NewApplyPendingBalance(
			*senderKey,
			senderAddr.String(),
			testDenom,
			senderAccount.DecryptableAvailableBalance,
			senderAccount.PendingBalanceCreditCounter,
			senderAccount.AvailableBalance,
			senderAccount.PendingBalanceLo,
			senderAccount.PendingBalanceHi)

		apbProto := cttypes.NewMsgApplyPendingBalanceProto(applyBalance)
		availableBalance, _ := apbProto.CurrentAvailableBalance.Marshal()

		t.Run(tt.name, func(t *testing.T) {
			in := inputs{
				Denom:                 testDenom,
				pendingBalanceCounter: uint32(senderAccount.PendingBalanceCreditCounter),
				availableBalance:      availableBalance,
				DecryptableBalance:    senderAccount.DecryptableAvailableBalance,
			}
			if tt.args.setUp != nil {
				in = tt.args.setUp(in)
			}
			inputArgs, err := applyPendingBalance.Inputs.Pack(
				in.Denom,
				in.DecryptableBalance,
				in.pendingBalanceCounter,
				in.availableBalance)
			require.Nil(t, err)

			resp, remainingGas, err := p.RunAndCalculateGas(
				&evm,
				*tt.args.caller,
				senderEVMAddr,
				append(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).ApplyPendingBalanceID, inputArgs...),
				4000000,
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

func TestPrecompileWithdraw_Execute(t *testing.T) {
	testDenom := "usei"
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	p, err := confidentialtransfers.NewPrecompile(k.CtKeeper(), ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
	require.Nil(t, err)
	ApplyPendingBalanceMethod, _ := p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).WithdrawID)
	expectedTrueResponse, _ := ApplyPendingBalanceMethod.Outputs.Pack(true)
	var senderAddr, otherAddr sdk.AccAddress
	var senderEVMAddr, otherEVMAddr common.Address
	notAssociatedUserPrivateKey := testkeeper.MockPrivateKey()
	_, notAssociatedEVMAddr := testkeeper.PrivateKeyToAddresses(notAssociatedUserPrivateKey)

	type inputs struct {
		denom                      string
		amount                     *big.Int
		decryptableBalance         string
		remainingBalanceCommitment []byte
		proofs                     []byte
	}

	type args struct {
		isReadOnly         bool
		isFromDelegateCall bool
		value              *big.Int
		setUp              func(in inputs) inputs
		caller             *common.Address
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
			name: "precompile should return true if input is valid",
			args: args{
				caller: &senderEVMAddr,
			},
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0xecd4e,
			wantErr:          false,
		},
		{
			name: "precompile should return error if caller did not create calldata",
			args: args{
				caller: &otherEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "ciphertext commitment equality verification failed: invalid request",
		},
		{
			name:       "precompile should return error if Sei address is not associated with an EVM address",
			args:       args{caller: &notAssociatedEVMAddr},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is not associated", notAssociatedEVMAddr),
		},
		{
			name: "precompile should return error if denom is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.denom = ""
					return in
				},
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "invalid denom",
		},
		{
			name: "precompile should return error if amount is zero",
			args: args{
				setUp: func(in inputs) inputs {
					in.amount = big.NewInt(0)
					return in
				},
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "invalid msg: invalid request",
		},
		{
			name: "precompile should return error if decryptable balance is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.decryptableBalance = ""
					return in
				},
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "invalid decryptable balance",
		},
		{
			name: "precompile should return error if remainingBalanceCommitment is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.remainingBalanceCommitment = []byte("invalid")
					return in
				},
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "invalid remainingBalanceCommitment",
		},
		{
			name: "precompile should return error if proofs is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.proofs = []byte("invalid")
					return in
				},
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if called from static call",
			args: args{
				isReadOnly: true,
				caller:     &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "cannot call ct precompile from staticcall",
		},
		{
			name: "precompile should return error if value is not nil",
			args: args{
				value:  big.NewInt(100),
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
	}
	for _, tt := range tests {
		// Setup sender addresses and environment
		senderPrivateKey := testkeeper.MockPrivateKey()
		senderAddr, senderEVMAddr = testkeeper.PrivateKeyToAddresses(senderPrivateKey)
		otherAddr, otherEVMAddr, _, _ = setUpCtAccount(k, ctx, testDenom)
		k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
		k.SetAddressMapping(ctx, otherAddr, otherEVMAddr)

		err := k.BankKeeper().MintCoins(
			ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(20000000))))
		require.Nil(t, err)
		err = k.BankKeeper().SendCoinsFromModuleToModule(ctx, types.ModuleName, cttypes.ModuleName, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
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
		senderAvailableBalance, err := teg.AddScalar(initSenderAccount.AvailableBalance, big.NewInt(1000))
		senderPendingBalanceLo, err := teg.AddScalar(initSenderAccount.PendingBalanceLo, big.NewInt(2000))
		senderPendingBalanceHi, err := teg.AddScalar(initSenderAccount.PendingBalanceHi, big.NewInt(3000))
		senderAesKey, err := utils.GetAESKey(*senderKey, testDenom)
		senderDecryptableBalance, err := encryption.EncryptAESGCM(big.NewInt(1000), senderAesKey)
		require.NoError(t, err)
		senderAccount := cttypes.Account{
			PublicKey:                   *initSenderAccount.Pubkey,
			PendingBalanceLo:            senderPendingBalanceLo,
			PendingBalanceHi:            senderPendingBalanceHi,
			PendingBalanceCreditCounter: 3,
			AvailableBalance:            senderAvailableBalance,
			DecryptableAvailableBalance: senderDecryptableBalance,
		}
		err = ctKeeper.SetAccount(ctx, senderAddr.String(), testDenom, senderAccount)
		require.NoError(t, err)

		p, err := confidentialtransfers.NewPrecompile(k.CtKeeper(), ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
		require.Nil(t, err)
		statedb := state.NewDBImpl(ctx, k, true)
		evm := vm.EVM{
			StateDB:   statedb,
			TxContext: vm.TxContext{Origin: senderEVMAddr},
		}

		withdrawMethod, err := p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).WithdrawID)
		require.Nil(t, err)

		withdrawAmount := big.NewInt(500)
		withdraw, _ := cttypes.NewWithdraw(
			*senderKey,
			senderAccount.AvailableBalance,
			testDenom,
			senderAddr.String(),
			senderAccount.DecryptableAvailableBalance,
			withdrawAmount)

		wdProto := cttypes.NewMsgWithdrawProto(withdraw)
		remainingBalanceCommitment, _ := wdProto.RemainingBalanceCommitment.Marshal()
		proofs, _ := wdProto.Proofs.Marshal()

		t.Run(tt.name, func(t *testing.T) {
			in := inputs{
				denom:                      testDenom,
				amount:                     withdrawAmount,
				decryptableBalance:         senderAccount.DecryptableAvailableBalance,
				remainingBalanceCommitment: remainingBalanceCommitment,
				proofs:                     proofs,
			}
			if tt.args.setUp != nil {
				in = tt.args.setUp(in)
			}
			inputArgs, err := withdrawMethod.Inputs.Pack(
				in.denom,
				in.amount,
				in.decryptableBalance,
				in.remainingBalanceCommitment,
				in.proofs)
			require.Nil(t, err)

			resp, remainingGas, err := p.RunAndCalculateGas(
				&evm,
				*tt.args.caller,
				senderEVMAddr,
				append(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).WithdrawID, inputArgs...),
				4000000,
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

func TestPrecompileCloseAccount_Execute(t *testing.T) {
	testDenom := "usei"
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	p, err := confidentialtransfers.NewPrecompile(k.CtKeeper(), ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
	require.Nil(t, err)
	CloseAccountMethod, _ := p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).CloseAccountID)
	expectedTrueResponse, _ := CloseAccountMethod.Outputs.Pack(true)
	var senderAddr, otherAddr sdk.AccAddress
	var senderEVMAddr, otherEVMAddr common.Address
	notAssociatedUserPrivateKey := testkeeper.MockPrivateKey()
	_, notAssociatedEVMAddr := testkeeper.PrivateKeyToAddresses(notAssociatedUserPrivateKey)

	type inputs struct {
		denom  string
		proofs []byte
	}

	type args struct {
		isReadOnly         bool
		isFromDelegateCall bool
		value              *big.Int
		setUp              func(in inputs) inputs
		caller             *common.Address
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
			name: "precompile should return true if input is valid",
			args: args{
				caller: &senderEVMAddr,
			},
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0x1e6c5d,
			wantErr:          false,
		},
		{
			name: "precompile should return error if caller did not create calldata",
			args: args{
				caller: &otherEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "pending balance lo must be 0: invalid request",
		},
		{
			name:       "precompile should return error if Sei address is not associated with an EVM address",
			args:       args{caller: &notAssociatedEVMAddr},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is not associated", notAssociatedEVMAddr),
		},
		{
			name: "precompile should return error if denom is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.denom = ""
					return in
				},
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "invalid denom",
		},
		{
			name: "precompile should return error if proofs is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.proofs = []byte("invalid")
					return in
				},
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "unexpected EOF",
		},
		{
			name: "precompile should return error if called from static call",
			args: args{
				isReadOnly: true,
				caller:     &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "cannot call ct precompile from staticcall",
		},
		{
			name: "precompile should return error if value is not nil",
			args: args{
				value:  big.NewInt(100),
				caller: &senderEVMAddr,
			},
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
	}
	for _, tt := range tests {
		// Setup sender addresses and environment
		senderPrivateKey := testkeeper.MockPrivateKey()
		senderAddr, senderEVMAddr = testkeeper.PrivateKeyToAddresses(senderPrivateKey)
		otherAddr, otherEVMAddr, _, _ = setUpCtAccount(k, ctx, testDenom)
		k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
		k.SetAddressMapping(ctx, otherAddr, otherEVMAddr)

		err := k.BankKeeper().MintCoins(
			ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(20000000))))
		require.Nil(t, err)
		err = k.BankKeeper().SendCoinsFromModuleToModule(ctx, types.ModuleName, cttypes.ModuleName, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
		err = k.BankKeeper().SendCoinsFromModuleToAccount(
			ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
		require.Nil(t, err)

		// setup sender and receiver ct accounts
		ctKeeper := k.CtKeeper()
		privHex := hex.EncodeToString(senderPrivateKey.Bytes())
		senderKey, _ := crypto.HexToECDSA(privHex)
		initSenderAccount, err := cttypes.NewInitializeAccount(senderAddr.String(), testDenom, *senderKey)
		require.NoError(t, err)
		senderAccount := cttypes.Account{
			PublicKey:                   *initSenderAccount.Pubkey,
			PendingBalanceLo:            initSenderAccount.PendingBalanceLo,
			PendingBalanceHi:            initSenderAccount.PendingBalanceHi,
			PendingBalanceCreditCounter: 0,
			AvailableBalance:            initSenderAccount.AvailableBalance,
			DecryptableAvailableBalance: initSenderAccount.DecryptableBalance,
		}
		err = ctKeeper.SetAccount(ctx, senderAddr.String(), testDenom, senderAccount)
		require.NoError(t, err)

		p, err := confidentialtransfers.NewPrecompile(k.CtKeeper(), ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
		require.Nil(t, err)
		statedb := state.NewDBImpl(ctx, k, true)
		evm := vm.EVM{
			StateDB:   statedb,
			TxContext: vm.TxContext{Origin: senderEVMAddr},
		}

		closeAccountMethod, err := p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).CloseAccountID)
		require.Nil(t, err)

		closeAccount, _ := cttypes.NewCloseAccount(
			*senderKey,
			senderAddr.String(),
			testDenom,
			senderAccount.PendingBalanceLo,
			senderAccount.PendingBalanceHi,
			senderAccount.AvailableBalance)

		clProto := cttypes.NewMsgCloseAccountProto(closeAccount)
		proofs, _ := clProto.Proofs.Marshal()

		t.Run(tt.name, func(t *testing.T) {
			in := inputs{
				denom:  testDenom,
				proofs: proofs,
			}
			if tt.args.setUp != nil {
				in = tt.args.setUp(in)
			}
			inputArgs, err := closeAccountMethod.Inputs.Pack(
				in.denom,
				in.proofs)
			require.Nil(t, err)

			resp, remainingGas, err := p.RunAndCalculateGas(
				&evm,
				*tt.args.caller,
				senderEVMAddr,
				append(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).CloseAccountID, inputArgs...),
				4000000,
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

func TestPrecompileAccount_Execute(t *testing.T) {
	testDenom := "usei"
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	userAddr, userEVMAddr, account, _ := setUpCtAccount(k, ctx, testDenom)

	k.SetAddressMapping(ctx, userAddr, userEVMAddr)

	notAssociatedUserPrivateKey := testkeeper.MockPrivateKey()
	_, notAssociatedEVMAddr := testkeeper.PrivateKeyToAddresses(notAssociatedUserPrivateKey)

	otherUserPrivateKey := testkeeper.MockPrivateKey()
	otherUserAddr, otherUserEVMAddr := testkeeper.PrivateKeyToAddresses(otherUserPrivateKey)
	k.SetAddressMapping(ctx, otherUserAddr, otherUserEVMAddr)

	err := k.BankKeeper().MintCoins(
		ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(
		ctx, types.ModuleName, userAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdk.NewInt(10000000))))
	require.Nil(t, err)

	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: userEVMAddr},
	}

	require.NoError(t, err)

	p, err := confidentialtransfers.NewPrecompile(k.CtKeeper(), ctkeeper.NewMsgServerImpl(k.CtKeeper()), k)
	require.Nil(t, err)
	AccountMethod, _ := p.ABI.MethodById(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).AccountID)
	accountProto := cttypes.NewCtAccount(account)
	pendingBalanceLo, _ := accountProto.PendingBalanceLo.Marshal()
	pendingBalanceHi, _ := accountProto.PendingBalanceHi.Marshal()
	availableBalance, _ := accountProto.AvailableBalance.Marshal()

	ctAccount := &confidentialtransfers.CtAccount{
		PublicKey:                   accountProto.PublicKey,
		PendingBalanceLo:            pendingBalanceLo,
		PendingBalanceHi:            pendingBalanceHi,
		PendingBalanceCreditCounter: accountProto.PendingBalanceCreditCounter,
		AvailableBalance:            availableBalance,
		DecryptableAvailableBalance: accountProto.DecryptableAvailableBalance,
	}

	expectedResponse, _ := AccountMethod.Outputs.Pack(ctAccount)

	type inputs struct {
		Account string
		Denom   string
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
			name: "precompile should return abi-encoded account if input is valid",
			args: args{
				isReadOnly: true,
			},
			wantRet:          expectedResponse,
			wantRemainingGas: 0x3cfd88,
			wantErr:          false,
		},
		{
			name: "precompile should return abi-encoded account if input is valid and EVM address is used",
			args: args{
				isReadOnly: true,
				setUp: func(in inputs) inputs {
					in.Account = userEVMAddr.String()
					return in
				},
			},
			wantRet:          expectedResponse,
			wantRemainingGas: 0x3cf925,
			wantErr:          false,
		},
		{
			name: "precompile should return abi-encoded account if input is valid and the call is not read-only",
			args: args{
				isReadOnly: false,
			},
			wantRet:          expectedResponse,
			wantRemainingGas: 0x3cfd88,
			wantErr:          false,
		},
		{
			name: "precompile should return error if account not found",
			args: args{
				isReadOnly: true,
				setUp: func(in inputs) inputs {
					in.Account = otherUserAddr.String()
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "account not found",
		},
		{
			name: "precompile should return error if address is empty",
			args: args{
				isReadOnly: true,
				setUp: func(in inputs) inputs {
					in.Account = ""
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid address",
		},
		{
			name: "precompile should return error if address is invalid",
			args: args{
				setUp: func(in inputs) inputs {
					in.Account = "invalid"
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid address invalid: decoding bech32 failed: invalid bech32 string length 7",
		},
		{
			name: "precompile should return error if address is not associated",
			args: args{
				setUp: func(in inputs) inputs {
					in.Account = notAssociatedEVMAddr.String()
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is not associated", notAssociatedEVMAddr.String()),
		},
		{
			name: "precompile should return error if denom is empty",
			args: args{
				isReadOnly: true,
				setUp: func(in inputs) inputs {
					in.Denom = ""
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid denom",
		},
		{
			name: "precompile should return error if denom is invalid",
			args: args{
				isReadOnly: true,
				setUp: func(in inputs) inputs {
					in.Denom = "invalid"
					return in
				},
			},
			wantErr:    true,
			wantErrMsg: "account not found",
		},
		{
			name:       "precompile should return error if called from delegate call",
			args:       args{isFromDelegateCall: true},
			wantErr:    true,
			wantErrMsg: "cannot delegatecall ct",
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
				Account: userAddr.String(),
				Denom:   testDenom,
			}
			if tt.args.setUp != nil {
				in = tt.args.setUp(in)
			}

			inputArgs, err := AccountMethod.Inputs.Pack(in.Account, in.Denom)
			require.Nil(t, err)

			resp, remainingGas, err := p.RunAndCalculateGas(
				&evm,
				common.Address{},
				common.Address{},
				append(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).AccountID, inputArgs...),
				4000000,
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
