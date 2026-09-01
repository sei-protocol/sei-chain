package giga_test

import (
	"crypto/ecdsa"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
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
// See CON-411 for the source file and the original docker-cluster
// reproduction, and app/eip7702_selfbalance_drain_test.go for the V2-only
// regression test this mirrors.
const drainContractCreationCode = "6080604052348015600e575f5ffd5b5060f58061001b5f395ff3fe6080604052348015600e575f5ffd5b50600436106026575f3560e01c80639890220b14602a575b5f5ffd5b60306032565b005b60405161dead905f90829047908381818185875af1925050503d805f81146073576040519150601f19603f3d011682016040523d82523d5f602084013e6078565b606091505b505090508060bb5760405162461bcd60e51b815260206004820152600c60248201526b191c985a5b8819985a5b195960a21b604482015260640160405180910390fd5b505056fea26469706673582212204a35f48107480beffc2123e3b6502a44bbae2ce9df637b35d8e1c97dd3ca7acf64736f6c63430008240033"

// drainSelector is the 4-byte selector for drain().
const drainSelector = "9890220b"

// encodeSignedEVMTx converts a signed Ethereum transaction of any supported
// type into a Sei tx envelope ready for RunBlock.
func encodeSignedEVMTx(t *testing.T, signedTx *ethtypes.Transaction) []byte {
	t.Helper()
	txData, err := ethtx.NewTxDataFromTx(signedTx)
	require.NoError(t, err)
	msg, err := types.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	tc := app.MakeEncodingConfig().TxConfig
	txBuilder := tc.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msg))
	bz, err := tc.TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)
	return bz
}

// buildEIP7702SelfBalanceDrainTxs builds the delegate/drain/undelegate
// sequence, associating both accounts first so Giga uses its native EIP-7702
// path instead of deferring an unassociated authority to V2.
func buildEIP7702SelfBalanceDrainTxs(t *testing.T, tCtx *GigaTestContext, sponsor, authority utils.TestAcct) (txs [][]byte, drainAddr common.Address) {
	t.Helper()
	// Three sponsor txs at up to 500,000 gas * 100 gwei each need up to 4.5e16
	// wei; fund generously above that.
	fundAccount(t, tCtx, sponsor.AccountAddress, big.NewInt(1_000_000_000_000_000_000)) // gas money only
	fundAccount(t, tCtx, authority.AccountAddress, big.NewInt(1_000_000_000_000_000))   // the balance drain() will empty
	tCtx.TestApp.EvmKeeper.SetAddressMapping(tCtx.Ctx, sponsor.AccountAddress, sponsor.EvmAddress)
	tCtx.TestApp.EvmKeeper.SetAddressMapping(tCtx.Ctx, authority.AccountAddress, authority.EvmAddress)
	tCtx.TestApp.GigaEvmKeeper.SetAddressMapping(tCtx.Ctx, sponsor.AccountAddress, sponsor.EvmAddress)
	tCtx.TestApp.GigaEvmKeeper.SetAddressMapping(tCtx.Ctx, authority.AccountAddress, authority.EvmAddress)

	chainID := big.NewInt(config.DefaultChainID)
	signer := ethtypes.NewPragueSigner(chainID)
	gasPrice := big.NewInt(100_000_000_000) // 100 gwei
	dead := common.HexToAddress("0x000000000000000000000000000000000000dEaD")

	signLegacy := func(key *ecdsa.PrivateKey, txdata *ethtypes.LegacyTx) *ethtypes.Transaction {
		signedTx, err := ethtypes.SignTx(ethtypes.NewTx(txdata), signer, key)
		require.NoError(t, err)
		return signedTx
	}
	signSetCode := func(key *ecdsa.PrivateKey, txdata *ethtypes.SetCodeTx) *ethtypes.Transaction {
		signedTx, err := ethtypes.SignNewTx(key, signer, txdata)
		require.NoError(t, err)
		return signedTx
	}

	// tx0 (sponsor nonce 0): deploy Drain.
	deployTx := signLegacy(sponsor.EvmPrivateKey, &ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: gasPrice,
		Gas:      500_000,
		Data:     common.FromHex(drainContractCreationCode),
	})
	drainAddr = crypto.CreateAddress(sponsor.EvmAddress, 0)

	// tx1 (sponsor nonce 1): sponsored authorization delegating authority -> Drain.
	// Applying it bumps the authority's own nonce 0 -> 1.
	delegateAuth, err := ethtypes.SignSetCode(authority.EvmPrivateKey, ethtypes.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: drainAddr,
		Nonce:   0,
	})
	require.NoError(t, err)
	delegateTx := signSetCode(sponsor.EvmPrivateKey, &ethtypes.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     1,
		GasTipCap: uint256.MustFromBig(gasPrice),
		GasFeeCap: uint256.MustFromBig(gasPrice),
		Gas:       200_000,
		To:        dead,
		AuthList:  []ethtypes.SetCodeAuthorization{delegateAuth},
	})

	// tx2 (sponsor nonce 2): call authority, running Drain.drain() in its
	// context. Empties authority's balance via SELFBALANCE + CALL.
	drainCallTx := signLegacy(sponsor.EvmPrivateKey, &ethtypes.LegacyTx{
		Nonce:    2,
		GasPrice: gasPrice,
		Gas:      200_000,
		To:       &authority.EvmAddress,
		Data:     common.FromHex(drainSelector),
	})

	// tx3 (authority nonce 1, self-sponsored): undelegate. Self-sponsored
	// auth nonce = tx nonce + 1. Balance is now empty, so the fee check fails.
	undelegateAuth, err := ethtypes.SignSetCode(authority.EvmPrivateKey, ethtypes.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: common.Address{}, // zero address == undelegate
		Nonce:   2,
	})
	require.NoError(t, err)
	undelegateTx := signSetCode(authority.EvmPrivateKey, &ethtypes.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     1,
		GasTipCap: uint256.MustFromBig(gasPrice),
		GasFeeCap: uint256.MustFromBig(gasPrice),
		Gas:       200_000,
		To:        dead,
		AuthList:  []ethtypes.SetCodeAuthorization{undelegateAuth},
	})

	return [][]byte{
		encodeSignedEVMTx(t, deployTx),
		encodeSignedEVMTx(t, delegateTx),
		encodeSignedEVMTx(t, drainCallTx),
		encodeSignedEVMTx(t, undelegateTx),
	}, drainAddr
}

