package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
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
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	seiutils "github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

const testAddr = "127.0.0.1"

var portProvider = atomic.Int32{}

func init() {
	portProvider.Store(7800)
}

type TestServer struct {
	evmrpc.EVMServer
	port int

	mockClient *MockClient
	app        *app.App
}

func (ts TestServer) Run(r func(port int)) {
	_ = ts.Start()
	defer ts.Stop()
	r(ts.port)
}

func (ts TestServer) SetupBlocks(blocks [][][]byte, initializer ...func(sdk.Context, *app.App)) {
	ts.mockClient.blocks = append(ts.mockClient.blocks, blocks...)
	blockHeight := int64(len(ts.mockClient.txResults) + 1)
	for i, block := range blocks {
		height := blockHeight + int64(i)
		blockTime := time.Now()
		res, err := ts.app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Txs:    block,
			Hash:   mockHash(height, 0),
			Height: height,
			Time:   blockTime,
		})
		if err != nil {
			panic(err)
		}
		_, _ = ts.app.Commit(context.Background())
		ts.mockClient.recordBlockResult(res.TxResults, res.ConsensusParamUpdates, res.Events)
	}
}

func initializeApp(
	t *testing.T,
	chainID string,
	initializer ...func(sdk.Context, *app.App),
) (*app.App, *abci.ResponseFinalizeBlock) {
	a := app.Setup(t, false, true, chainID == "pacific-1")
	a.ChainID = chainID
	res, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Txs:    [][]byte{},
		Hash:   mockHash(1, 0),
		Height: 1,
		Time:   time.Now(),
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
			Txs:    block,
			Hash:   mockHash(height, 0),
			Height: height,
			Time:   blockTime,
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
	s, err := evmrpc.NewEVMHTTPServer(
		log.NewNopLogger(),
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
		func(ctx context.Context, hash common.Hash) (bool, error) {
			return false, nil
		},
	)
	if err != nil {
		panic(err)
	}
	if store := a.EvmKeeper.ReceiptStore(); store != nil {
		latest := int64(math.MaxInt64)
		if err := store.SetLatestVersion(latest); err != nil {
			panic(err)
		}
		_ = store.SetEarliestVersion(1)
	}
	return TestServer{EVMServer: s, port: port, mockClient: mockClient, app: a}
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
