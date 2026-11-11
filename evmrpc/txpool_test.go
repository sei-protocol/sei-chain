package evmrpc_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/export"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/evmrpc/rpcutils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestTxPoolContent(t *testing.T) {
	body := "{\"jsonrpc\": \"2.0\",\"method\": \"txpool_content\",\"params\":[],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)

	// check pending has 1 txn in it
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	resObj = resObj["result"].(map[string]interface{})
	pendingMap := resObj["pending"].(map[string]interface{})
	require.Equal(t, 1, len(pendingMap))

	// check that txn
	for fromAddr, txns := range pendingMap {
		for nonce, txn := range txns.(map[string]interface{}) {
			require.NotZero(t, nonce)
			tx := txn.(map[string]interface{})
			require.Nil(t, tx["blockNumber"])
			require.Nil(t, tx["blockHash"])
			require.Equal(t, strings.ToLower(tx["from"].(string)), strings.ToLower(fromAddr))
			requireNotZeroHex(t, tx["gas"].(string))
			requireNotZeroHex(t, tx["gasPrice"].(string))
			// maxFeePerGas
			requireNotZeroHex(t, tx["maxFeePerGas"].(string))
			// maxPriorityFeePerGas
			// hash
			requireNotZeroHex(t, tx["hash"].(string))
			// input
			requireNotZeroHex(t, tx["input"].(string))
			// nonce -- can be 0
			// to
			requireNotZeroHex(t, tx["to"].(string))
			// transactionIndex -- not set yet for pending
			// value
			requireNotZeroHex(t, tx["value"].(string))
			// type -- can be 0
			// acccesslist-- can be any array value
			require.Equal(t, tx["chainId"], "0xae3f2") // 713714
			requireNotZeroHex(t, tx["v"].(string))
			requireNotZeroHex(t, tx["r"].(string))
			requireNotZeroHex(t, tx["s"].(string))
		}
	}

	// check queued has nothing in it
	queuedMap := resObj["queued"].(map[string]interface{})
	require.Equal(t, 0, len(queuedMap))
}

func requireNotZeroHex(t *testing.T, hexStr string) {
	hexStr = strings.TrimPrefix(hexStr, "0x")
	for i := 0; i < len(hexStr); i++ {
		if hexStr[i] != '0' {
			return // not all zeros
		}
	}
	t.Errorf("requireNotZeroHex: %s is all zeros", hexStr)
}

func TestTxPoolContentSenderRecovery(t *testing.T) {
	ctxProvider := func(int64) sdk.Context { return Ctx }
	txConfigProvider := func(int64) client.TxConfig { return TxConfig }

	api := evmrpc.NewTxPoolAPI(
		&MockClient{},
		EVMKeeper,
		ctxProvider,
		txConfigProvider,
		evmrpc.NewTxPoolConfig(10),
		evmrpc.ConnectionTypeHTTP,
	)

	content, err := api.Content(context.Background())
	require.NoError(t, err)

	pending := content["pending"]
	require.Len(t, pending, 1)
	require.Empty(t, content["queued"])

	ethMsg := evmtypes.MustGetEVMTransactionMessage(UnconfirmedTx)
	ethTx, _ := ethMsg.AsTransaction()

	expectedSender, err := rpcutils.RecoverEVMSenderWithContext(Ctx, ethTx)
	require.NoError(t, err)

	nonceStr := strconv.FormatUint(ethTx.Nonce(), 10)
	senderTxs, ok := pending[expectedSender.Hex()]
	if !ok {
		for addr := range pending {
			if strings.EqualFold(addr, expectedSender.Hex()) {
				senderTxs = pending[addr]
				ok = true
				break
			}
		}
	}
	require.True(t, ok, "pending txs keyed by expected sender not found")

	rpcTx, ok := senderTxs[nonceStr]
	require.True(t, ok)
	require.Equal(t, expectedSender, rpcTx.From)
	require.Equal(t, ethTx.Hash(), rpcTx.Hash)
}

// pagedMempoolClient serves UnconfirmedTxs the way Tendermint does outside
// unsafe mode: at most 100 txs per page, and ErrPageOutOfRange past the last
// page. Call i reads pools[i], and calls past the end read the last pool.
type pagedMempoolClient struct {
	MockClient
	pools [][]tmtypes.Tx
	err   error
	pages []int
}

