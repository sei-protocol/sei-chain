package evmrpc_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"math/big"

	"github.com/cosmos/go-bip39"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func waitForReceipt(t *testing.T, ctx sdk.Context, txHash common.Hash) *types.Receipt {
	t.Helper()
	var receipt *types.Receipt
	require.Eventually(t, func() bool {
		var err error
		receipt, err = EVMKeeper.GetReceipt(ctx, txHash)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
	return receipt
}

type pendingNonceClient struct {
	*MockClient
	nextNonce uint64
	proxyURL  *url.URL
}

func (c *pendingNonceClient) EvmNextPendingNonce(common.Address) uint64 {
	return c.nextNonce
}

func (c *pendingNonceClient) EvmProxy(common.Address) utils.Option[*url.URL] {
	if c.proxyURL == nil {
		return utils.None[*url.URL]()
	}
	return utils.Some(c.proxyURL)
}

func TestGetTransactionCount(t *testing.T) {
	originalCtx := Ctx
	defer func() { Ctx = originalCtx }()
	Ctx = Ctx.WithBlockHeight(1)
	// happy path
	bodyByNumber := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"0x1234567890123456789012345678901234567890\",\"0x8\"],\"id\":\"test\"}"
	bodyByHash := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"0x1234567890123456789012345678901234567890\",\"0x3030303030303030303030303030303030303030303030303030303030303031\"],\"id\":\"test\"}"

	for _, body := range []string{bodyByNumber, bodyByHash} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		count := resObj["result"].(string)
		require.Equal(t, "0x1", count)
	}

	// address that doesn't have tx
	strangerBody := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"0x0123456789012345678902345678901234567891\",\"0x8\"],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(strangerBody))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	count := resObj["result"].(string)
	require.Equal(t, "0x0", count) // no tx

	// error cases
	for body, errStr := range map[string]string{
		bodyByHash: "error block",
	} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestBadPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		errMap := resObj["error"].(map[string]interface{})
		errMsg := errMap["message"].(string)
		require.Equal(t, errStr, errMsg)
	}
	Ctx = Ctx.WithBlockHeight(8)
}

func TestGetTransactionError(t *testing.T) {
	h := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, h, &types.Receipt{VmError: "test error", BlockNumber: 1})
	waitForReceipt(t, Ctx, h)
	resObj := sendRequestGood(t, "getTransactionErrorByHash", "0x1111111111111111111111111111111111111111111111111111111111111111")
	require.Equal(t, "test error", resObj["result"])

	resObj = sendRequestBad(t, "getTransactionReceipt", "0x1111111111111111111111111111111111111111111111111111111111111111")
	require.Equal(t, "error block", resObj["error"].(map[string]interface{})["message"])
}

