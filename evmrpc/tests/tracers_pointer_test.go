package tests

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// TestTraceNativePointerTransferMatchesReceipt traces a transfer through the ERC20 pointer
// of a native denom, which the pointer makes through the bank precompile's send, and
// requires the trace to fail where the chain failed and to report the gas it charged.
func TestTraceNativePointerTransferMatchesReceipt(t *testing.T) {
	holder := getSeiAddrWithMnemonic(mnemonic1)
	pointer := crypto.CreateAddress(getAddrWithMnemonic(mnemonic1), 0)
	for _, tc := range []struct {
		name  string
		denom string
		// extendingDenom, when set, is registered with a pointer of its own.
		extendingDenom string
		status         string
	}{
		{name: "denom", denom: "ufoo", status: "0x1"},
		// The chain resolves denom to its own pointer, not to the pointer of the denom
		// extending it, so the bank precompile accepts the transfer.
		{name: "denom another denom extends", denom: "factory/creator/pamp", extendingDenom: "factory/creator/pamp/vest", status: "0x1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initializers := []func(sdk.Context, *app.App){
				mnemonicInitializer(mnemonic1),
				nativePointerInitializer(t, tc.denom, pointer, holder),
			}
			if tc.extendingDenom != "" {
				extendingPointer := common.HexToAddress("0x00000000000000000000000000000000000a0002")
				initializers = append(initializers, nativePointerInitializer(t, tc.extendingDenom, extendingPointer, holder))
			}
			deployData := deployNativePointer(t, 0, tc.denom)
			transferData := transferThroughPointer(t, 1, pointer)
			transfer := signTxWithMnemonic(transferData, mnemonic1)
			SetupTestServer(t,
				[][][]byte{{signAndEncodeTx(deployData, mnemonic1), encodeEvmTx(transferData, transfer)}},
				initializers...,
			).Run(func(port int) {
				res := sendRequestWithNamespace("eth", port, "getTransactionReceipt", transfer.Hash().Hex())
				receipt, ok := res["result"].(map[string]interface{})
				require.True(t, ok, "receipt: %v", res)
				require.Equal(t, tc.status, receipt["status"])

				res = sendRequestWithNamespace("debug", port, "traceTransaction", transfer.Hash().Hex(), map[string]interface{}{"tracer": "callTracer"})
				trace, ok := res["result"].(map[string]interface{})
				require.True(t, ok, "trace: %v", res)
				_, failed := trace["error"]
				require.Equal(t, tc.status == "0x0", failed, "trace: %v", trace)
				require.Equal(t, receipt["gasUsed"], trace["gasUsed"])
			})
		})
	}
}

// nativePointerInitializer registers pointer as the ERC20 pointer of denom and gives holder
// some of denom to transfer.
func nativePointerInitializer(t *testing.T, denom string, pointer common.Address, holder sdk.AccAddress) func(sdk.Context, *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		require.NoError(t, a.EvmKeeper.SetERC20NativePointer(ctx, denom, pointer))
		amt := sdk.NewCoins(sdk.NewCoin(denom, sdk.NewInt(1000)))
		require.NoError(t, a.BankKeeper.MintCoins(ctx, types.ModuleName, amt))
		require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, holder, amt))
	}
}

func deployNativePointer(t *testing.T, nonce uint64, denom string) ethtypes.TxData {
	args, err := native.GetParsedABI().Pack("", denom, denom, denom, uint8(6))
	require.NoError(t, err)
	return &ethtypes.DynamicFeeTx{
		Nonce:     nonce,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       5000000,
		Data:      append(native.GetBin(), args...),
		ChainID:   chainId,
	}
}

func transferThroughPointer(t *testing.T, nonce uint64, pointer common.Address) ethtypes.TxData {
	recipient := common.HexToAddress("0x00000000000000000000000000000000000a0001")
	data, err := native.GetParsedABI().Pack("transfer", recipient, big.NewInt(1))
	require.NoError(t, err)
	return &ethtypes.DynamicFeeTx{
		Nonce:     nonce,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       500000,
		To:        &pointer,
		Data:      data,
		ChainID:   chainId,
	}
}
