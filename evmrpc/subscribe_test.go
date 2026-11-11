package evmrpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestSubscribeNewHeads(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestGood(t, "subscribe", "newHeads")
	defer func() { done <- struct{}{} }()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	expectedKeys := []string{
		"parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot",
		"receiptsRoot", "logsBloom", "difficulty", "number", "gasLimit",
		"gasUsed", "timestamp", "milliTimestamp", "extraData", "mixHash", "nonce",
		"baseFeePerGas", "withdrawalsRoot", "blobGasUsed", "excessBlobGas",
		"parentBeaconBlockRoot", "hash",
	}
	inapplicableKeys := make(map[string]struct{})
	for _, key := range []string{
		"difficulty", "extraData", "nonce", "sha3Uncles", "mixHash",
		"excessBlobGas", "parentBeaconBlockRoot", "withdrawlsRoot",
		"withdrawalsRoot", "blobGasUsed",
	} {
		inapplicableKeys[key] = struct{}{}
	}
	var subscriptionId string
	allZerosRE := regexp.MustCompile(`^0+$`)
	var numbers []string
	for len(numbers) < len(NewHeadsMockHeights) {
		select {
		case resObj := <-recvCh:
			_, ok := resObj["error"]
			if ok {
				t.Fatal("Received error:", resObj["error"])
			}
			if subscriptionId == "" {
				// get subscriptionId from first message
				subscriptionId = resObj["result"].(string)
				// Reset timer now that subscription is confirmed, then trigger new heads
				timer.Reset(5 * time.Second)
				NewHeadsCalled <- struct{}{}
				continue
			}
			method := resObj["method"].(string)
			if method != "eth_subscription" {
				t.Fatal("Method is not eth_subscription")
			}
			paramMap := resObj["params"].(map[string]interface{})
			if paramMap["subscription"] != subscriptionId {
				t.Fatal("Subscription ID does not match")
			}
			resultMap := paramMap["result"].(map[string]interface{})
			// check all fields
			for _, key := range expectedKeys {
				if _, ok := resultMap[key]; !ok {
					t.Fatalf("%s is nil", key)
				}
				// check that applicable keys aren't all 0's
				if _, inapplicable := inapplicableKeys[key]; !inapplicable {
					got := fmt.Sprintf("%v", resultMap[key])
					if allZerosRE.MatchString(got) {
						t.Fatalf("%s must be non-zero (got %v)", key, resultMap[key])
					}
				}
			}
			// This path's header time is time.Now(), so only the relationship
			// between the two timestamp fields is assertable here.
			secs, err := hexutil.DecodeUint64(resultMap["timestamp"].(string))
			require.NoError(t, err)
			millis, err := hexutil.DecodeUint64(resultMap["milliTimestamp"].(string))
			require.NoError(t, err)
			require.Equal(t, secs, millis/1000, "milliTimestamp and timestamp describe the same instant")
			requireNewHeadMatchesBlock(t, resultMap)
			numbers = append(numbers, resultMap["number"].(string))
		case <-timer.C:
			t.Fatalf("received %d of %d newHeads events within 5 seconds", len(numbers), len(NewHeadsMockHeights))
		}
	}
	require.Equal(t, []string{fmt.Sprintf("0x%x", MockHeight8), fmt.Sprintf("0x%x", MockHeight2)}, numbers)
}

// requireNewHeadMatchesBlock asserts that a newHeads header reports the logsBloom, gasUsed and
// baseFeePerGas that eth_getBlockByNumber reports for the same block.
func requireNewHeadMatchesBlock(t *testing.T, header map[string]interface{}) {
	t.Helper()
	number := header["number"].(string)
	resObj := sendRequestGood(t, "getBlockByNumber", number, false)
	require.Nil(t, resObj["error"])
	block := resObj["result"].(map[string]interface{})
	for _, key := range []string{"logsBloom", "gasUsed", "baseFeePerGas"} {
		require.Equalf(t, block[key], header[key], "newHeads %s of block %s", key, number)
	}
	if number == fmt.Sprintf("0x%x", MockHeight2) {
		// Block 2 has receipts with logs, so the comparison above is not between two empty headers.
		require.NotEqual(t, hexutil.Encode(ethtypes.Bloom{}.Bytes()), header["logsBloom"])
		require.NotEqual(t, "0x0", header["gasUsed"])
	}
}