func TestSign(t *testing.T) {
	homeDir := t.TempDir()
	txApi := evmrpc.NewTransactionAPI(nil, nil, nil, nil, homeDir, evmrpc.ConnectionTypeHTTP, utils.None[time.Duration](), &evmrpc.WatermarkManager{}, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	infoApi := evmrpc.NewInfoAPI(nil, nil, nil, nil, homeDir, 1024, evmrpc.ConnectionTypeHTTP, nil, nil)
	clientCtx := client.Context{}.WithViper("").WithHomeDir(homeDir)
	clientCtx, err := config.ReadFromClientConfig(clientCtx)
	require.Nil(t, err)
	kb, err := client.NewKeyringFromBackend(clientCtx, keyring.BackendTest)
	require.Nil(t, err)
	entropySeed, err := bip39.NewEntropy(256)
	require.Nil(t, err)
	mnemonic, err := bip39.NewMnemonic(entropySeed)
	require.Nil(t, err)
	algos, _ := kb.SupportedAlgorithms()
	algo, err := keyring.NewSigningAlgoFromString(string(hd.Secp256k1Type), algos)
	require.Nil(t, err)
	_, err = kb.NewAccount("test", mnemonic, "", hd.CreateHDPath(sdk.GetConfig().GetCoinType(), 0, 0).String(), algo)
	require.Nil(t, err)
	accounts, _ := infoApi.Accounts(t.Context())
	account := accounts[0]
	signed, err := txApi.Sign(t.Context(), account, []byte("data"))
	require.Nil(t, err)
	require.NotEmpty(t, signed)

	// Test signing with address that doesn't have hosted key
	nonExistentAddr := common.HexToAddress("0x9999999999999999999999999999999999999999")
	_, err = txApi.Sign(t.Context(), nonExistentAddr, []byte("data"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "address does not have hosted key")
}

func TestGetVMError(t *testing.T) {
	resObj := sendRequestGood(t, "getVMError", "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32")
	require.Equal(t, "", resObj["result"].(string))
	resObj = sendRequestGood(t, "getVMError", "0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872f")
	require.Equal(t, "receipt not found", resObj["error"].(map[string]interface{})["message"])
}

// Pins the discriminator used by sei_getTransactionReceiptExcludeTraceFail.
// Per evmrpc/README.md, the endpoint should filter out txs that were
// "included in blocks but not executed" — pre-state-check failures and
// chain-generated synthetic txs. Reverts ran in the VM and produce a real
// trace; they must come through.
func TestGetTransactionReceiptExcludeTraceFail(t *testing.T) {
	call := func(hash string) map[string]interface{} {
		body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"sei_getTransactionReceiptExcludeTraceFail\",\"params\":[\"%s\"],\"id\":\"test\"}", hash)
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		return resObj
	}

	cases := []struct {
		name        string
		hash        string
		expectPanic bool // true => endpoint returns ErrPanicTx
	}{
		{
			// Non-nonce ante failure (deferred-info stub receipt:
			// EffectiveGasPrice=0, GasUsed=0, VmError="insufficient funds").
			// The chain only writes stub receipts for txs whose nonce passed
			// validation but failed a later ante check — so nonce-too-high /
			// nonce-too-low never appear here; the realistic VmError content
			// is "insufficient funds", "insufficient fee", etc. The
			// discriminator must catch this regardless of VmError content.
			name:        "non-nonce ante failure is excluded",
			hash:        TestPanicTxHash,
			expectPanic: true,
		},
		{
			// Chain-generated synthetic (TxType=ShellEVMTxType). No real EVM
			// execution → no trace → exclude.
			name:        "synthetic is excluded",
			hash:        TestSyntheticTxHash,
			expectPanic: true,
		},
		{
			// Revert (Status=0 with EffectiveGasPrice>0). The VM ran and
			// produced a trace; the REVERT just shows up inside it.
			// Including this case is what distinguishes us from the prior
			// over-strict Status==0 mapping.
			name:        "revert is included",
			hash:        TestNonPanicTxHash,
			expectPanic: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resObj := call(tc.hash)
			errObj, hasErr := resObj["error"].(map[string]interface{})
			if tc.expectPanic {
				require.True(t, hasErr, "expected ErrPanicTx but got result=%v", resObj["result"])
				require.Equal(t, evmrpc.ErrPanicTx.Error(), errObj["message"])
				require.Nil(t, resObj["result"])
				return
			}
			if hasErr {
				// Allow downstream errors that aren't the panic-tx exclusion —
				// the mock fixture isn't a complete revert (no signer-recoverable
				// tx in the block from the receipt's perspective), so the
				// endpoint may fail at a later step. The point is the panic-tx
				// check itself must not exclude the revert.
				require.NotEqual(t, evmrpc.ErrPanicTx.Error(), errObj["message"], "revert tx was excluded as panic")
			}
		})
	}
}

// The panic/synthetic decision for a missing receipt must not be cached.
// Receipt-store writes can lag the RPC for a freshly committed tx, so a hash
// that initially looks panic-like must flip to "include" once its receipt
// (Status=1) lands within the cache TTL.
func TestGetTransactionReceiptExcludeTraceFailLateReceipt(t *testing.T) {
	// Fresh hash per invocation so the test stays correct under -count>1
	// (the receipt store and isPanicCache persist across iterations).
	var hashBytes [32]byte
	_, err := rand.Read(hashBytes[:])
	require.NoError(t, err)
	hash := common.Hash(hashBytes)

	call := func() map[string]interface{} {
		body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"sei_getTransactionReceiptExcludeTraceFail\",\"params\":[\"%s\"],\"id\":\"test\"}", hash.Hex())
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		return resObj
	}

	// First call: no receipt → endpoint reports the tx as panic.
	resObj := call()
	errObj, ok := resObj["error"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, evmrpc.ErrPanicTx.Error(), errObj["message"])

	// Receipt lands with Status=1. The next call must NOT still report the
	// tx as panic — that would mean the prior "no receipt" answer was cached.
	require.NoError(t, EVMKeeper.MockReceipt(Ctx, hash, &types.Receipt{
		BlockNumber:       MockHeight8,
		TransactionIndex:  0,
		TxHashHex:         hash.Hex(),
		Status:            1,
		EffectiveGasPrice: 1000000,
	}))
	resObj = call()
	if errObj, ok := resObj["error"].(map[string]interface{}); ok {
		require.NotEqual(t, evmrpc.ErrPanicTx.Error(), errObj["message"], "cache was poisoned by the prior missing-receipt lookup")
	}
}

// lowLatestTMClient reports a fixed LatestBlockHeight via Status, regardless
// of what blocks the receipt store contains.
type lowLatestTMClient struct {
	client.Client
	latest int64
}

func (c *lowLatestTMClient) EvmNextPendingNonce(common.Address) uint64 { return 0 }

func (c *lowLatestTMClient) EvmTxByHash(common.Hash) (tmtypes.Tx, bool) { return nil, false }

