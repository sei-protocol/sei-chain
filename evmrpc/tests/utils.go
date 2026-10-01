package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	seiutils "github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

const testAddr = "127.0.0.1"

var portProvider = atomic.Int32{}

func init() {
	portProvider.Store(7800)
}

type TestServer struct {
	evmrpc.EVMServer
	port int

	mockClient  *MockClient
	app         *app.App
	ctxProvider func(int64) sdk.Context
}

func (ts TestServer) Run(r func(port int)) {
	_ = ts.Start()
	defer ts.Stop()
	r(ts.port)
}

// RequireTxSucceeded fails the test unless the index-th transaction of the
// block at height committed with code 0.
func (ts TestServer) RequireTxSucceeded(t *testing.T, height int64, index int) {
	t.Helper()
	results := ts.mockClient.txResults[height-1]
	require.Less(t, index, len(results))
	require.Equal(t, uint32(0), results[index].Code, results[index].Log)
}

func (ts TestServer) SetupBlocks(blocks [][][]byte, initializer ...func(sdk.Context, *app.App)) {
	ts.mockClient.blocks = append(ts.mockClient.blocks, blocks...)
	blockHeight := int64(len(ts.mockClient.txResults) + 1)
	for i, block := range blocks {
		height := blockHeight + int64(i)
		blockTime := time.Now()
		res, err := ts.app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Txs:  block,
			Hash: mockHash(height, 0),
			Header: &tmproto.Header{
				ChainID: ts.app.ChainID,
				Height:  height,
				Time:    blockTime,
			},
		})
		if err != nil {
			panic(err)
		}
		_, _ = ts.app.Commit(context.Background())
		ts.mockClient.recordBlockResult(res.TxResults, res.ConsensusParamUpdates, res.Events)
	}
	settleCommittedBlocks(ts.app, ts.ctxProvider)
}

// settleCommittedBlocks blocks until the state store and receipt store have applied every block the
// app has committed. Both apply writes in the background, so a query served from either right after
// Commit would otherwise read state that is not there yet.
func settleCommittedBlocks(a *app.App, ctxProvider func(int64) sdk.Context) {
	latest := ctxProvider(evmrpc.LatestCtxHeight).BlockHeight()
	if stateStore := a.GetStateStore(); stateStore != nil {
		if w, ok := stateStore.(dbtypes.PendingWriteWaiter); ok {
			w.WaitForPendingWrites()
		}
		if stateStore.GetLatestVersion() < latest {
			if err := stateStore.SetLatestVersion(latest); err != nil {
				panic(err)
			}
		}
	}
	if store := a.EvmKeeper.ReceiptStore(); store != nil {
		deadline := time.Now().Add(receiptSettleTimeout)
		for store.LatestVersion() < latest {
			if time.Now().After(deadline) {
				panic(fmt.Sprintf("receipt store still at version %d after committing height %d", store.LatestVersion(), latest))
			}
			time.Sleep(time.Millisecond)
		}
	}
}

// receiptSettleTimeout bounds how long settleCommittedBlocks waits for the receipt writer, which
// otherwise has no failure signal a caller can observe short of the package test timeout.
const receiptSettleTimeout = 30 * time.Second

func initializeApp(
	t *testing.T,
	chainID string,
	initializer ...func(sdk.Context, *app.App),
) (*app.App, *abci.ResponseFinalizeBlock) {
	a := app.Setup(t, false, true, chainID == "pacific-1")
	a.ChainID = chainID
	res, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Txs:  [][]byte{},
		Hash: mockHash(1, 0),
		Header: &tmproto.Header{
			ChainID: chainID,
			Height:  1,
			Time:    time.Now(),
		},
	})
	if err != nil {
		panic(err)
	}
	ctx := a.GetContextForDeliverTx(nil)
	for _, i := range initializer {
		i(ctx, a)
	}
	_, _ = a.Commit(context.Background())
	return a, res
}

func SetupTestServer(
	t *testing.T,
	blocks [][][]byte,
	initializer ...func(sdk.Context, *app.App),
) TestServer {
	a, res := initializeApp(t, "sei-test", initializer...)
	mockClient := &MockClient{blocks: append([][][]byte{{}}, blocks...)}
	mockClient.recordBlockResult(res.TxResults, res.ConsensusParamUpdates, res.Events)
	for i, block := range blocks {
		height := int64(i + 2)
		blockTime := time.Now()
		res, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Txs:  block,
			Hash: mockHash(height, 0),
			Header: &tmproto.Header{
				ChainID: a.ChainID,
				Height:  height,
				Time:    blockTime,
			},
		})
		if err != nil {
			panic(err)
		}
		// for i, txRes := range res.TxResults {
		// 	fmt.Printf("tx %d: %s\n", i, txRes.Log)
		// }
		_, _ = a.Commit(context.Background())
		mockClient.recordBlockResult(res.TxResults, res.ConsensusParamUpdates, res.Events)
	}
	return setupTestServer(a, a.RPCContextProvider, mockClient)
}

func SetupMockPacificTestServer(t *testing.T, initializer func(*app.App, *MockClient) sdk.Context) TestServer {
	a, res := initializeApp(t, "pacific-1")
	mockClient := &MockClient{blocks: [][][]byte{{}}}
	// seed mock client with genesis block results so latest height queries work
	mockClient.recordBlockResult(res.TxResults, res.ConsensusParamUpdates, res.Events)
	ctx := initializer(a, mockClient)
	return setupTestServer(a, func(int64) sdk.Context { return ctx }, mockClient)
}

