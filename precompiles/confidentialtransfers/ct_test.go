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

	err := k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))))
	require.Nil(t, err)

	// setup sender and receiver ct accounts
	ctKeeper := k.CtKeeper()
	privHex := hex.EncodeToString(senderPrivateKey.Bytes())
	senderKey, _ := crypto.HexToECDSA(privHex)
	initSenderAccount, err := cttypes.NewInitializeAccount(senderAddr.String(), "usei", *senderKey)
	require.NoError(t, err)
	teg := elgamal.NewTwistedElgamal()
	newSenderBalance, err := teg.AddScalar(initSenderAccount.AvailableBalance, big.NewInt(1000))
	senderAesKey, err := utils.GetAESKey(*senderKey, "usei")
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
	err = ctKeeper.SetAccount(ctx, senderAddr.String(), "usei", senderAccount)
	require.NoError(t, err)

	privHex = hex.EncodeToString(receiverPrivateKey.Bytes())
	receiverKey, _ := crypto.HexToECDSA(privHex)
	//receiverAesKey, err := utils.GetAESKey(*receiverKey, "usei")
	initReceiverAccount, err := cttypes.NewInitializeAccount(senderAddr.String(), "usei", *receiverKey)
	require.NoError(t, err)
	receiverAccount := cttypes.Account{
		PublicKey:                   *initReceiverAccount.Pubkey,
		PendingBalanceLo:            initReceiverAccount.PendingBalanceLo,
		PendingBalanceHi:            initReceiverAccount.PendingBalanceHi,
		PendingBalanceCreditCounter: 0,
		AvailableBalance:            initReceiverAccount.AvailableBalance,
		DecryptableAvailableBalance: initReceiverAccount.DecryptableBalance,
	}
	err = ctKeeper.SetAccount(ctx, receiverAddr.String(), "usei", receiverAccount)
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
		"usei",
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

	args, err := transfer.Inputs.Pack(
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
	resp, remainingGas, err := p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*confidentialtransfers.PrecompileExecutor).TransferID, args...), 2000000, nil, nil, false, false) // should error because of read only call
	require.NoError(t, err)
	expectedResponse, _ := transfer.Outputs.Pack(true)
	require.Equal(t, expectedResponse, resp)
	require.Equal(t, uint64(0xec0b6), remainingGas)
	//receiverAccount, _ = ctKeeper.GetAccount(ctx, receiverAddr.String(), "usei")
	//balance, _ := encryption.DecryptAESGCM(receiverAccount.DecryptableAvailableBalance, receiverAesKey)
	//require.Equal(t, big.NewInt(100), balance)
	//_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), 100000, big.NewInt(1), nil, false, false) // should error because it's not payable
	//require.NotNil(t, err)
	//_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), 100000, nil, nil, false, false) // should error because address is not whitelisted
	//require.NotNil(t, err)
	//invalidDenomArgs, err := send.Inputs.Pack(senderEVMAddr, evmAddr, "", big.NewInt(25))
	//require.Nil(t, err)
	//_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, invalidDenomArgs...), 100000, nil, nil, false, false) // should error because denom is empty
	//require.NotNil(t, err)
}