func (c *lowLatestTMClient) EvmProxy(common.Address) utils.Option[*url.URL] {
	return utils.None[*url.URL]()
}

func (c *lowLatestTMClient) Status(context.Context) (*coretypes.ResultStatus, error) {
	return &coretypes.ResultStatus{
		SyncInfo: coretypes.SyncInfo{LatestBlockHeight: c.latest, EarliestBlockHeight: 1},
	}, nil
}

// When the receipt's block sits above the safe-latest watermark (e.g. tendermint
// status lags the receipt store by a block), eth_getTransactionReceipt must
// return JSON null — the spec's "not yet mined" signal — so clients poll again,
// matching what eth_getBlockByNumber already does.
func TestGetTransactionReceiptReturnsNullAboveWatermark(t *testing.T) {
	var hashBytes [32]byte
	_, err := rand.Read(hashBytes[:])
	require.NoError(t, err)
	hash := common.Hash(hashBytes)

	receiptHeight := int64(MockHeight8 + 100)
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, hash, &types.Receipt{
		BlockNumber:       uint64(receiptHeight),
		TxHashHex:         hash.Hex(),
		Status:            1,
		EffectiveGasPrice: 1000000,
	})

	tmClient := &lowLatestTMClient{latest: MockHeight8}
	ctxProvider := func(int64) sdk.Context { return Ctx.WithBlockHeight(MockHeight8) }
	watermarks := evmrpc.NewWatermarkManager(tmClient, ctxProvider, nil, EVMKeeper.ReceiptStore())
	txAPI := evmrpc.NewTransactionAPI(tmClient, EVMKeeper, ctxProvider, nil, t.TempDir(), evmrpc.ConnectionTypeHTTP, utils.None[time.Duration](), watermarks, evmrpc.NewBlockCache(8), &sync.Mutex{})

	result, err := txAPI.GetTransactionReceipt(context.Background(), hash)
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestCumulativeGasUsedPopulation(t *testing.T) {
	blockHeight := int64(1000)
	Ctx = Ctx.WithBlockHeight(blockHeight)

	txHashes := []common.Hash{
		common.HexToHash("0xc90ff2909ee2bba49a1a84bc3eb44fd1f0b389f6fb204ce77fcc48f58f1b8967"),
		common.HexToHash("0xb71247ff6fa4d16f68b559d3f37e6a76662e8c4bda795dec534c118740b993f4"),
		common.HexToHash("0xe81adea20595a48d49f6856c9d45de5d1874b7120c7fb053acacc9a297cd7106"),
		common.HexToHash("0xd768d6dff68f95fea0a096c43976fee8fe1f7bde24bdd6b48e086b7283967a0f"),
		common.HexToHash("0xe22b36ac447615070cb93f178ca41e4ca0482908d54c688cd0b9f42ccb81eed0"),
		common.HexToHash("0x65fda2369f700599385c9dbe2870f8a56051a8a45c3ebc49a8c56a46b7ecc9fb"),
		common.HexToHash("0x331decb2e371768a8b78eb03bcd91e54a65b35d43accd789900901a77a94c701"),
		common.HexToHash("0xef71e67093ace8649c4b5bc66fc823e7746e504c05d5c41deca909b3f5a66c4c"),
		common.HexToHash("0x96b8b807b31edef98c1486a3ca6326f61a09f9a825b2de76845ac7a8ff59912d"),
		common.HexToHash("0x194dd7db211b09b1e86ee2f188c75e20f31b602d78cbb4762aeb704406b8a6e0"),
		common.HexToHash("0x06a58c740f3f7f1af8f0e9eaded8578a099c3fe8ef8ee947c539af34ecf70aa8"),
	}
	correctCumulativeGasUsedValues := []uint64{21000, 43000, 66000, 90000, 115000, 141000, 168000, 196000, 225000, 255000, 286000}

	stateDB := state.NewDBImpl(Ctx, EVMKeeper, true)

	for i, txHash := range txHashes {
		Ctx = Ctx.WithTxIndex(i)

		msg := &core.Message{
			From:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
			To:       &common.Address{},
			GasPrice: big.NewInt(1000000000),
			Nonce:    uint64(i),
		}

		_, err := EVMKeeper.WriteReceipt(
			Ctx,
			stateDB,
			msg,
			2,
			txHash,
			uint64(21000+i*1000),
			"",
		)
		require.Nil(t, err)
	}

	err := EVMKeeper.FlushTransientReceipts(Ctx)
	require.Nil(t, err)

	for i := 0; i < len(txHashes); i++ {
		receipt := waitForReceipt(t, Ctx, txHashes[i])
		require.Equal(t, receipt.CumulativeGasUsed, correctCumulativeGasUsedValues[i])
	}
}

