package app_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

// drainContractCreationCode is the compiled bytecode of:
//
//	contract Drain {
//	    function drain() external {
//	        (bool ok, ) = payable(0x…dEaD).call{value: address(this).balance}("");
//	        require(ok, "drain failed");
//	    }
//	}
//
// See CON-411 for the source file and the original docker-cluster reproduction.
const drainContractCreationCode = "6080604052348015600e575f5ffd5b5060f58061001b5f395ff3fe6080604052348015600e575f5ffd5b50600436106026575f3560e01c80639890220b14602a575b5f5ffd5b60306032565b005b60405161dead905f90829047908381818185875af1925050503d805f81146073576040519150601f19603f3d011682016040523d82523d5f602084013e6078565b606091505b505090508060bb5760405162461bcd60e51b815260206004820152600c60248201526b191c985a5b8819985a5b195960a21b604482015260640160405180910390fd5b505056fea26469706673582212204a35f48107480beffc2123e3b6502a44bbae2ce9df637b35d8e1c97dd3ca7acf64736f6c63430008240033"

// drainSelector is the 4-byte selector for drain().
const drainSelector = "9890220b"

// signAndEncodeEVMTx converts a signed Ethereum transaction of any supported
// type into a Sei tx envelope ready for ProcessBlock.
func signAndEncodeEVMTx(t *testing.T, a *app.App, signedTx *ethtypes.Transaction) []byte {
	t.Helper()
	typedTx, err := ethtx.NewTxDataFromTx(signedTx)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(typedTx)
	require.NoError(t, err)
	txBuilder := a.GetTxConfig().NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msg))
	bz, err := a.GetTxConfig().TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)
	return bz
}

// This is a regression test to ensure that accounts that lack the upfront balance to execute the transaction are
// executed the same across giga and v2.  Specifically, the transaction increments the nonce and generates a
// receipt with status=0, and gasUsed=0. The specific mechanism is a 7702 contract.
func TestEIP7702SelfBalanceDrainProducesStubReceipt(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	a := testWrapper.App
	ctx := testWrapper.Ctx.WithBlockHeight(1)
	chainID := a.EvmKeeper.ChainID(ctx)
	signer := ethtypes.NewPragueSigner(chainID)

	sponsorKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	sponsorAddr := crypto.PubkeyToAddress(sponsorKey.PublicKey)

	authorityKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	authorityAddr := crypto.PubkeyToAddress(authorityKey.PublicKey)

	fund := func(addr common.Address, usei int64) {
		coins := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(usei)))
		require.NoError(t, a.BankKeeper.MintCoins(ctx, "evm", coins))
		require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", sdk.AccAddress(addr[:]), coins))
	}
	fund(sponsorAddr, 10_000_000)  // gas money only
	fund(authorityAddr, 1_000_000) // the balance drain() will empty

	gasPrice := big.NewInt(100_000_000_000) // 100 gwei, matches other app tests
	dead := common.HexToAddress("0x000000000000000000000000000000000000dEaD")

	// tx0 (sponsor nonce 0): deploy Drain.
	deployTx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: gasPrice,
		Gas:      500_000,
		Data:     common.FromHex(drainContractCreationCode),
	}), signer, sponsorKey)
	require.NoError(t, err)
	drainAddr := crypto.CreateAddress(sponsorAddr, 0)

	// tx1 (sponsor nonce 1): sponsored authorization delegating authority -> Drain.
	// Applying it bumps the authority's own nonce 0 -> 1.
	delegateAuth, err := ethtypes.SignSetCode(authorityKey, ethtypes.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: drainAddr,
		Nonce:   0,
	})
	require.NoError(t, err)
	delegateTx, err := ethtypes.SignNewTx(sponsorKey, signer, &ethtypes.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     1,
		GasTipCap: uint256.MustFromBig(gasPrice),
		GasFeeCap: uint256.MustFromBig(gasPrice),
		Gas:       200_000,
		To:        dead,
		AuthList:  []ethtypes.SetCodeAuthorization{delegateAuth},
	})
	require.NoError(t, err)

	// tx2 (sponsor nonce 2): call authority, running Drain.drain() in its
	// context. Empties authority's balance via SELFBALANCE + CALL.
	drainCallTx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    2,
		GasPrice: gasPrice,
		Gas:      200_000,
		To:       &authorityAddr,
		Data:     common.FromHex(drainSelector),
	}), signer, sponsorKey)
	require.NoError(t, err)

	// tx3 (authority nonce 1, self-sponsored): undelegate. Self-sponsored
	// auth nonce = tx nonce + 1. Balance is now empty, so BuyGas fails.
	undelegateAuth, err := ethtypes.SignSetCode(authorityKey, ethtypes.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: common.Address{}, // zero address == undelegate
		Nonce:   2,
	})
	require.NoError(t, err)
	undelegateTx, err := ethtypes.SignNewTx(authorityKey, signer, &ethtypes.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     1,
		GasTipCap: uint256.MustFromBig(gasPrice),
		GasFeeCap: uint256.MustFromBig(gasPrice),
		Gas:       200_000,
		To:        dead,
		AuthList:  []ethtypes.SetCodeAuthorization{undelegateAuth},
	})
	require.NoError(t, err)

	txs := [][]byte{
		signAndEncodeEVMTx(t, a, deployTx),
		signAndEncodeEVMTx(t, a, delegateTx),
		signAndEncodeEVMTx(t, a, drainCallTx),
		signAndEncodeEVMTx(t, a, undelegateTx),
	}

	req := &abci.RequestFinalizeBlock{Header: &types.Header{ChainID: "sei-test", Height: 1}}
	_, txResults, _, err := a.ProcessBlock(ctx, txs, finalizeToBlockProcessReq(req), req.DecidedLastCommit, false, nil)
	require.NoError(t, err)
	require.Len(t, txResults, 4)

	require.Equal(t, uint32(0), txResults[0].Code, "deploy should succeed")
	require.Equal(t, uint32(0), txResults[1].Code, "delegate should succeed")
	require.Equal(t, uint32(0), txResults[2].Code, "drain call should succeed")

	// Ethereum-facing receipt (not the raw Cosmos ExecTxResult) carries the
	// stub signature: status=0, gasUsed=0.
	require.NotEqual(t, uint32(0), txResults[3].Code, "undelegate should fail ante's real BuyGas check")
	receipt, err := a.EvmKeeper.GetTransientReceipt(ctx, undelegateTx.Hash(), 3)
	require.NoError(t, err)
	require.Equal(t, uint32(0), receipt.Status, "the CON-375/411 stub-receipt signature")
	require.Equal(t, uint64(0), receipt.GasUsed, "the CON-375/411 stub-receipt signature")

	require.Equal(t, uint64(2), a.EvmKeeper.GetNonce(ctx, authorityAddr),
		"the authority's nonce still advances past the failed undelegate")

	// Ante failed before the auth list was processed, so the undelegate
	// never applied.
	wantCode := append([]byte{0xef, 0x01, 0x00}, drainAddr.Bytes()...)
	require.Equal(t, wantCode, a.EvmKeeper.GetCode(ctx, authorityAddr),
		"the authority remains delegated to Drain since the undelegate never applied")
}