func (c *pagedMempoolClient) UnconfirmedTxs(_ context.Context, page, perPage *int) (*coretypes.ResultUnconfirmedTxs, error) {
	if c.err != nil {
		return nil, c.err
	}
	pool := c.pools[min(len(c.pages), len(c.pools)-1)]
	p := 1
	if page != nil {
		p = *page
	}
	c.pages = append(c.pages, p)
	size := 30
	if perPage != nil && *perPage > 0 {
		size = min(*perPage, 100)
	}
	pages := max(1, (len(pool)+size-1)/size)
	if p < 1 || p > pages {
		return nil, fmt.Errorf("%w expected range: [1, %d], given %d", coretypes.ErrPageOutOfRange, pages, p)
	}
	first := (p - 1) * size
	last := min(len(pool), first+size)
	return &coretypes.ResultUnconfirmedTxs{Count: last - first, Total: len(pool), Txs: pool[first:last]}, nil
}

func newPagedTxPoolAPI(c *pagedMempoolClient, maxNumTxs int) *evmrpc.TxPoolAPI {
	return evmrpc.NewTxPoolAPI(
		c,
		EVMKeeper,
		func(int64) sdk.Context { return Ctx },
		func(int64) client.TxConfig { return TxConfig },
		evmrpc.NewTxPoolConfig(maxNumTxs),
		evmrpc.ConnectionTypeHTTP,
	)
}

// signedEVMTxs returns n encoded EVM txs from one fresh sender with nonces 0..n-1.
func signedEVMTxs(t *testing.T, n int) (common.Address, []tmtypes.Tx) {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	txs := make([]tmtypes.Tx, n)
	for i := range txs {
		txs[i] = encodeEVMTx(t, signEVMTx(t, key, uint64(i)))
	}
	return crypto.PubkeyToAddress(key.PublicKey), txs
}

func signEVMTx(t *testing.T, key *ecdsa.PrivateKey, nonce uint64) *ethtx.DynamicFeeTx {
	t.Helper()
	chainID := big.NewInt(config.DefaultChainID)
	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	signer := ethtypes.MakeSigner(evmtypes.DefaultChainConfig().EthereumConfig(chainID), big.NewInt(Ctx.BlockHeight()), uint64(Ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasFeeCap: big.NewInt(10),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(1),
	}), signer, key)
	require.NoError(t, err)
	txData, err := ethtx.NewDynamicFeeTx(tx)
	require.NoError(t, err)
	return txData
}

func encodeEVMTx(t *testing.T, txData *ethtx.DynamicFeeTx) tmtypes.Tx {
	t.Helper()
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	builder := TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	bz, err := Encoder(builder.GetTx())
	require.NoError(t, err)
	return bz
}

// cosmosTxs returns n distinct encoded bank sends.
func cosmosTxs(t *testing.T, n int) []tmtypes.Tx {
	t.Helper()
	from, _ := testkeeper.MockAddressPair()
	to, _ := testkeeper.MockAddressPair()
	txs := make([]tmtypes.Tx, n)
	for i := range txs {
		builder := TxConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(banktypes.NewMsgSend(from, to, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(int64(i+1)))))))
		bz, err := Encoder(builder.GetTx())
		require.NoError(t, err)
		txs[i] = bz
	}
	return txs
}

// pendingNonces returns the nonces Content lists as pending for sender.
func pendingNonces(t *testing.T, content map[string]map[string]map[string]*export.RPCTransaction, sender common.Address) []uint64 {
	t.Helper()
	nonces := make([]uint64, 0, len(content["pending"][sender.Hex()]))
	for nonceStr := range content["pending"][sender.Hex()] {
		nonce, err := strconv.ParseUint(nonceStr, 10, 64)
		require.NoError(t, err)
		nonces = append(nonces, nonce)
	}
	slices.Sort(nonces)
	return nonces
}

func nonceRange(from, to uint64) []uint64 {
	nonces := make([]uint64, 0, to-from)
	for n := from; n < to; n++ {
		nonces = append(nonces, n)
	}
	return nonces
}

func TestTxPoolContentReadsPastOnePage(t *testing.T) {
	sender, txs := signedEVMTxs(t, 250)
	c := &pagedMempoolClient{pools: [][]tmtypes.Tx{txs}}

	content, err := newPagedTxPoolAPI(c, 1000).Content(context.Background())
	require.NoError(t, err)
	require.Equal(t, nonceRange(0, 250), pendingNonces(t, content, sender))
	require.Equal(t, []int{1, 2, 3}, c.pages)
}