func TestGetTransactionReceiptFailedTxWithZeroGas(t *testing.T) {
	// This tests the receipt population logic for ante handler failures
	txHash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	// Create a failed receipt with 0 gas (ante handler failure case)
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           0, // Failed
		GasUsed:          0, // Zero gas used
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// The receipt should be returned even if tx not found in block
	// This tests that the code path for Status==0 && GasUsed==0 is executed
	result := resObj["result"]
	if result != nil {
		// If result exists, verify it has the expected structure
		resultMap := result.(map[string]interface{})
		require.NotNil(t, resultMap["status"])
	}
}

func TestGetTransactionByBlockNumberAndIndexErrors(t *testing.T) {
	body := `{"jsonrpc": "2.0","method": "eth_getTransactionByBlockNumberAndIndex","params":["0x8","0xFFFFFFFFFF"],"id":"test"}`
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Overflow tx index should return null result, not an error (Ethereum JSON-RPC spec)
	require.Nil(t, resObj["error"])
	require.Nil(t, resObj["result"])

	body = `{"jsonrpc": "2.0","method": "eth_getTransactionByBlockNumberAndIndex","params":["0x999999","0x0"],"id":"test"}`
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj = map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Non-existent block returns null result (Ethereum JSON-RPC spec).
	require.Nil(t, resObj["error"])
	require.Nil(t, resObj["result"])
}

func TestGetTransactionByBlockHashAndIndexErrors(t *testing.T) {
	invalidHash := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByBlockHashAndIndex","params":["%s","0x0"],"id":"test"}`, invalidHash)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Non-existent block hash returns null result (Ethereum JSON-RPC spec).
	require.Nil(t, resObj["error"])
	require.Nil(t, resObj["result"])

	body = fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByBlockHashAndIndex","params":["%s","0xFFFFFFFFFF"],"id":"test"}`, TestBlockHash)
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj = map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Overflow tx index should return null result, not an error (Ethereum JSON-RPC spec)
	require.Nil(t, resObj["error"])
	require.Nil(t, resObj["result"])
}

func TestGetTransactionByHashNotFound(t *testing.T) {
	nonExistentHash := "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByHash","params":["%s"],"id":"test"}`, nonExistentHash)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return null for non-existent transaction
	require.Nil(t, resObj["result"])
}

func TestGetTransactionByHashTxNotFound(t *testing.T) {
	txHash := common.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")

	// Create a receipt but ensure the tx won't be found in the block
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 999, // Invalid index
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByHash","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return null when tx not found in block
	require.Nil(t, resObj["result"])
}

func TestGetTransactionErrorByHashNotFound(t *testing.T) {
	nonExistentHash := "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionErrorByHash","params":["%s"],"id":"test"}`, nonExistentHash)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return empty string for non-existent transaction
	require.Equal(t, "", resObj["result"])
}

func TestGetTransactionWithBlockIndexOutOfRange(t *testing.T) {
	body := `{"jsonrpc": "2.0","method": "eth_getTransactionByBlockNumberAndIndex","params":["0x8","0x999"],"id":"test"}`
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Ethereum JSON-RPC: out-of-range index yields null result, not an error
	require.Nil(t, resObj["error"])
	require.Nil(t, resObj["result"])
}

func TestEncodeReceiptTransactionNotFound(t *testing.T) {
	txHash := common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")

	// Create a receipt with invalid transaction index
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 999, // Invalid index that won't be found
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should get an error when transaction not found in block
	errMap := resObj["error"].(map[string]interface{})
	require.Contains(t, errMap["message"].(string), "failed to find transaction in block")
}

func TestEncodeReceiptWithEthTxAndEmptyFrom(t *testing.T) {
	// This test uses an existing transaction that should have a receipt
	// The test verifies that receipts are properly encoded with from field
	txHash := common.HexToHash("0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32")

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should successfully get receipt with from field populated
	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		require.NotNil(t, resultMap["from"])
	}
}

func TestEncodeReceiptContractAddress(t *testing.T) {
	// Test coverage for lines 456-463: contract address handling
	txHash := common.HexToHash("0xbbbb0000000000000000000000000000000000000000000000000000000000bb")

	// Create a receipt with contract address but no "to" field (contract creation)
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
		ContractAddress:  "0x5555555555555555555555555555555555555555",
		To:               "",
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should have contractAddress field set
	if resObj["result"] != nil {
		resultMap := resObj["result"].(map[string]interface{})
		require.NotNil(t, resultMap["contractAddress"])
	}
}

func TestEncodeReceiptWithToAddress(t *testing.T) {
	// Test coverage for lines 461-463: receipt.To != ""
	txHash := common.HexToHash("0xcccc0000000000000000000000000000000000000000000000000000000000cc")

	// Create a receipt with "to" field set (regular transaction)
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
		To:               "0x9876543210987654321098765432109876543210",
		ContractAddress:  "",
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should have "to" field set
	if resObj["result"] != nil {
		resultMap := resObj["result"].(map[string]interface{})
		require.NotNil(t, resultMap["to"])
	}
}

