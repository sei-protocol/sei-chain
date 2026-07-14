package app

import (
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestPrepareGigaStaticTxsConcurrentlyPrecomputesEVMMeta(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	wrapper := NewTestWrapper(t, tm, valPub, false)

	privKey := secp256k1.GenPrivKey()
	key, err := crypto.HexToECDSA(hex.EncodeToString(privKey.Bytes()))
	require.NoError(t, err)

	to := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	txData := &ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &to,
	}
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(wrapper.Ctx.BlockHeight()), uint64(wrapper.Ctx.BlockTime().Unix()))
	ethTx, err := ethtypes.SignTx(ethtypes.NewTx(txData), signer, key)
	require.NoError(t, err)

	ethTxData, err := ethtx.NewTxDataFromTx(ethTx)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(ethTxData)
	require.NoError(t, err)

	txBuilder := wrapper.App.GetTxConfig().NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msg))
	txBytes, err := wrapper.App.GetTxConfig().TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)

	typedTxs, err := wrapper.App.DecodeTxBytesConcurrently(wrapper.Ctx.Context(), [][]byte{txBytes})
	require.NoError(t, err)
	require.NotNil(t, typedTxs[0])

	chainID := wrapper.App.GigaEvmKeeper.ChainID(wrapper.Ctx)
	staticTxs := wrapper.App.prepareGigaStaticTxsConcurrently(wrapper.Ctx, typedTxs, chainID)

	require.Len(t, staticTxs, 1)
	require.NotNil(t, staticTxs[0])
	require.Nil(t, staticTxs[0].errResult)
	require.NoError(t, staticTxs[0].err)
	require.NotNil(t, staticTxs[0].evm)
	require.Equal(t, ethTx.Hash(), staticTxs[0].evm.ethTx.Hash())
	require.Equal(t, crypto.PubkeyToAddress(key.PublicKey), staticTxs[0].evm.sender)
	require.Equal(t, sdk.AccAddress(privKey.PubKey().Address()), staticTxs[0].evm.seiAddr)
}

func TestGigaStaticBlockPipelineAllowsMultipleHeights(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	wrapper := NewTestWrapper(t, tm, valPub, false)
	wrapper.App.GigaExecutorEnabled = true

	txBytes, sender, seiAddr := buildGigaStaticTestTx(t, wrapper, 1)
	txs := [][]byte{txBytes}
	ctxH := wrapper.Ctx.WithBlockHeight(10)
	ctxNext := wrapper.Ctx.WithBlockHeight(11)
	reqH := &BlockProcessRequest{Height: 10, Hash: []byte("block-h"), Time: tm}
	reqNext := &BlockProcessRequest{Height: 11, Hash: []byte("block-h-plus-1"), Time: tm}

	wrapper.App.StartGigaStaticBlockProcessing(ctxNext, txs, reqNext, nil)
	wrapper.App.StartGigaStaticBlockProcessing(ctxH, txs, reqH, nil)

	staticH := wrapper.App.waitForGigaStaticBlockProcessing(reqH, txs)
	require.NotNil(t, staticH)
	require.Len(t, staticH.typedTxs, 1)
	require.Len(t, staticH.staticTxs, 1)
	require.Equal(t, sender, staticH.staticTxs[0].evm.sender)
	require.Equal(t, seiAddr, staticH.staticTxs[0].evm.seiAddr)

	wrapper.App.gigaStaticPipelineMutex.Lock()
	_, nextStillPrepared := wrapper.App.gigaStaticPipeline[newGigaStaticBlockKey(reqNext.Height, reqNext.Hash, txs)]
	wrapper.App.gigaStaticPipelineMutex.Unlock()
	require.True(t, nextStillPrepared, "future block static work should remain available while current block is consumed")

	staticNext := wrapper.App.waitForGigaStaticBlockProcessing(reqNext, txs)
	require.NotNil(t, staticNext)
	require.Equal(t, sender, staticNext.staticTxs[0].evm.sender)
	require.Equal(t, seiAddr, staticNext.staticTxs[0].evm.seiAddr)
}

func TestGigaStaticBlockPipelineCapsSameHeightChurn(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	wrapper := NewTestWrapper(t, tm, valPub, false)
	wrapper.App.GigaExecutorEnabled = true

	txBytes, _, _ := buildGigaStaticTestTx(t, wrapper, 1)
	txs := [][]byte{txBytes}
	ctx := wrapper.Ctx.WithBlockHeight(10)
	for i := 0; i < gigaStaticPipelineMaxJobsPerHeight+1; i++ {
		req := &BlockProcessRequest{Height: 10, Hash: []byte{byte(i + 1)}, Time: tm}
		wrapper.App.StartGigaStaticBlockProcessing(ctx, txs, req, nil)
	}

	jobs := make([]*gigaStaticBlockJob, 0, gigaStaticPipelineMaxJobsPerHeight)
	wrapper.App.gigaStaticPipelineMutex.Lock()
	for key, job := range wrapper.App.gigaStaticPipeline {
		if key.height == 10 {
			jobs = append(jobs, job)
		}
	}
	wrapper.App.gigaStaticPipelineMutex.Unlock()

	require.Len(t, jobs, gigaStaticPipelineMaxJobsPerHeight)
	for _, job := range jobs {
		<-job.done
	}
}

func buildGigaStaticTestTx(t *testing.T, wrapper *TestWrapper, nonce uint64) ([]byte, common.Address, sdk.AccAddress) {
	t.Helper()

	privKey := secp256k1.GenPrivKey()
	key, err := crypto.HexToECDSA(hex.EncodeToString(privKey.Bytes()))
	require.NoError(t, err)

	to := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	txData := &ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &to,
	}
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(wrapper.Ctx.BlockHeight()), uint64(wrapper.Ctx.BlockTime().Unix()))
	ethTx, err := ethtypes.SignTx(ethtypes.NewTx(txData), signer, key)
	require.NoError(t, err)

	ethTxData, err := ethtx.NewTxDataFromTx(ethTx)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(ethTxData)
	require.NoError(t, err)

	txBuilder := wrapper.App.GetTxConfig().NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msg))
	txBytes, err := wrapper.App.GetTxConfig().TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)

	return txBytes, crypto.PubkeyToAddress(key.PublicKey), sdk.AccAddress(privKey.PubKey().Address())
}
