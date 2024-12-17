package confidentialtransfers_test

import (
	"encoding/hex"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/precompiles/confidentialtransfers"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	ctkeeper "github.com/sei-protocol/sei-chain/x/confidentialtransfers/keeper"
	cttypes "github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
	"math/big"
	"testing"
)

func TestPrecompileExecutor_Execute(t *testing.T) {
	transferPrecompile, _ := confidentialtransfers.NewPrecompile(nil, nil)
	transferMethod, _ := transferPrecompile.ABI.MethodById(transferPrecompile.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferID)
	expectedTrueResponse, _ := transferMethod.Outputs.Pack(true)
	type fields struct {
	}
	type args struct {
		isReadOnly         bool
		isFromDelegateCall bool
		value              *big.Int
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
			name:             "precompile should return true of input is valid",
			args:             args{},
			wantRet:          expectedTrueResponse,
			wantRemainingGas: 0xec0b6,
			wantErr:          false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDenom := "usei"
			testApp := testkeeper.EVMTestApp
			ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
			k := &testApp.EvmKeeper

			// Setup sender addresses and environment
			senderPrivateKey := testkeeper.MockPrivateKey()
			senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(senderPrivateKey)
			k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
			// Setup receiver addresses and environment
			receiverPrivateKey := testkeeper.MockPrivateKey()
			receiverAddr, receiverEVMAddr := testkeeper.PrivateKeyToAddresses(receiverPrivateKey)
			k.SetAddressMapping(ctx, receiverAddr, receiverEVMAddr)

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

			privHex = hex.EncodeToString(receiverPrivateKey.Bytes())
			receiverKey, _ := crypto.HexToECDSA(privHex)
			initReceiverAccount, err := cttypes.NewInitializeAccount(senderAddr.String(), testDenom, *receiverKey)
			require.NoError(t, err)
			receiverAccount := cttypes.Account{
				PublicKey:                   *initReceiverAccount.Pubkey,
				PendingBalanceLo:            initReceiverAccount.PendingBalanceLo,
				PendingBalanceHi:            initReceiverAccount.PendingBalanceHi,
				PendingBalanceCreditCounter: 0,
				AvailableBalance:            initReceiverAccount.AvailableBalance,
				DecryptableAvailableBalance: initReceiverAccount.DecryptableBalance,
			}
			err = ctKeeper.SetAccount(ctx, receiverAddr.String(), testDenom, receiverAccount)
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
				&receiverAccount.PublicKey,
				nil)
			trProto := cttypes.NewMsgTransferProto(tr)
			fromAmountLo, _ := trProto.FromAmountLo.Marshal()
			fromAmountHi, _ := trProto.FromAmountHi.Marshal()
			toAmountLo, _ := trProto.ToAmountLo.Marshal()
			toAmountHi, _ := trProto.ToAmountHi.Marshal()
			remainingBalance, _ := trProto.RemainingBalance.Marshal()
			proofs, _ := trProto.Proofs.Marshal()

			inputArgs, err := transfer.Inputs.Pack(
				senderEVMAddr,
				receiverEVMAddr,
				trProto.Denom,
				fromAmountLo,
				fromAmountHi,
				toAmountLo,
				toAmountHi,
				remainingBalance,
				trProto.DecryptableBalance,
				proofs)
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
				require.Equal(t, tt.wantErrMsg, err.Error())
				return
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantRet, resp)
				require.Equal(t, tt.wantRemainingGas, remainingGas)
			}

		})
	}
}