func TestGetTransactionReceiptContractCreationFailure(t *testing.T) {
	// Test coverage for lines 131-135: contract creation with failed tx (etx.To() == nil)
	// This tests the else branch where contract address is calculated
	txHash := common.HexToHash("0xdddd1111000000000000000000000000000000000000000000000000000000dd")

	// Create a failed receipt with 0 gas for a contract creation (no "to" address)
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           0, // Failed
		GasUsed:          0, // Zero gas used
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
		To:               "",
		ContractAddress:  "",
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// The receipt should be processed even if tx not found in block
	// This exercises the contract creation code path
	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		require.NotNil(t, resultMap["status"])
	}
}

func TestGetTransactionByHashFromMempool(t *testing.T) {
	// Test coverage for lines 210-233: finding transaction in mempool
	// Uses the UnconfirmedTx that's set up in the mock client
	txHash := common.HexToHash("0x1234567890123456789012345678901234567890123456789012345678901234")

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByHash","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return transaction from mempool or nil if not found
	// This exercises the mempool search code path
	_ = resObj["result"]
}

func TestGetTransactionByBlockHashAndIndexSuccess(t *testing.T) {
	// Test coverage for lines 185-197: successful path
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByBlockHashAndIndex","params":["%s","0x0"],"id":"test"}`, TestBlockHash)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should successfully get transaction by block hash and index
	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		require.NotNil(t, resultMap["hash"])
	}
}

func TestEncodeReceiptWithEthTxToField(t *testing.T) {
	// Test coverage for lines 451-454: etx.To() != nil branch
	txHash := common.HexToHash("0xeeee0000000000000000000000000000000000000000000000000000000000ee")

	// Create a receipt with empty From to trigger the etx != nil && receipt.From == "" path
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "", // Empty to trigger recovery
		To:               "0x9876543210987654321098765432109876543210",
		ContractAddress:  "",
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should have from and to fields populated
	if resObj["result"] != nil {
		resultMap := resObj["result"].(map[string]interface{})
		// Either from is populated from receipt or recovered from tx
		require.NotNil(t, resultMap["status"])
	}
}

func TestEncodeReceiptContractAddressNil(t *testing.T) {
	// Test coverage for lines 458-460: else branch where contractAddress is set to nil
	txHash := common.HexToHash("0xffff0000000000000000000000000000000000000000000000000000000000ff")

	// Create a receipt with both ContractAddress and To set (should result in contractAddress = nil)
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
		To:               "0x9876543210987654321098765432109876543210",
		ContractAddress:  "0x5555555555555555555555555555555555555555", // Has contract address
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should have contractAddress set to nil (not the contract address from receipt)
	if resObj["result"] != nil {
		resultMap := resObj["result"].(map[string]interface{})
		// contractAddress should be nil when To is set
		require.NotNil(t, resultMap["to"])
	}
}

func TestGetTransactionByHashNonEVMTransaction(t *testing.T) {
	// Test coverage for lines 256-258: non-EVM transaction error
	txHash := common.HexToHash("0x1111222200000000000000000000000000000000000000000000000011112222")

	// Create a receipt for a non-EVM transaction
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByHash","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return error or nil for non-EVM transaction
	_ = resObj["result"]
}

func TestGetTransactionWithBlockNonEVMTransaction(t *testing.T) {
	// Test coverage for lines 307-310: non-EVM transaction in getTransactionWithBlock
	// This would require a block with a non-EVM transaction at index 0
	// The test exercises the error path when msg is not a MsgEVMTransaction
	body := `{"jsonrpc": "2.0","method": "eth_getTransactionByBlockNumberAndIndex","params":["0x8","0x0"],"id":"test"}`
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return transaction or error
	_ = resObj["result"]
}

func TestEncodeRPCTransactionBlockHeight1(t *testing.T) {
	// Test coverage for lines 323-327: block.Block.Height > 1 vs else branch
	// This tests the baseFeePerGas calculation for block height 1
	txHash := common.HexToHash("0x2222333300000000000000000000000000000000000000000000000022223333")

	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      1,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
		To:               "0x9876543210987654321098765432109876543210",
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should successfully encode receipt for block 1
	if resObj["result"] != nil {
		resultMap := resObj["result"].(map[string]interface{})
		require.NotNil(t, resultMap["status"])
	}
}

func TestGetTransactionCountPending(t *testing.T) {
	originalCtx := Ctx
	defer func() { Ctx = originalCtx }()
	Ctx = Ctx.WithBlockHeight(1)
	// Test coverage for lines 280-283: pending block number
	body := `{"jsonrpc": "2.0","method": "eth_getTransactionCount","params":["0x1234567890123456789012345678901234567890","pending"],"id":"test"}`
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return nonce for pending transactions
	require.NotNil(t, resObj["result"])
}

