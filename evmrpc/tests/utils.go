package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	seiutils "github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
)

const testAddr = "127.0.0.1"

var portProvider = atomic.Int32{}

func init() {
	portProvider.Store(7800)
}

type TestServer struct {
	evmrpc.EVMServer
	port int
}

func (ts TestServer) Run(r func(port int)) {
	_ = ts.Start()
	defer ts.Stop()
	r(ts.port)
}

func SetupTestServer(
	blocks [][][]byte,
	initializer ...func(sdk.Context, *app.App),
) TestServer {
	port := int(portProvider.Add(1))
	mockClient := MockClient{blocks: append([][][]byte{{}}, blocks...)}
	a := app.Setup(false, true)
	a.ChainID = "sei-test"
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
		_, _ = a.Commit(context.Background())
		mockClient.recordBlockResult(res.TxResults, res.ConsensusParamUpdates, res.Events)
	}
	cfg := evmrpc.DefaultConfig
	cfg.HTTPEnabled = true
	cfg.HTTPPort = port
	s, err := evmrpc.NewEVMHTTPServer(
		log.NewNopLogger(),
		cfg,
		&mockClient,
		&a.EvmKeeper,
		a.BaseApp,
		a.AnteHandler,
		a.RPCContextProvider,
		a.GetTxConfig(),
		"",
		func(ctx context.Context, hash common.Hash) (bool, error) {
			return false, nil
		},
	)
	if err != nil {
		panic(err)
	}
	return TestServer{EVMServer: s, port: port}
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
	defer res.Body.Close()
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
	tx := signCosmosTxWithMnemonic(msg, mnemonic1, acctN, seq)
	txBz, _ := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	return txBz
}