func TestTxPoolContentStopsAtMaxTxs(t *testing.T) {
	sender, txs := signedEVMTxs(t, 250)
	c := &pagedMempoolClient{pools: [][]tmtypes.Tx{txs}}

	content, err := newPagedTxPoolAPI(c, 150).Content(context.Background())
	require.NoError(t, err)
	require.Equal(t, nonceRange(0, 150), pendingNonces(t, content, sender))
	require.Equal(t, []int{1, 2}, c.pages)
}

func TestTxPoolContentSkipsTxRepeatedAcrossPages(t *testing.T) {
	sender, txs := signedEVMTxs(t, 200)
	_, arrived := signedEVMTxs(t, 1)
	// A tx arriving at the front between pages moves nonce 99 onto page 2.
	shifted := append(slices.Clone(arrived), txs...)
	c := &pagedMempoolClient{pools: [][]tmtypes.Tx{txs, shifted}}

	// Counting nonce 99 twice would fill the cap before nonce 199 is read.
	content, err := newPagedTxPoolAPI(c, 200).Content(context.Background())
	require.NoError(t, err)
	require.Equal(t, nonceRange(0, 200), pendingNonces(t, content, sender))
	require.Equal(t, []int{1, 2, 3}, c.pages)
}

func TestTxPoolContentStopsWhenPoolShrinks(t *testing.T) {
	sender, txs := signedEVMTxs(t, 250)
	c := &pagedMempoolClient{pools: [][]tmtypes.Tx{txs, txs, txs[:50]}}

	content, err := newPagedTxPoolAPI(c, 1000).Content(context.Background())
	require.NoError(t, err)
	require.Equal(t, nonceRange(0, 200), pendingNonces(t, content, sender))
	require.Equal(t, []int{1, 2, 3}, c.pages)
}

func TestTxPoolContentMixedCosmosAndEVMTxs(t *testing.T) {
	sender, evmTxs := signedEVMTxs(t, 120)
	cosmos := cosmosTxs(t, 120)
	var pool []tmtypes.Tx
	for i := range evmTxs {
		pool = append(pool, cosmos[i], evmTxs[i])
	}

	t.Run("lists only EVM txs", func(t *testing.T) {
		c := &pagedMempoolClient{pools: [][]tmtypes.Tx{pool}}
		content, err := newPagedTxPoolAPI(c, 1000).Content(context.Background())
		require.NoError(t, err)
		require.Len(t, content["pending"], 1)
		require.Equal(t, nonceRange(0, 120), pendingNonces(t, content, sender))
		require.Equal(t, []int{1, 2, 3}, c.pages)
	})

	t.Run("counts Cosmos txs against the cap", func(t *testing.T) {
		c := &pagedMempoolClient{pools: [][]tmtypes.Tx{pool}}
		content, err := newPagedTxPoolAPI(c, 100).Content(context.Background())
		require.NoError(t, err)
		require.Equal(t, nonceRange(0, 50), pendingNonces(t, content, sender))
		require.Equal(t, []int{1}, c.pages)
	})
}

func TestTxPoolContentSkipsTxWithUnrecoverableSender(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	unsigned := signEVMTx(t, key, 1)
	unsigned.R = []byte{}
	pool := []tmtypes.Tx{
		encodeEVMTx(t, signEVMTx(t, key, 0)),
		encodeEVMTx(t, unsigned),
		encodeEVMTx(t, signEVMTx(t, key, 2)),
	}
	c := &pagedMempoolClient{pools: [][]tmtypes.Tx{pool}}

	content, err := newPagedTxPoolAPI(c, 1000).Content(context.Background())
	require.NoError(t, err)
	require.Len(t, content["pending"], 1)
	require.Equal(t, []uint64{0, 2}, pendingNonces(t, content, sender))
}

func TestTxPoolContentReturnsMempoolError(t *testing.T) {
	c := &pagedMempoolClient{err: errors.New("mempool unavailable")}

	_, err := newPagedTxPoolAPI(c, 1000).Content(context.Background())
	require.ErrorContains(t, err, "mempool unavailable")
}