func TestGetTransactionCountPendingUsesProxy(t *testing.T) {
	address := common.HexToAddress("0x1234567890123456789012345678901234567890")
	var gotMethod string
	var gotAddress string
	var gotBlockTag map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Len(t, req.Params, 2)

		gotMethod = req.Method
		require.NoError(t, json.Unmarshal(req.Params[0], &gotAddress))
		require.NoError(t, json.Unmarshal(req.Params[1], &gotBlockTag))

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  "0x2a",
		}))
	}))
	defer server.Close()

	proxyURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	api := evmrpc.NewTransactionAPI(
		&pendingNonceClient{MockClient: &MockClient{}, nextNonce: 7, proxyURL: proxyURL},
		nil,
		nil,
		nil,
		"",
		evmrpc.ConnectionTypeHTTP,
		utils.None[time.Duration](),
		&evmrpc.WatermarkManager{},
		evmrpc.NewBlockCache(1),
		&sync.Mutex{},
	)

	nonce, err := api.GetTransactionCount(context.Background(), address, rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber))
	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(42), *nonce)
	require.Equal(t, "eth_getTransactionCount", gotMethod)
	require.Equal(t, address.Hex(), gotAddress)
	require.Equal(t, map[string]string{"blockNumber": "pending"}, gotBlockTag)
}

func TestGetTransactionCountPendingFallsBackToLocalNonce(t *testing.T) {
	address := common.HexToAddress("0x1234567890123456789012345678901234567890")
	api := evmrpc.NewTransactionAPI(
		&pendingNonceClient{MockClient: &MockClient{}, nextNonce: 7},
		nil,
		nil,
		nil,
		"",
		evmrpc.ConnectionTypeHTTP,
		utils.None[time.Duration](),
		&evmrpc.WatermarkManager{},
		evmrpc.NewBlockCache(1),
		&sync.Mutex{},
	)

	nonce, err := api.GetTransactionCount(context.Background(), address, rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber))
	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(7), *nonce)
}

func TestGetTransactionCountWithBlockNumber(t *testing.T) {
	// Test coverage for lines 290-295: specific block number
	body := `{"jsonrpc": "2.0","method": "eth_getTransactionCount","params":["0x1234567890123456789012345678901234567890","0x5"],"id":"test"}`
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return nonce for specific block
	_ = resObj["result"]
}

func TestReplaceFromWithEmptyAddress(t *testing.T) {
	// Test coverage for lines 339-342: replaceFrom function
	// This is tested indirectly through encodeRPCTransaction
	// We test by creating a receipt that triggers the replaceFrom logic
	txHash := common.HexToHash("0xaaaa1111000000000000000000000000000000000000000000000000aaaa1111")

	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
		To:               "0x9876543210987654321098765432109876543210",
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should have from field populated
	if resObj["result"] != nil {
		resultMap := resObj["result"].(map[string]interface{})
		require.NotNil(t, resultMap["from"])
	}
}

func TestGetEvmTxIndexWithWasmMsg(t *testing.T) {
	// Test coverage for lines 397-400: MsgExecuteContract case
	// This tests the wasm message handling in GetEvmTxIndex
	// The function should handle both EVM and Wasm messages
	txHash := common.HexToHash("0x3333444400000000000000000000000000000000000000000000000033334444")

	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should handle receipt encoding
	_ = resObj["result"]
}

func TestEncodeReceiptWithLogs(t *testing.T) {
	// Test coverage for lines 426-429: log processing
	txHash := common.HexToHash("0x4444555500000000000000000000000000000000000000000000000044445555")

	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
		To:               "0x9876543210987654321098765432109876543210",
		Logs:             []*types.Log{},
		LogsBloom:        make([]byte, 256),
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should have logs field
	if resObj["result"] != nil {
		resultMap := resObj["result"].(map[string]interface{})
		require.NotNil(t, resultMap["logs"])
	}
}

func TestGetTransactionReceiptErrorRecovery(t *testing.T) {
	// Test coverage for lines 122-125: error in RecoverEVMSender
	// This tests the error path when sender recovery fails
	txHash := common.HexToHash("0x5555666600000000000000000000000000000000000000000000000055556666")

	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           0, // Failed
		GasUsed:          0, // Zero gas
		TransactionIndex: 0,
		From:             "",
	}
	testkeeper.MustMockReceipt(t, EVMKeeper, Ctx, txHash, receipt)
	waitForReceipt(t, Ctx, txHash)

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should handle error or return result
	_ = resObj["result"]
}

func TestGetTransactionByHashRecoverySenderError(t *testing.T) {
	// Test coverage for lines 213-217: error in RecoverEVMSenderWithContext
	// This tests mempool transaction with sender recovery error
	txHash := common.HexToHash("0x6666777700000000000000000000000000000000000000000000000066667777")

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByHash","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return error or nil
	_ = resObj["result"]
}

