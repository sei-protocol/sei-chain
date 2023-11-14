package bank_test

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	senderAddr, senderEVMAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100))))
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100))))
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	p, err := bank.NewPrecompile(k.BankKeeper(), k)
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k)
	evm := vm.EVM{
		StateDB: statedb,
	}
	sendInput := []byte{}
	for name, m := range p.ABI.Methods {
		if name == "send" {
			sendInput = m.ID
		}
	}
	require.NotEmpty(t, sendInput)
	send, err := p.ABI.MethodById(sendInput)
	require.Nil(t, err)
	args, err := send.Inputs.Pack(senderEVMAddr, evmAddr, "usei", big.NewInt(100))
	require.Nil(t, err)
	res, err := p.Run(&evm, append(sendInput, args...))
	require.Nil(t, err)
	is, err := send.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(is))
	require.True(t, is[0].(bool))
	require.Equal(t, uint64(100), k.BankKeeper().GetBalance(statedb.Ctx(), seiAddr, "usei").Amount.Uint64())
	res, err = p.Run(&evm, append(sendInput, args[:3]...))
	require.NotNil(t, err)
	args, err = send.Inputs.Pack(senderEVMAddr, evmAddr, "", big.NewInt(100))
	res, err = p.Run(&evm, append(sendInput, args...))
	require.NotNil(t, err)

	balanceInput := []byte{}
	for name, m := range p.ABI.Methods {
		if name == "balance" {
			balanceInput = m.ID
		}
	}
	require.NotEmpty(t, balanceInput)
	balance, err := p.ABI.MethodById(balanceInput)
	require.Nil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "usei")
	require.Nil(t, err)
	res, err = p.Run(&evm, append(balanceInput, args...))
	require.Nil(t, err)
	is, err = balance.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(is))
	require.Equal(t, big.NewInt(100), is[0].(*big.Int))
	res, err = p.Run(&evm, append(balanceInput, args[:1]...))
	require.NotNil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "")
	require.Nil(t, err)
	res, err = p.Run(&evm, append(balanceInput, args...))
	require.NotNil(t, err)

	// invalid input
	_, err = p.Run(&evm, []byte{1, 2, 3, 4})
	require.NotNil(t, err)
}

func TestRequiredGas(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper()
	p, err := bank.NewPrecompile(k.BankKeeper(), k)
	require.Nil(t, err)
	sendInput := []byte{}
	for name, m := range p.ABI.Methods {
		if name == "send" {
			sendInput = m.ID
		}
	}
	require.NotEmpty(t, sendInput)
	sendRequiredGas := p.RequiredGas(sendInput)
	require.Equal(t, uint64(2000), sendRequiredGas)
	balanceInput := []byte{}
	for name, m := range p.ABI.Methods {
		if name == "balance" {
			balanceInput = m.ID
		}
	}
	require.NotEmpty(t, balanceInput)
	balanceRequiredGas := p.RequiredGas(balanceInput)
	require.Equal(t, uint64(1000), balanceRequiredGas)
	// invalid method
	require.Equal(t, uint64(0), p.RequiredGas([]byte{1, 1, 1, 1}))
}

func TestAddress(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper()
	p, err := bank.NewPrecompile(k.BankKeeper(), k)
	require.Nil(t, err)
	require.Equal(t, common.HexToAddress(bank.BankAddress), p.Address())
}
