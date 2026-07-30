package evmrpc_test

import (
	"crypto/sha256"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/export"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestEncodeTmBlock_EmptyTransactions(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	block := &coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data:   tmtypes.Data{},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}

	// Call EncodeTmBlock with empty transactions
	result, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, block, k, true, false, false, false, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.Nil(t, err)

	// Assert txHash is equal to ethtypes.EmptyTxsHash
	require.Equal(t, ethtypes.EmptyTxsHash, result["transactionsRoot"])
}

func TestEncodeBankMsg(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	fromSeiAddr, _ := testkeeper.MockAddressPair()
	toSeiAddr, _ := testkeeper.MockAddressPair()
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(banktypes.NewMsgSend(fromSeiAddr, toSeiAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))))
	tx := b.GetTx()
	resBlock := coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{func() []byte {
					bz, _ := Encoder(tx)
					return bz
				}()},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}
	res, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, &resBlock, k, true, false, false, false, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.Nil(t, err)
	txs := res["transactions"].([]any)
	require.Equal(t, 0, len(txs))
}

func TestEncodeWasmExecuteMsg(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithBlockHeight(MockHeight8)
	fromSeiAddr, fromEvmAddr := testkeeper.MockAddressPair()
	toSeiAddr, _ := testkeeper.MockAddressPair()
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(&wasmtypes.MsgExecuteContract{
		Sender:   fromSeiAddr.String(),
		Contract: toSeiAddr.String(),
		Msg:      []byte{1, 2, 3},
	})
	tx := b.GetTx()
	bz, _ := Encoder(tx)
	txHash := sha256.Sum256(bz)
	hash := common.BytesToHash(txHash[:])
	testkeeper.MustMockReceipt(t, k, ctx, hash, &types.Receipt{
		TransactionIndex: 1,
		From:             fromEvmAddr.Hex(),
		TxHashHex:        hash.Hex(),
	})
	receipt := testkeeper.WaitForReceipt(t, k, ctx, hash)
	require.Equal(t, hash.Hex(), receipt.TxHashHex)
	resBlock := coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{bz},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}
	res, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, &resBlock, k, true, false, true, false, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.Nil(t, err)
	txs := res["transactions"].([]any)
	require.Equal(t, 1, len(txs))
	ti := uint64(0)
	bh := common.HexToHash(MockBlockID.Hash.String())
	to := common.Address(toSeiAddr)
	require.Equal(t, &export.RPCTransaction{
		BlockHash:        &bh,
		BlockNumber:      (*hexutil.Big)(big.NewInt(MockHeight8)),
		From:             fromEvmAddr,
		To:               &to,
		Input:            []byte{1, 2, 3},
		Hash:             common.Hash(sha256.Sum256(bz)),
		TransactionIndex: (*hexutil.Uint64)(&ti),
		V:                nil,
		R:                nil,
		S:                nil,
	}, txs[0].(*export.RPCTransaction))
}

func TestEncodeBankTransferMsg(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	fromSeiAddr, fromEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, fromSeiAddr, fromEvmAddr)
	toSeiAddr, _ := testkeeper.MockAddressPair()
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(&banktypes.MsgSend{
		FromAddress: fromSeiAddr.String(),
		ToAddress:   toSeiAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("usei", sdk.OneInt())),
	})
	ti := uint64(0)
	tx := b.GetTx()
	bz, _ := Encoder(tx)
	resBlock := coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{bz},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}
	res, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, &resBlock, k, true, true, false, false, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.Nil(t, err)
	txs := res["transactions"].([]any)
	require.Equal(t, 1, len(txs))
	bh := common.HexToHash(MockBlockID.Hash.String())
	to := common.Address(toSeiAddr)
	require.Equal(t, &export.RPCTransaction{
		BlockHash:        &bh,
		BlockNumber:      (*hexutil.Big)(big.NewInt(MockHeight8)),
		From:             fromEvmAddr,
		To:               &to,
		Value:            (*hexutil.Big)(big.NewInt(1_000_000_000_000)),
		Hash:             common.Hash(sha256.Sum256(bz)),
		TransactionIndex: (*hexutil.Uint64)(&ti),
		V:                nil,
		R:                nil,
		S:                nil,
	}, txs[0].(*export.RPCTransaction))
}