func setupTestServer(
	a *app.App,
	ctxProvider func(int64) sdk.Context,
	mockClient *MockClient,
) TestServer {
	port := int(portProvider.Add(1))
	cfg := evmrpcconfig.DefaultConfig
	cfg.HTTPEnabled = true
	cfg.HTTPPort = port
	cfg.EnabledLegacySeiApis = evmrpc.SeiLegacyAllGatedMethodNames()
	s, err := evmrpc.NewEVMHTTPServer(
		cfg,
		mockClient,
		&a.EvmKeeper,
		a.BeginBlockKeepers,
		a.BaseApp,
		a.TracerAnteHandler,
		ctxProvider,
		func(int64) client.TxConfig { return a.GetTxConfig() },
		"",
		a.GetStateStore(),
		false,
		nil,
	)
	if err != nil {
		panic(err)
	}
	settleCommittedBlocks(a, ctxProvider)
	if store := a.EvmKeeper.ReceiptStore(); store != nil {
		// These tests seed receipts by other means and would otherwise read against an unset window.
		if err := receipt.PinVersions(store, 1, store.LatestVersion()); err != nil {
			panic(err)
		}
	}
	return TestServer{EVMServer: s, port: port, mockClient: mockClient, app: a, ctxProvider: ctxProvider}
}

func sendRequestWithNamespace(namespace string, port int, method string, params ...interface{}) map[string]interface{} {
	paramsFormatted := ""
	if len(params) > 0 {
		paramsFormatted = strings.Join(seiutils.Map(params, formatParam), ",")
	}
	body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_%s\",\"params\":[%s],\"id\":\"test\"}", namespace, method, paramsFormatted)
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", testAddr, port), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer func() { _ = res.Body.Close() }()
	resBody, _ := io.ReadAll(res.Body)
	resObj := map[string]interface{}{}
	_ = json.Unmarshal(resBody, &resObj)
	return resObj
}

func formatParam(p interface{}) string {
	if p == nil {
		return "null"
	}
	switch v := p.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%f", v)
	case string:
		return fmt.Sprintf("\"%s\"", v)
	case common.Address:
		return fmt.Sprintf("\"%s\"", v)
	case []common.Address:
		wrapper := func(i common.Address) string {
			return formatParam(i)
		}
		return fmt.Sprintf("[%s]", strings.Join(seiutils.Map(v, wrapper), ","))
	case common.Hash:
		return fmt.Sprintf("\"%s\"", v)
	case []common.Hash:
		wrapper := func(i common.Hash) string {
			return formatParam(i)
		}
		return fmt.Sprintf("[%s]", strings.Join(seiutils.Map(v, wrapper), ","))
	case [][]common.Hash:
		wrapper := func(i []common.Hash) string {
			return formatParam(i)
		}
		return fmt.Sprintf("[%s]", strings.Join(seiutils.Map(v, wrapper), ","))
	case []string:
		return fmt.Sprintf("[%s]", strings.Join(v, ","))
	case []float64:
		return fmt.Sprintf("[%s]", strings.Join(seiutils.Map(v, func(s float64) string { return fmt.Sprintf("%f", s) }), ","))
	case []interface{}:
		return fmt.Sprintf("[%s]", strings.Join(seiutils.Map(v, formatParam), ","))
	case map[string]interface{}:
		kvs := []string{}
		for k, v := range v {
			kvs = append(kvs, fmt.Sprintf("\"%s\":%s", k, formatParam(v)))
		}
		return fmt.Sprintf("{%s}", strings.Join(kvs, ","))
	case map[string]map[string]interface{}:
		kvs := []string{}
		for k, v := range v {
			kvs = append(kvs, fmt.Sprintf("\"%s\":%s", k, formatParam(v)))
		}
		return fmt.Sprintf("{%s}", strings.Join(kvs, ","))
	default:
		return fmt.Sprintf("%s", p)
	}
}

func signAndEncodeTx(txData ethtypes.TxData, mnemonic string) []byte {
	signed := signTxWithMnemonic(txData, mnemonic)
	return encodeEvmTx(txData, signed)
}

func encodeEvmTx(txData ethtypes.TxData, signed *ethtypes.Transaction) []byte {
	var typedTx proto.Message
	switch txData.(type) {
	case *ethtypes.LegacyTx:
		typedTx, _ = ethtx.NewLegacyTx(signed)
	case *ethtypes.AccessListTx:
		typedTx, _ = ethtx.NewAccessListTx(signed)
	case *ethtypes.DynamicFeeTx:
		typedTx, _ = ethtx.NewDynamicFeeTx(signed)
	case *ethtypes.BlobTx:
		typedTx, _ = ethtx.NewBlobTx(signed)
	case *ethtypes.SetCodeTx:
		typedTx, _ = ethtx.NewSetCodeTx(signed)
	default:
		panic("invalid tx type")
	}
	msg, _ := types.NewMsgEVMTransaction(typedTx)
	builder := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	_ = builder.SetMsgs(msg)
	tx := builder.GetTx()
	txBz, _ := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	return txBz
}

func signAndEncodeCosmosTx(msg sdk.Msg, mnemonic string, acctN uint64, seq uint64) []byte {
	tx, err := signCosmosTxWithMnemonic(msg, mnemonic, acctN, seq)
	if err != nil {
		// TODO: pass in testing.T and assert no error instead
		panic(err)
	}
	return encodeCosmosTx(tx)
}

func encodeCosmosTx(tx sdk.Tx) []byte {
	txBz, _ := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	return txBz
}