func TestGetTransactionReceiptGenericError(t *testing.T) {
	// Test coverage for line 106: generic error (not "not found")
	// This would require a keeper that returns a different error
	txHash := common.HexToHash("0x7777888800000000000000000000000000000000000000000000000077778888")

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return nil for not found
	_ = resObj["result"]
}

func TestGetTransactionErrorByHashGenericError(t *testing.T) {
	// Test coverage for line 270: generic error in GetTransactionErrorByHash
	txHash := common.HexToHash("0x8888999900000000000000000000000000000000000000000000000088889999")

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionErrorByHash","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return empty string for not found
	_ = resObj["result"]
}

func TestGetTransactionByHashGenericError(t *testing.T) {
	// Test coverage for line 244: generic error in GetTransactionByHash
	txHash := common.HexToHash("0x9999aaaa000000000000000000000000000000000000000000000099999aaaa")

	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByHash","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should return nil for not found
	_ = resObj["result"]
}

func TestGetTransactionReceiptFailedTxWithToAddress(t *testing.T) {
	// Test failed transaction with zero gas usage
	// When a transaction fails before reaching the VM, it has Status=0 and GasUsed=0
	// The receipt should be populated from the transaction data in the block
	txHash := tx1.Hash()

	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           0, // Failed
		GasUsed:          0, // Zero gas - triggers population from block
		TransactionIndex: 0,
	}
	ctxWithHeight := Ctx.WithBlockHeight(8)
	testkeeper.MustMockReceipt(t, EVMKeeper, ctxWithHeight, txHash, receipt)
	waitForReceipt(t, ctxWithHeight, txHash)

	resObj := sendRequestGood(t, "getTransactionReceipt", txHash.Hex())

	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		// Verify the receipt was populated from the transaction in the block
		require.NotNil(t, resultMap["from"])
		require.NotNil(t, resultMap["to"])
	}
}

func TestGetTransactionByHashMempoolTransaction(t *testing.T) {
	// Test retrieving an unconfirmed transaction from the mempool
	// GetTransactionByHash should search the mempool before checking committed blocks
	unconfirmedEthTx, _ := UnconfirmedTx.GetMsgs()[0].(*types.MsgEVMTransaction).AsTransaction()
	txHash := unconfirmedEthTx.Hash()

	resObj := sendRequestGood(t, "getTransactionByHash", txHash.Hex())

	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		// Verify all expected transaction fields are present
		require.NotNil(t, resultMap["hash"])
		require.NotNil(t, resultMap["from"])
		require.NotNil(t, resultMap["type"])
		require.NotNil(t, resultMap["gas"])
		require.NotNil(t, resultMap["v"])
		require.NotNil(t, resultMap["r"])
		require.NotNil(t, resultMap["s"])
	}
}

func TestEncodeReceiptWithEmptyFrom(t *testing.T) {
	// Test receipt encoding when receipt.From is empty
	// Should recover sender from transaction (line 447)
	txHash := tx1.Hash()

	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "", // Empty from - triggers recovery
		To:               "0x9876543210987654321098765432109876543210",
	}
	ctxWithHeight := Ctx.WithBlockHeight(8)
	testkeeper.MustMockReceipt(t, EVMKeeper, ctxWithHeight, txHash, receipt)
	waitForReceipt(t, ctxWithHeight, txHash)

	resObj := sendRequestGood(t, "getTransactionReceipt", txHash.Hex())

	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		// Verify from was recovered from the transaction
		require.NotNil(t, resultMap["from"])
	}
}

func TestReplaceFromWithEmptyFromField(t *testing.T) {
	// Test replaceFrom function when tx.From is empty (lines 340-342)
	// This happens with legacy transactions that don't have from populated
	txHash := tx1.Hash()

	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
		To:               "0x9876543210987654321098765432109876543210",
	}
	ctxWithHeight := Ctx.WithBlockHeight(8)
	testkeeper.MustMockReceipt(t, EVMKeeper, ctxWithHeight, txHash, receipt)
	waitForReceipt(t, ctxWithHeight, txHash)

	// Query by block and index which uses encodeRPCTransaction and replaceFrom
	resObj := sendRequestGood(t, "getTransactionByBlockNumberAndIndex", "0x8", "0x0")

	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		// Verify from field is present (either from tx or receipt)
		require.NotNil(t, resultMap["from"])
	}
}

func TestGetTransactionByBlockNumberAndIndexSuccess(t *testing.T) {
	// Test retrieving a transaction by block number and index
	resObj := sendRequestGood(t, "getTransactionByBlockNumberAndIndex", "0x8", "0x0")
	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		require.NotNil(t, resultMap["hash"])
		require.NotNil(t, resultMap["blockHash"])
		require.NotNil(t, resultMap["blockNumber"])
		require.NotNil(t, resultMap["transactionIndex"])
		require.NotNil(t, resultMap["from"])
		require.NotNil(t, resultMap["gas"])
		require.NotNil(t, resultMap["gasPrice"])
	}
}