// Wasm-execute txs contribute receipt.GasUsed to the block's gasUsed total.
func TestEncodeWasmExecuteMsg_GasUsedFromReceipt(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithBlockHeight(MockHeight8)
	fromSeiAddr, fromEvmAddr := testkeeper.MockAddressPair()
	toSeiAddr, _ := testkeeper.MockAddressPair()
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(&wasmtypes.MsgExecuteContract{
		Sender:   fromSeiAddr.String(),
		Contract: toSeiAddr.String(),
		Msg:      []byte{1, 2, 3},
	})
	tx := b.GetTx()
	bz, _ := Encoder(tx)
	txHash := sha256.Sum256(bz)
	hash := common.BytesToHash(txHash[:])
	testkeeper.MustMockReceipt(t, k, ctx, hash, &types.Receipt{
		TransactionIndex: 1,
		From:             fromEvmAddr.Hex(),
		TxHashHex:        hash.Hex(),
		GasUsed:          54321,
	})
	resBlock := coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{bz},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}
	res, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, &resBlock, k, true, false, true, false, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.Nil(t, err)
	require.Equal(t, hexutil.Uint64(54321), res["gasUsed"])
	txs := res["transactions"].([]any)
	require.Equal(t, 1, len(txs))
}

// Bank-send txs without a matching EVM receipt contribute 0 to the block's
// gasUsed total. (This is the Autobahn case for plain MsgSend txs; under
// legacy, bank sends that emit EVM-relevant events would have a synthetic
// receipt and contribute receipt.GasUsed.)
func TestEncodeBankTransferMsg_NoReceiptGasUsedZero(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	fromSeiAddr, fromEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, fromSeiAddr, fromEvmAddr)
	toSeiAddr, _ := testkeeper.MockAddressPair()
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(&banktypes.MsgSend{
		FromAddress: fromSeiAddr.String(),
		ToAddress:   toSeiAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("usei", sdk.OneInt())),
	})
	tx := b.GetTx()
	bz, _ := Encoder(tx)
	resBlock := coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{bz},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}
	res, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, &resBlock, k, true, true, false, false, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.Nil(t, err)
	require.Equal(t, hexutil.Uint64(0), res["gasUsed"])
	txs := res["transactions"].([]any)
	require.Equal(t, 1, len(txs))
}