// TestSubscribeNewHeadsAutobahn exercises the in-process notifier path
// (used under Autobahn) end-to-end: a WS client subscribes to newHeads
// against the notifier-backed server, the test pushes events via
// NotifierForTest.OnBlockCommitted, and the WS subscriber must receive an
// eth_subscription notification carrying each encoded header.
func TestSubscribeNewHeadsAutobahn(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestAndListen(t, TestNotifierWSPort, "subscribe", "newHeads")
	defer func() { done <- struct{}{} }()

	hash := common.HexToHash("0x4242424242424242424242424242424242424242424242424242424242424242").Bytes()
	appHash := common.HexToHash("0x3131313131313131313131313131313131313131313131313131313131313131").Bytes()
	proposer := common.HexToAddress("0x9999999999999999999999999999999999999999").Bytes()
	// Fractional second on purpose; see TestEncodeCommittedBlock.
	ts := time.Unix(1_700_000_500, 250_000_000).UTC()
	// TxResults also carry the gas of Cosmos transactions and of EVM transactions that failed their ante
	// handler, neither of which a header's gasUsed counts.
	committed := []struct {
		height    int64
		txResults []*abci.ExecTxResult
	}{
		{height: MockHeight8, txResults: []*abci.ExecTxResult{{GasUsed: 21000}, {GasUsed: 50000}}},
		{height: MockHeight2, txResults: []*abci.ExecTxResult{{GasUsed: 40000}, {GasUsed: 21000}, {GasUsed: 40000}, {GasUsed: 30000}}},
	}

	expectedKeys := []string{
		"parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot",
		"receiptsRoot", "logsBloom", "difficulty", "number", "gasLimit",
		"gasUsed", "timestamp", "milliTimestamp", "extraData", "mixHash", "nonce",
		"baseFeePerGas", "withdrawalsRoot", "blobGasUsed", "excessBlobGas",
		"parentBeaconBlockRoot", "hash",
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	var subscriptionId string
	var numbers []string
	for len(numbers) < len(committed) {
		select {
		case resObj := <-recvCh:
			if errVal, ok := resObj["error"]; ok {
				t.Fatal("Received error:", errVal)
			}
			if subscriptionId == "" {
				subscriptionId = resObj["result"].(string)
				timer.Reset(5 * time.Second)
				for _, c := range committed {
					NotifierForTest.OnBlockCommitted(hash, &tmproto.Header{
						Height:          c.height,
						Time:            ts,
						ProposerAddress: proposer,
					}, &abci.ResponseFinalizeBlock{
						AppHash:   appHash,
						TxResults: c.txResults,
						// gasLimit is sourced from the SDK ConsensusParams in
						// the consumer goroutine, not from the response, so
						// ConsensusParamUpdates is intentionally omitted here.
					})
				}
				continue
			}
			require.Equal(t, "eth_subscription", resObj["method"])
			paramMap := resObj["params"].(map[string]interface{})
			require.Equal(t, subscriptionId, paramMap["subscription"])
			resultMap := paramMap["result"].(map[string]interface{})
			for _, k := range expectedKeys {
				_, ok := resultMap[k]
				require.Truef(t, ok, "missing key %q in header", k)
			}
			require.Equal(t, common.BytesToHash(hash).Hex(), resultMap["hash"])
			require.Equal(t, common.BytesToAddress(proposer).Hex(), resultMap["miner"])
			require.Equal(t, common.BytesToHash(appHash).Hex(), resultMap["stateRoot"])
			require.Equal(t, fmt.Sprintf("0x%x", ts.Unix()), resultMap["timestamp"])
			require.Equal(t, fmt.Sprintf("0x%x", ts.UnixMilli()), resultMap["milliTimestamp"])
			// gasLimit comes from the SDK ConsensusParams that the test
			// runtime sets; just assert it's a non-zero hex string.
			gasLimitStr, _ := resultMap["gasLimit"].(string)
			require.NotEmpty(t, gasLimitStr)
			require.NotEqual(t, "0x0", gasLimitStr)
			// Fields the Autobahn path does not surface must serialize to
			// the zero hash.
			zeroHash := (common.Hash{}).Hex()
			require.Equal(t, zeroHash, resultMap["parentHash"])
			require.Equal(t, zeroHash, resultMap["receiptsRoot"])
			require.Equal(t, zeroHash, resultMap["transactionsRoot"])
			requireNewHeadMatchesBlock(t, resultMap)
			numbers = append(numbers, resultMap["number"].(string))
		case <-timer.C:
			t.Fatalf("received %d of %d newHeads events within 5 seconds", len(numbers), len(committed))
		}
	}
	require.Equal(t, []string{fmt.Sprintf("0x%x", MockHeight8), fmt.Sprintf("0x%x", MockHeight2)}, numbers)
}

func TestSubscribeEmptyLogs(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestGood(t, "subscribe", "logs")
	defer func() { done <- struct{}{} }()

	timer := time.NewTimer(2 * time.Second)

	// just testing to see that we don't crash when no params are provided
	for {
		select {
		case <-recvCh:
			return
		case <-timer.C:
			t.Fatal("No message received within 5 seconds")
		}
	}
}

func TestSubscribeNewLogs(t *testing.T) {
	t.Skip()
	data := map[string]interface{}{
		"fromBlock": "0x0",
		"toBlock":   "latest",
		"address": []common.Address{
			common.HexToAddress("0x1111111111111111111111111111111111111111"),
		},
		"topics": [][]common.Hash{
			{
				common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
				common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111112"),
			},
		},
	}
	recvCh, done := sendWSRequestGood(t, "subscribe", "logs", data)
	defer func() { done <- struct{}{} }()

	receivedSubMsg := false
	receivedEvents := false
	timer := time.NewTimer(2 * time.Second)

	var subscriptionId string

	for {
		select {
		case resObj := <-recvCh:
			_, ok := resObj["error"]
			if ok {
				t.Fatal("Received error:", resObj["error"])
			}
			if !receivedSubMsg {
				// get subscriptionId from first message
				subscriptionId = resObj["result"].(string)
				receivedSubMsg = true
				continue
			}
			receivedEvents = true
			method := resObj["method"].(string)
			if method != "eth_subscription" {
				t.Fatal("Method is not eth_subscription")
			}
			paramMap := resObj["params"].(map[string]interface{})
			if paramMap["subscription"] != subscriptionId {
				t.Fatal("Subscription ID does not match")
			}
			resultMap := paramMap["result"].(map[string]interface{})
			if resultMap["address"] != "0x1111111111111111111111111111111111111111" && resultMap["address"] != "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48" {
				t.Fatalf("Unexpected address, got %v", resultMap["address"])
			}
			firstTopic := resultMap["topics"].([]interface{})[0].(string)
			if firstTopic != "0x1111111111111111111111111111111111111111111111111111111111111111" {
				t.Fatalf("Unexpected topic, got %v", firstTopic)
			}
		case <-timer.C:
			if !receivedSubMsg || !receivedEvents {
				t.Fatal("No message received within 5 seconds")
			}
			return
		}
	}
}

// dialLogsSubscriptions serves eth_subscribe("logs") from fetcher in-process and returns a client
// connected to it.
func dialLogsSubscriptions(t *testing.T, fetcher *evmrpc.LogFetcher) *rpc.Client {
	t.Helper()
	server := rpc.NewServer()
	require.NoError(t, server.RegisterName("eth", evmrpc.NewLogsSubscriptionAPIForTest(fetcher, 10)))
	client := rpc.DialInProc(server)
	t.Cleanup(func() {
		client.Close()
		server.Stop()
	})
	return client
}

// subscribeLogs subscribes client to the logs crit matches and returns the raw notifications.
func subscribeLogs(t *testing.T, client *rpc.Client, crit map[string]interface{}) <-chan json.RawMessage {
	t.Helper()
	notifications := make(chan json.RawMessage, 16)
	sub, err := client.EthSubscribe(t.Context(), notifications, "logs", crit)
	require.NoError(t, err)
	t.Cleanup(sub.Unsubscribe)
	return notifications
}

// TestSubscribeLogsNotifiesAnOversizedBlock asserts that a logs subscription
// reaching a block whose logs alone exceed the log caps notifies an error that
// names the block.
func TestSubscribeLogsNotifiesAnOversizedBlock(t *testing.T) {
	t.Parallel()
	match := common.HexToAddress(LogCapAddr)
	topic := common.HexToHash(LogCapBlockHash)
	candidates := buildRangeCapCandidates(5, uint64(rangeCapTestHeight), match, topic, nil)
	fixture := setupRangeCapFixture(t, 5, candidates, evmrpc.FilterConfigTest{
		MaxLog:   1,
		MaxBlock: evmrpc.DefaultMaxBlockRange,
	})

	notifications := subscribeLogs(t, dialLogsSubscriptions(t, fixture.fetcher), map[string]interface{}{
		"fromBlock": hexutil.EncodeUint64(uint64(rangeCapTestHeight)),
		"address":   match,
	})

	select {
	case raw := <-notifications:
		var notification map[string]string
		require.NoError(t, json.Unmarshal(raw, &notification), "notification %s", raw)
		require.Equal(t, hexutil.EncodeUint64(uint64(rangeCapTestHeight)), notification["blockNumber"], "notification %s", raw)
		require.Contains(t, notification["error"], fmt.Sprintf("logs of block %d skipped", rangeCapTestHeight))
		require.Contains(t, notification["error"], "too many logs")
		require.Len(t, notification, 2, "notification %s", raw)
	case <-time.After(10 * time.Second):
		t.Fatal("no notification for the oversized block")
	}
}

// TestSubscribeLogsStartsAfterTheHead asserts that a logs subscription with an
// omitted fromBlock, or with the fromBlock 0 to latest range ethclient sends
// for an open-ended query, does not deliver the logs of the head block at
// subscription, while a numeric fromBlock at that block does.
func TestSubscribeLogsStartsAfterTheHead(t *testing.T) {
	t.Parallel()
	const headLogs = 2
	match := common.HexToAddress(LogCapAddr)
	topic := common.HexToHash(LogCapBlockHash)
	candidates := buildRangeCapCandidates(headLogs, uint64(rangeCapTestHeight), match, topic, nil)
	fixture := setupRangeCapFixture(t, headLogs, candidates, evmrpc.FilterConfigTest{
		MaxLog:   10,
		MaxBlock: evmrpc.DefaultMaxBlockRange,
	})
	client := dialLogsSubscriptions(t, fixture.fetcher)

	atHead := subscribeLogs(t, client, map[string]interface{}{
		"fromBlock": hexutil.EncodeUint64(uint64(rangeCapTestHeight)),
		"address":   match,
	})
	omitted := subscribeLogs(t, client, map[string]interface{}{"address": match})
	openEnded := subscribeLogs(t, client, map[string]interface{}{
		"fromBlock": "0x0",
		"toBlock":   "latest",
		"address":   match,
	})

	for range headLogs {
		select {
		case raw := <-atHead:
			var log struct {
				BlockNumber hexutil.Uint64 `json:"blockNumber"`
			}
			require.NoError(t, json.Unmarshal(raw, &log), "notification %s", raw)
			require.Equal(t, hexutil.Uint64(rangeCapTestHeight), log.BlockNumber)
		case <-time.After(10 * time.Second):
			t.Fatal("no log for the subscription from the head block")
		}
	}

	// Each subscription polls as soon as it is created, so the others have had as long as the
	// one above to deliver the head block.
	select {
	case raw := <-omitted:
		t.Fatalf("subscription with an omitted fromBlock delivered %s", raw)
	case raw := <-openEnded:
		t.Fatalf("subscription from block 0 to latest delivered %s", raw)
	case <-time.After(time.Second):
	}
}

func TestSubscriptionManager(t *testing.T) {
	manager := evmrpc.NewSubscriptionManager(&MockClient{})
	res, subCh, err := manager.Subscribe(context.Background(), evmrpc.NewHeadQueryBuilder(), 10)
	require.Nil(t, err)
	require.NotNil(t, subCh)
	require.Equal(t, 1, int(res))

	res, subCh, err = manager.Subscribe(context.Background(), evmrpc.NewHeadQueryBuilder(), 10)
	require.Nil(t, err)
	require.NotNil(t, subCh)
	require.Equal(t, 2, int(res))

	badManager := evmrpc.NewSubscriptionManager(&MockBadClient{})
	_, subCh, err = badManager.Subscribe(context.Background(), evmrpc.NewHeadQueryBuilder(), 10)
	require.NotNil(t, err)
	require.Nil(t, subCh)
}

// failingReceiptStore is a receipt store, caught up to every height, whose receipt reads fail with err.
type failingReceiptStore struct {
	receipt.ReceiptStore
	err error
}

func (s *failingReceiptStore) LatestVersion() int64 { return math.MaxInt64 }

func (s *failingReceiptStore) GetReceipt(sdk.Context, common.Hash) (*types.Receipt, error) {
	return nil, s.err
}

func newHeadsAPIReadingReceiptsFrom(store receipt.ReceiptStore) *evmrpc.SubscriptionAPI {
	k := &keeper.Keeper{}
	k.SetReceiptStoreForTesting(store)
	return evmrpc.NewLogsSubscriptionAPIForTest(evmrpc.NewLogFetcherForTest(evmrpc.LogFetcherTestDeps{
		TmClient:         &MockClient{},
		K:                k,
		TxConfigProvider: func(int64) client.TxConfig { return TxConfig },
		CtxProvider:      func(int64) sdk.Context { return Ctx },
	}), 1)
}

// TestNewHeadReceiptTotalsFailOnReceiptReadError verifies that a receipt that
// exists but cannot be read sends the newHeads header to its fallback rather
// than leaving that transaction's logs out of its bloom.
func TestNewHeadReceiptTotalsFailOnReceiptReadError(t *testing.T) {
	cause := errors.New("pebble: I/O error")
	api := newHeadsAPIReadingReceiptsFrom(&failingReceiptStore{err: cause})

	_, _, err := api.ReadReceiptTotalsForTest(MockHeight2)

	require.ErrorIs(t, err, cause)
}

// TestNewHeadReceiptTotalsSkipMissingReceipts verifies that a transaction
// with no receipt is left out, as eth_getBlockByNumber leaves it out, and is
// not taken for a failed read.
func TestNewHeadReceiptTotalsSkipMissingReceipts(t *testing.T) {
	api := newHeadsAPIReadingReceiptsFrom(&failingReceiptStore{err: receipt.ErrNotFound})

	logsBloom, gasUsed, err := api.ReadReceiptTotalsForTest(MockHeight2)

	require.NoError(t, err)
	require.Equal(t, ethtypes.Bloom{}, logsBloom)
	require.Zero(t, gasUsed)
}