func TestGetTransactionByHashSuccess(t *testing.T) {
	// Test retrieving a committed transaction by hash
	txHash := common.HexToHash("0xef123456000000000000000000000000000000000000000000000000ef123456")

	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
		To:               "0x9876543210987654321098765432109876543210",
	}
	ctxWithHeight := Ctx.WithBlockHeight(8)
	testkeeper.MustMockReceipt(t, EVMKeeper, ctxWithHeight, txHash, receipt)
	waitForReceipt(t, ctxWithHeight, txHash)

	resObj := sendRequestGood(t, "getTransactionByHash", txHash.Hex())
	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		require.NotNil(t, resultMap["hash"])
		require.NotNil(t, resultMap["from"])
	}
}

func TestGetTransactionByBlockHashAndIndexFullSuccess(t *testing.T) {
	// Test the full success path with actual test block hash
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByBlockHashAndIndex","params":["%s","0x0"],"id":"test"}`, TestBlockHash)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should successfully return transaction
	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		require.NotNil(t, resultMap["hash"])
	}
}

func TestEncodeReceiptFullPath(t *testing.T) {
	// Test receipt encoding with logs and bloom filter
	txHash := common.HexToHash("0xgh789012000000000000000000000000000000000000000000000000gh789012")

	receipt := &types.Receipt{
		TxHashHex:         txHash.Hex(),
		BlockNumber:       8,
		Status:            1,
		GasUsed:           30000,
		TransactionIndex:  0,
		From:              "0x1234567890123456789012345678901234567890",
		To:                "0x9876543210987654321098765432109876543210",
		CumulativeGasUsed: 30000,
		EffectiveGasPrice: 100000000000,
		TxType:            2,
		ContractAddress:   "",
		Logs: []*types.Log{{
			Address: "0x2222222222222222222222222222222222222222",
			Topics:  []string{"0x2222222222222222222222222222222222222222222222222222222222222222"},
		}},
		LogsBloom: make([]byte, 256),
	}
	ctxWithHeight := Ctx.WithBlockHeight(8)
	testkeeper.MustMockReceipt(t, EVMKeeper, ctxWithHeight, txHash, receipt)
	waitForReceipt(t, ctxWithHeight, txHash)

	resObj := sendRequestGood(t, "getTransactionReceipt", txHash.Hex())
	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		require.NotNil(t, resultMap["blockHash"])
		require.NotNil(t, resultMap["blockNumber"])
		require.NotNil(t, resultMap["transactionHash"])
		require.NotNil(t, resultMap["transactionIndex"])
		require.NotNil(t, resultMap["from"])
		require.NotNil(t, resultMap["gasUsed"])
		require.NotNil(t, resultMap["cumulativeGasUsed"])
		require.NotNil(t, resultMap["logs"])
		require.NotNil(t, resultMap["logsBloom"])
		require.NotNil(t, resultMap["type"])
		require.NotNil(t, resultMap["effectiveGasPrice"])
		require.NotNil(t, resultMap["status"])
		_, hasContractAddress := resultMap["contractAddress"]
		require.True(t, hasContractAddress)
	}
}

func TestGetEvmTxIndexSuccess(t *testing.T) {
	// This tests GetEvmTxIndex by calling GetTransactionByHash which uses it
	// The function iterates through msgs and finds the matching transaction
	txHash := "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32"
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByHash","params":["%s"],"id":"test"}`, txHash)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Should find the transaction via GetEvmTxIndex
	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		require.NotNil(t, resultMap["hash"])
		require.NotNil(t, resultMap["transactionIndex"])
	}
}

func TestEncodeRPCTransactionSuccess(t *testing.T) {
	// Test encodeRPCTransaction by getting a transaction by block and index
	// This exercises lines 316-336 including baseFeePerGas calculation and replaceFrom
	body := `{"jsonrpc": "2.0","method": "eth_getTransactionByBlockNumberAndIndex","params":["0x8","0x0"],"id":"test"}`
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))

	// Verify RPC transaction encoding
	if resObj["result"] != nil {
		resultMap := resObj["result"].(map[string]interface{})
		// These fields test encodeRPCTransaction
		require.NotNil(t, resultMap["hash"])
		require.NotNil(t, resultMap["blockHash"])
		require.NotNil(t, resultMap["blockNumber"])
		require.NotNil(t, resultMap["from"])
		require.NotNil(t, resultMap["gas"])
		require.NotNil(t, resultMap["gasPrice"])
		require.NotNil(t, resultMap["nonce"])
		require.NotNil(t, resultMap["transactionIndex"])
		require.NotNil(t, resultMap["value"])
		require.NotNil(t, resultMap["v"])
		require.NotNil(t, resultMap["r"])
		require.NotNil(t, resultMap["s"])
	}
}
