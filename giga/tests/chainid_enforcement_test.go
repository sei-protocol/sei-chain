package giga_test

import (
	"math/big"
	"testing"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

// TestChainIDEnforced_TypedTx pins chain-id enforcement for typed (non-legacy) transactions on
// every executor path: the synchronous giga driver, the giga OCC driver, and v2.
//
// Sender recovery does not itself compare chain ids. helpers.RecoverAddressesFromTx builds the
// sig hash from the *signer's* chain id (go-ethereum modernSigner.Hash is
// tx.inner.sigHash(s.chainID)) and then recovers the key, which is only the last step of
// go-ethereum's modernSigner.Sender. Sender also rejects tx.ChainId() != s.chainID; Sei makes
// that comparison in EvmStatelessChecks (app/ante/evm_checktx.go) instead, which both giga
// drivers invoke before execution.
//
// Two consequences make this worth pinning. The ChainId field of a typed tx is not covered by
// the signature, so it can be edited on a signed transaction without invalidating it. And
// because recovery is chain-id agnostic, EvmStatelessChecks is the only step that rejects a
// mismatch — a legacy tx would still be caught by AdjustV, but a typed tx would not.
//
// The test therefore asserts both halves: recovery still returns the true sender for an edited
// ChainId, and the transaction is nevertheless rejected on every executor with no state change.
func TestChainIDEnforced_TypedTx(t *testing.T) {
	for _, mode := range []ExecutorMode{ModeGigaSequential, ModeGigaOCC, ModeV2withOCC} {
		mode := mode
		t.Run(mode.String(), func(t *testing.T) {
			blockTime := time.Now()
			accts := utils.NewTestAccounts(3)
			sender := utils.NewSigner()
			recipient := utils.NewSigner()

			tCtx := NewGigaTestContext(t, accts, blockTime, 2, mode)
			fundAccount(t, tCtx, sender.AccountAddress, new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1000)))
			tCtx.TestApp.EvmKeeper.SetAddressMapping(tCtx.Ctx, sender.AccountAddress, sender.EvmAddress)
			tCtx.TestApp.EvmKeeper.SetAddressMapping(tCtx.Ctx, recipient.AccountAddress, recipient.EvmAddress)

			to := recipient.EvmAddress
			value := new(big.Int).Mul(big.NewInt(1e12), big.NewInt(7))
			inner := &ethtypes.DynamicFeeTx{
				ChainID:   big.NewInt(config.DefaultChainID),
				Nonce:     0,
				GasFeeCap: big.NewInt(100000000000),
				GasTipCap: big.NewInt(100000000000),
				Gas:       21000,
				To:        &to,
				Value:     value,
			}
			honest, err := ethtypes.SignTx(ethtypes.NewTx(inner), sender.EvmSigner, sender.EvmPrivateKey)
			require.NoError(t, err)

			// Same signature, different ChainId field. The signature stays valid because the
			// sig hash is built from the signer's chain id, not from this field.
			v, r, s := honest.RawSignatureValues()
			mismatched := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
				ChainID:   big.NewInt(config.DefaultChainID + 1),
				Nonce:     inner.Nonce,
				GasFeeCap: inner.GasFeeCap,
				GasTipCap: inner.GasTipCap,
				Gas:       inner.Gas,
				To:        inner.To,
				Value:     inner.Value,
				V:         v,
				R:         r,
				S:         s,
			})

			// go-ethereum rejects it outright, which is the behaviour Sei relocates.
			gethSigner := ethtypes.LatestSignerForChainID(big.NewInt(config.DefaultChainID))
			_, err = ethtypes.Sender(gethSigner, mismatched)
			require.Error(t, err, "go-ethereum Sender rejects a mismatched chain id")

			// Sei's recovery accepts it and returns the true sender, so the guard below is the
			// only thing keeping it out of a block.
			recovered, _, _, err := evmante.RecoverSenderFromEthTx(
				tCtx.Ctx, mismatched, big.NewInt(config.DefaultChainID))
			require.NoError(t, err, "recovery is chain-id agnostic")
			require.Equal(t, sender.EvmAddress, recovered, "recovery returns the true sender")

			// EvmStatelessChecks must reject it on this executor, leaving no trace.
			before := weiBalanceOf(t, tCtx, recipient.AccountAddress)
			_, results, err := RunBlock(t, tCtx, [][]byte{encodeEthTx(t, mismatched)})
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.Equal(t, sdkerrorsInvalidChainIDCode, results[0].Code)
			require.Equal(t, "invalid chain-id", results[0].Log)
			require.Equal(t, 0,
				new(big.Int).Sub(weiBalanceOf(t, tCtx, recipient.AccountAddress), before).Sign(),
				"a rejected transaction must not move value")
			require.Equal(t, uint64(0),
				tCtx.TestApp.GigaEvmKeeper.GetNonce(tCtx.Ctx, sender.EvmAddress),
				"a rejected transaction must not consume the nonce")

			// The same transaction with the correct chain id still executes, so the guard is
			// not over-broad.
			before = weiBalanceOf(t, tCtx, recipient.AccountAddress)
			_, results, err = RunBlock(t, tCtx, [][]byte{encodeEthTx(t, honest)})
			require.NoError(t, err)
			require.Equal(t, uint32(0), results[0].Code, results[0].Log)
			require.Equal(t, 0,
				new(big.Int).Sub(weiBalanceOf(t, tCtx, recipient.AccountAddress), before).Cmp(value))
		})
	}
}

// sdkerrorsInvalidChainIDCode is sdkerrors.ErrInvalidChainID, which lives in the root codespace.
const sdkerrorsInvalidChainIDCode = uint32(28)

// weiBalanceOf returns the account's balance in wei, combining the usei and wei components.
func weiBalanceOf(t testing.TB, tCtx *GigaTestContext, addr sdk.AccAddress) *big.Int {
	t.Helper()
	usei := tCtx.TestApp.BankKeeper.GetBalance(tCtx.Ctx, addr, "usei").Amount.BigInt()
	wei := tCtx.TestApp.BankKeeper.GetWeiBalance(tCtx.Ctx, addr).BigInt()
	return new(big.Int).Add(new(big.Int).Mul(usei, big.NewInt(1e12)), wei)
}

// encodeEthTx wraps a signed ethereum transaction in the MsgEVMTransaction envelope and encodes
// it to the bytes a block carries.
func encodeEthTx(t testing.TB, ethTx *ethtypes.Transaction) []byte {
	t.Helper()
	txConfig := app.MakeEncodingConfig().TxConfig
	txData, err := ethtx.NewTxDataFromTx(ethTx)
	require.NoError(t, err)
	msg, err := types.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	txBuilder := txConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msg))
	txBuilder.SetGasLimit(10000000000)
	txBytes, err := txConfig.TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)
	return txBytes
}