// TestGigaValidation_EIP7702SelfBalanceDrain_LastResultsHash is CON-411's
// Giga-differential test. Any fee/nonce/balance validation failure makes
// Giga abort the whole batch and re-run it through V2 (app/app.go,
// executeEVMTxWithGigaExecutor), so this checks that hand-off is lossless
// rather than testing independent Giga logic -- there is none for this
// failure class.
func TestGigaValidation_EIP7702SelfBalanceDrain_LastResultsHash(t *testing.T) {
	runCase := func(t *testing.T, gigaMode ExecutorMode) {
		blockTime := time.Now()
		accts := utils.NewTestAccounts(3)
		sponsor := utils.NewSigner()
		authority := utils.NewSigner()

		v2Ctx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2Sequential)
		v2Txs, _ := buildEIP7702SelfBalanceDrainTxs(t, v2Ctx, sponsor, authority)
		_, v2Results, err := RunBlock(t, v2Ctx, v2Txs)
		require.NoError(t, err)
		require.Len(t, v2Results, 4)
		require.Equal(t, uint32(0), v2Results[0].Code, "V2: deploy should succeed")
		require.Equal(t, uint32(0), v2Results[1].Code, "V2: delegate should succeed")
		require.Equal(t, uint32(0), v2Results[2].Code, "V2: drain call should succeed")
		require.NotEqual(t, uint32(0), v2Results[3].Code, "V2: undelegate should fail its real balance check")

		gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, gigaMode)
		gigaTxs, _ := buildEIP7702SelfBalanceDrainTxs(t, gigaCtx, sponsor, authority)
		_, gigaResults, err := RunBlock(t, gigaCtx, gigaTxs)
		require.NoError(t, err)
		require.Len(t, gigaResults, 4)

		CompareDeterministicFields(t, "EIP7702SelfBalanceDrain/"+gigaMode.String(), v2Results, gigaResults)
		CompareLastResultsHash(t, "EIP7702SelfBalanceDrain/"+gigaMode.String(), v2Results, gigaResults)
	}
	for _, mode := range []ExecutorMode{ModeGigaSequential, ModeGigaOCC} {
		t.Run(mode.String(), func(t *testing.T) { runCase(t, mode) })
	}
}