// TestEncodeTmBlock_ExcludeUntraceable pins the block-side counterpart of the
// tx-side discriminator: a correct-nonce ante failure (e.g. insufficient
// funds) lands a stub receipt in the receipt store. filterTransactions's
// isReceiptFromAnteError post-v5.8.0 only matches nonce-error VmError
// content, so the stub flows through to EncodeTmBlock — where
// excludeUntraceable=true catches it via EffectiveGasPrice==0 && GasUsed==0.
//
// We use the global EVMKeeper (where setup_test.go's fixtures live) and a
// post-v5.8.0 ctx so filterTransactions takes the production branch.
// DebugTracePanicTx isn't suitable here (Nonce=100, gets filtered by
// filterTransactions's nonce-bumping check before reaching EncodeTmBlock);
// we register a correct-nonce ante-stub fixture instead.
func TestEncodeTmBlock_ExcludeUntraceable(t *testing.T) {
	k := EVMKeeper

	// Build a correct-nonce EVM tx whose receipt mimics an ante-deferred
	// stub: EffectiveGasPrice=0, GasUsed=0, VmError set. We need a fresh
	// sender so startOfBlockNonce equals the tx's nonce — otherwise
	// filterTransactions's nonce-bumping branch filters it out at
	// utils.go:253 before EncodeTmBlock sees it.
	chainID := big.NewInt(config.DefaultChainID)
	privKey, _ := crypto.GenerateKey()
	signer := ethtypes.MakeSigner(types.DefaultChainConfig().EthereumConfig(chainID), big.NewInt(Ctx.BlockHeight()), uint64(Ctx.BlockTime().Unix()))
	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	rawTx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		Nonce: 0, GasFeeCap: big.NewInt(10), Gas: 22000, To: &to, Value: big.NewInt(0), ChainID: chainID,
	}), signer, privKey)
	require.NoError(t, err)
	td, err := ethtx.NewDynamicFeeTx(rawTx)
	require.NoError(t, err)
	msg, err := types.NewMsgEVMTransaction(td)
	require.NoError(t, err)
	builder := TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	anteStubTx := builder.GetTx()
	anteStubBz, _ := Encoder(anteStubTx)
	anteStubHash := rawTx.Hash()

	// Mock the stub receipt on EVMKeeper so getOrSetCachedReceipt finds it.
	stubHeight := int64(MockHeight103)
	stubCtx := Ctx.WithBlockHeight(stubHeight)
	require.NoError(t, k.MockReceipt(stubCtx, anteStubHash, &types.Receipt{
		BlockNumber:      uint64(stubHeight),
		TransactionIndex: 0,
		TxHashHex:        anteStubHash.Hex(),
		VmError:          "insufficient funds",
	}))

	nonPanicBz, _ := Encoder(DebugTraceNonPanicTx)

	block := &coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(stubHeight),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{anteStubBz, nonPanicBz},
			},
			LastCommit: &tmtypes.Commit{Height: stubHeight - 1},
		},
	}

	// Post-v5.8.0 ctx so filterTransactions's isReceiptFromAnteError stays
	// narrow (nonce-only VmError) — the production path where the stub
	// flows through and EncodeTmBlock has to apply its own check.
	ctx := Ctx.WithBlockHeight(stubHeight).WithClosestUpgradeName(LatestCtxUpgradeName)
	ctxProvider := func(int64) sdk.Context { return ctx }
	txConfigProvider := func(int64) client.TxConfig { return TxConfig }

	// excludeUntraceable=true: ante stub dropped, revert kept.
	res, err := evmrpc.EncodeTmBlock(ctxProvider, txConfigProvider, block, k,
		false /*fullTx*/, false /*includeBankTransfers*/, false /*includeSyntheticTxs*/, true, /*excludeUntraceable*/
		evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.NoError(t, err)
	txs := res["transactions"].([]any)
	require.Len(t, txs, 1, "expected only the revert to survive, got %v", txs)
	require.Equal(t, strings.ToLower(TestNonPanicTxHash), strings.ToLower(txs[0].(string)))

	// excludeUntraceable=false: ante stub flows through (matches regular
	// eth_getBlockBy* behavior per PR #2343's TestAnteFailureOthers).
	res, err = evmrpc.EncodeTmBlock(ctxProvider, txConfigProvider, block, k,
		false /*fullTx*/, false /*includeBankTransfers*/, false /*includeSyntheticTxs*/, false, /*excludeUntraceable*/
		evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.NoError(t, err)
	txs = res["transactions"].([]any)
	require.Len(t, txs, 2, "expected revert + ante stub to flow through, got %v", txs)
}

func TestEVMLaunchHeightValidation(t *testing.T) {
	// Test ValidateEVMBlockHeight function
	// Should pass for pacific-1 with valid height
	err := evmrpc.ValidateEVMBlockHeight("pacific-1", 79123881)
	require.NoError(t, err)

	err = evmrpc.ValidateEVMBlockHeight("pacific-1", 79123882)
	require.NoError(t, err)

	// Should fail for pacific-1 with invalid height
	err = evmrpc.ValidateEVMBlockHeight("pacific-1", 79123880)
	require.Error(t, err)
	require.Contains(t, err.Error(), "EVM is only supported from block 79123881 onwards")

	// Should pass for other chains with any height
	err = evmrpc.ValidateEVMBlockHeight("atlantic-2", 1)
	require.NoError(t, err)

	err = evmrpc.ValidateEVMBlockHeight("test-chain", 1)
	require.NoError(t, err)
}

func TestEVMBlockValidationEdgeCases(t *testing.T) {
	// Test edge cases for EVM block validation

	// Test exactly at launch height
	err := evmrpc.ValidateEVMBlockHeight("pacific-1", 79123881)
	require.NoError(t, err)

	// Test one block before launch height
	err = evmrpc.ValidateEVMBlockHeight("pacific-1", 79123880)
	require.Error(t, err)
	require.Equal(t, "EVM is only supported from block 79123881 onwards", err.Error())

	// Test way before launch height
	err = evmrpc.ValidateEVMBlockHeight("pacific-1", 1000000)
	require.Error(t, err)
	require.Equal(t, "EVM is only supported from block 79123881 onwards", err.Error())

	// Test block 0
	err = evmrpc.ValidateEVMBlockHeight("pacific-1", 0)
	require.Error(t, err)
	require.Equal(t, "EVM is only supported from block 79123881 onwards", err.Error())
}

func TestEVMBlockValidationDifferentChains(t *testing.T) {
	// Test that validation only applies to pacific-1
	chains := []string{"atlantic-2", "arctic-1", "test-chain", "unknown-chain", ""}

	for _, chainID := range chains {
		// All non-pacific-1 chains should pass validation for any block height
		err := evmrpc.ValidateEVMBlockHeight(chainID, 1)
		require.NoError(t, err, "Chain %s should not validate block heights", chainID)

		err = evmrpc.ValidateEVMBlockHeight(chainID, 0)
		require.NoError(t, err, "Chain %s should not validate block heights", chainID)

		err = evmrpc.ValidateEVMBlockHeight(chainID, 79123880)
		require.NoError(t, err, "Chain %s should not validate block heights", chainID)
	}
}
