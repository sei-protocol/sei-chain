package evmrpc_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/websocket"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/bytes"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/rpc/client/mock"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

const TestAddr = "127.0.0.1"
const TestPort = 7777
const TestWSPort = 7778
const TestBadPort = 7779

const MockHeight = 8

var EncodingConfig = app.MakeEncodingConfig()
var TxConfig = EncodingConfig.TxConfig
var Encoder = TxConfig.TxEncoder()
var Decoder = TxConfig.TxDecoder()
var Tx sdk.Tx
var UnconfirmedTx sdk.Tx

var SConfig = evmrpc.SimulateConfig{GasCap: 10000000}

var filterTimeoutDuration = 500 * time.Millisecond

type MockClient struct {
	mock.Client
}

func (c *MockClient) mockBlock(height int64) *coretypes.ResultBlock {
	return &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{
			Hash: bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000001")),
		},
		Block: &tmtypes.Block{
			Header: tmtypes.Header{
				ChainID:         "test",
				Height:          height,
				Time:            time.Unix(1696941649, 0),
				DataHash:        bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000002")),
				AppHash:         bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000003")),
				LastResultsHash: bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000004")),
				ProposerAddress: tmtypes.Address([]byte("0000000000000000000000000000000000000000000000000000000000000005")),
				LastBlockID: tmtypes.BlockID{
					Hash: bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000006")),
				},
				LastCommitHash:     bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000007")),
				ValidatorsHash:     bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000009")),
				NextValidatorsHash: bytes.HexBytes([]byte("000000000000000000000000000000000000000000000000000000000000000A")),
				ConsensusHash:      bytes.HexBytes([]byte("000000000000000000000000000000000000000000000000000000000000000B")),
				EvidenceHash:       bytes.HexBytes([]byte("000000000000000000000000000000000000000000000000000000000000000E")),
			},
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{func() []byte {
					bz, _ := Encoder(Tx)
					return bz
				}()},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight - 1,
			},
		},
	}
}

func (c *MockClient) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	return &coretypes.ResultGenesis{Genesis: &tmtypes.GenesisDoc{InitialHeight: 1}}, nil
}

func (c *MockClient) Block(context.Context, *int64) (*coretypes.ResultBlock, error) {
	return c.mockBlock(MockHeight), nil
}

func (c *MockClient) BlockByHash(context.Context, bytes.HexBytes) (*coretypes.ResultBlock, error) {
	return c.mockBlock(MockHeight), nil
}

func (c *MockClient) BlockResults(context.Context, *int64) (*coretypes.ResultBlockResults, error) {
	abciEvent := NewABCIEventBuilder().SetBlockNum(8).Build()
	return &coretypes.ResultBlockResults{
		TxsResults: []*abci.ExecTxResult{
			{
				Data: func() []byte {
					bz, _ := Encoder(Tx)
					return bz
				}(),
				GasWanted: 10,
				GasUsed:   5,
				Events: []abci.Event{
					abciEvent,
				},
			},
		},
	}, nil
}

func (c *MockClient) Subscribe(context.Context, string, string, ...int) (<-chan coretypes.ResultEvent, error) {
	return make(chan coretypes.ResultEvent, 1), nil
}

func (c *MockClient) Events(_ context.Context, req *coretypes.RequestEvents) (*coretypes.ResultEvents, error) {
	if strings.Contains(req.Filter.Query, "tm.event = 'NewBlock'") {
		var cursor int
		var err error
		if req.After != "" {
			cursor, err = strconv.Atoi(req.After)
			if err != nil {
				panic("invalid cursor")
			}
		} else {
			cursor = MockHeight
		}
		resultBlock := c.mockBlock(int64(cursor))
		data := tmtypes.EventDataNewBlock{
			Block:   resultBlock.Block,
			BlockID: tmtypes.BlockID{},
		}
		newCursor := strconv.FormatInt(int64(cursor)+1, 10)
		return buildSingleResultEvent(data, false, newCursor, "event"), nil
	} else if strings.Contains(req.Filter.Query, "tm.event = 'Tx'") {
		eb := NewABCIEventBuilder()

		// assume newCursor is block number for testing purposes
		var newCursor string
		if req.After != "" {
			cursorAfter, err := strconv.Atoi(req.After)
			if err != nil {
				panic("invalid cursor")
			}
			nextBlockNum := cursorAfter + 1
			eb = eb.SetBlockNum(nextBlockNum)
			newCursor = strconv.FormatInt(int64(nextBlockNum), 10)
		} else {
			var startBlock int
			re := regexp.MustCompile(`evm_log.block_number >= '(\d+?)'`)
			matches := re.FindStringSubmatch(req.Filter.Query)
			if len(matches) == 2 {
				var err error
				startBlock, err = strconv.Atoi(matches[1])
				if err != nil {
					return nil, err
				}
				eb = eb.SetBlockNum(startBlock)
				newCursor = strconv.FormatInt(int64(startBlock), 10)
			}
		}

		var blockHash string
		re := regexp.MustCompile(`evm_log.block_hash = '(.+?)'`)
		matches := re.FindStringSubmatch(req.Filter.Query)
		if len(matches) == 2 {
			blockHash = matches[1]
			eb = eb.SetBlockHash(blockHash)
		}

		var contractAddress string
		re = regexp.MustCompile(`evm_log.contract_address = '(.+?)'.*`)
		matches = re.FindStringSubmatch(req.Filter.Query)
		if len(matches) == 2 {
			contractAddress = matches[1]
			eb = eb.SetContractAddress(contractAddress)
		}

		// hardcode topic matches to match up with tests since doing the regex is too complicated
		if strings.Contains(req.Filter.Query, "evm_log.topics = MATCHES '\\[(0x0000000000000000000000000000000000000000000000000000000000000123).*\\]'") {
			eb = eb.SetTopics([]string{"0x0000000000000000000000000000000000000000000000000000000000000123"})
		} else if strings.Contains(req.Filter.Query, "evm_log.topics = MATCHES '\\[(0x0000000000000000000000000000000000000000000000000000000000000123)[^\\,]*,(0x0000000000000000000000000000000000000000000000000000000000000456).*\\]'") {
			eb = eb.SetTopics([]string{"0x0000000000000000000000000000000000000000000000000000000000000123", "0x0000000000000000000000000000000000000000000000000000000000000456"})
		} else if strings.Contains(req.Filter.Query, "evm_log.topics = MATCHES '\\[[^\\,]*,(0x0000000000000000000000000000000000000000000000000000000000000456).*\\]'") {
			eb = eb.SetTopics([]string{"0x0000000000000000000000000000000000000000000000000000000000000123", "0x0000000000000000000000000000000000000000000000000000000000000456"})
		}

		return buildSingleResultEvent(eb.Build(), false, newCursor, "event"), nil
	} else {
		panic("unknown query")
	}
}

func buildSingleResultEvent(data interface{}, more bool, cursor string, event string) *coretypes.ResultEvents {
	eventData, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	return &coretypes.ResultEvents{
		Items: []*coretypes.EventItem{
			{
				Cursor: cursor,
				Event:  event,
				Data:   eventData,
			},
		},
		More:   more,
		Oldest: cursor,
		Newest: cursor,
	}
}

func (c *MockClient) BroadcastTx(context.Context, tmtypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	return &coretypes.ResultBroadcastTx{Code: 0}, nil
}

func (c *MockClient) UnconfirmedTxs(ctx context.Context, page, perPage *int) (*coretypes.ResultUnconfirmedTxs, error) {
	tx, _ := Encoder(UnconfirmedTx)
	return &coretypes.ResultUnconfirmedTxs{Txs: []tmtypes.Tx{tx}}, nil
}

type MockBadClient struct {
	MockClient
}

func (m *MockBadClient) Block(context.Context, *int64) (*coretypes.ResultBlock, error) {
	return nil, errors.New("error block")
}

func (m *MockBadClient) BlockByHash(context.Context, bytes.HexBytes) (*coretypes.ResultBlock, error) {
	return nil, errors.New("error block")
}

func (m *MockBadClient) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	return nil, errors.New("error genesis")
}

func (m *MockBadClient) Subscribe(context.Context, string, string, ...int) (<-chan coretypes.ResultEvent, error) {
	return nil, errors.New("bad subscribe")
}

func (m *MockBadClient) BroadcastTx(context.Context, tmtypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	return &coretypes.ResultBroadcastTx{Code: 1, Codespace: "test", Log: "log"}, nil
}

var EVMKeeper *keeper.Keeper
var Ctx sdk.Context

func init() {
	types.RegisterInterfaces(EncodingConfig.InterfaceRegistry)
	EVMKeeper, Ctx = testkeeper.MockEVMKeeper()
	goodConfig := evmrpc.DefaultConfig
	goodConfig.HTTPPort = TestPort
	goodConfig.WSPort = TestWSPort
	goodConfig.FilterTimeout = 500 * time.Millisecond
	HttpServer, err := evmrpc.NewEVMHTTPServer(log.NewNopLogger(), goodConfig, &MockClient{}, EVMKeeper, func(int64) sdk.Context { return Ctx }, TxConfig)
	if err != nil {
		panic(err)
	}
	if err := HttpServer.Start(); err != nil {
		panic(err)
	}
	badConfig := evmrpc.DefaultConfig
	badConfig.HTTPPort = TestBadPort
	badConfig.FilterTimeout = 500 * time.Millisecond
	badHTTPServer, err := evmrpc.NewEVMHTTPServer(log.NewNopLogger(), badConfig, &MockBadClient{}, EVMKeeper, func(int64) sdk.Context { return Ctx }, TxConfig)
	if err != nil {
		panic(err)
	}
	if err := badHTTPServer.Start(); err != nil {
		panic(err)
	}
	wsServer, err := evmrpc.NewEVMWebSocketServer(log.NewNopLogger(), goodConfig, &MockBadClient{}, EVMKeeper, func(int64) sdk.Context { return Ctx }, TxConfig)
	if err != nil {
		panic(err)
	}
	if err := wsServer.Start(); err != nil {
		panic(err)
	}
	time.Sleep(1 * time.Second)

	to := common.HexToAddress("010203")
	txData := ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   big.NewInt(1),
	}
	mnemonic := "fish mention unlock february marble dove vintage sand hub ordinary fade found inject room embark supply fabric improve spike stem give current similar glimpse"
	derivedPriv, _ := hd.Secp256k1.Derive()(mnemonic, "", "")
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	evmParams := EVMKeeper.GetParams(Ctx)
	ethCfg := evmParams.GetChainConfig().EthereumConfig(big.NewInt(1))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(Ctx.BlockHeight()), uint64(Ctx.BlockTime().Unix()))
	tx := ethtypes.NewTx(&txData)
	tx, err = ethtypes.SignTx(tx, signer, key)
	if err != nil {
		panic(err)
	}
	typedTx, err := ethtx.NewDynamicFeeTx(tx)
	if err != nil {
		panic(err)
	}
	msg, err := types.NewMsgEVMTransaction(typedTx)
	if err != nil {
		panic(err)
	}
	b := TxConfig.NewTxBuilder()
	if err := b.SetMsgs(msg); err != nil {
		panic(err)
	}
	Tx = b.GetTx()
	if err := EVMKeeper.SetReceipt(Ctx, tx.Hash(), &types.Receipt{
		From:              "0x1234567890123456789012345678901234567890",
		To:                "0x1234567890123456789012345678901234567890",
		TransactionIndex:  0,
		BlockNumber:       8,
		TxType:            1,
		ContractAddress:   "0x1234567890123456789012345678901234567890",
		CumulativeGasUsed: 123,
		TxHashHex:         "0x123456789012345678902345678901234567890123456789012345678901234",
		GasUsed:           55,
		Status:            0,
		EffectiveGasPrice: 10,
	}); err != nil {
		panic(err)
	}
	seiAddr, err := sdk.AccAddressFromHex(common.Bytes2Hex([]byte("seiAddr")))
	if err != nil {
		panic(err)
	}
	evmAddr := common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))
	EVMKeeper.SetAddressMapping(Ctx, seiAddr, evmAddr)
	EVMKeeper.SetOrDeleteBalance(Ctx, common.HexToAddress("0x1234567890123456789023456789012345678901"), 1000)
	EVMKeeper.SetCode(Ctx, common.HexToAddress("0x1234567890123456789023456789012345678901"), []byte("abc"))
	EVMKeeper.SetState(
		Ctx,
		common.HexToAddress("0x1234567890123456789023456789012345678901"),
		common.BytesToHash([]byte("key")),
		common.BytesToHash([]byte("value")),
	)
	EVMKeeper.SetNonce(Ctx, common.HexToAddress("0x1234567890123456789012345678901234567890"), 1)

	unconfirmedTxData := ethtypes.DynamicFeeTx{
		Nonce:     2,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(2000),
		Data:      []byte("abc"),
		ChainID:   big.NewInt(1),
	}
	tx = ethtypes.NewTx(&unconfirmedTxData)
	tx, err = ethtypes.SignTx(tx, signer, key)
	if err != nil {
		panic(err)
	}
	typedTx, err = ethtx.NewDynamicFeeTx(tx)
	if err != nil {
		panic(err)
	}
	msg, err = types.NewMsgEVMTransaction(typedTx)
	if err != nil {
		panic(err)
	}
	b = TxConfig.NewTxBuilder()
	if err := b.SetMsgs(msg); err != nil {
		panic(err)
	}
	UnconfirmedTx = b.GetTx()
}

//nolint:deadcode
func sendRequestGood(t *testing.T, method string, params ...interface{}) map[string]interface{} {
	return sendRequest(t, TestPort, method, params...)
}

//nolint:deadcode
func sendRequestBad(t *testing.T, method string, params ...interface{}) map[string]interface{} {
	return sendRequest(t, TestBadPort, method, params...)
}

func sendRequest(t *testing.T, port int, method string, params ...interface{}) map[string]interface{} {
	paramsFormatted := ""
	if len(params) > 0 {
		paramsFormatted = strings.Join(utils.Map(params, formatParam), ",")
	}
	body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_%s\",\"params\":[%s],\"id\":\"test\"}", method, paramsFormatted)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, port), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
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
		return fmt.Sprintf("[%s]", strings.Join(utils.Map(v, wrapper), ","))
	case common.Hash:
		return fmt.Sprintf("\"%s\"", v)
	case []common.Hash:
		wrapper := func(i common.Hash) string {
			return formatParam(i)
		}
		return fmt.Sprintf("[%s]", strings.Join(utils.Map(v, wrapper), ","))
	case [][]common.Hash:
		wrapper := func(i []common.Hash) string {
			return formatParam(i)
		}
		return fmt.Sprintf("[%s]", strings.Join(utils.Map(v, wrapper), ","))
	case []string:
		return fmt.Sprintf("[%s]", strings.Join(v, ","))
	case []interface{}:
		return fmt.Sprintf("[%s]", strings.Join(utils.Map(v, formatParam), ","))
	case map[string]interface{}:
		kvs := []string{}
		for k, v := range v {
			kvs = append(kvs, fmt.Sprintf("\"%s\":%s", k, formatParam(v)))
		}
		return fmt.Sprintf("{%s}", strings.Join(kvs, ","))
	default:
		panic("did not match on type")
	}
}

type ABCIEventBuilder struct {
	contractAddress string
	blockHash       string
	blockNum        int
	data            string
	index           int
	txIndex         int
	removed         bool
	topics          []string
	txHash          common.Hash
}

func NewABCIEventBuilder() *ABCIEventBuilder {
	return &ABCIEventBuilder{
		contractAddress: "0x1111111111111111111111111111111111111111111111111111111111111111",
		blockHash:       "0x1111111111111111111111111111111111111111111111111111111111111111",
		blockNum:        8,
		data:            "xyz",
		index:           1,
		txIndex:         2,
		removed:         true,
		topics:          []string{"0x1111111111111111111111111111111111111111111111111111111111111111,0x1111111111111111111111111111111111111111111111111111111111111112"},
		txHash:          common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111113"),
	}
}

func (b *ABCIEventBuilder) SetContractAddress(contractAddress string) *ABCIEventBuilder {
	b.contractAddress = contractAddress
	return b
}

func (b *ABCIEventBuilder) SetBlockHash(blockHash string) *ABCIEventBuilder {
	b.blockHash = blockHash
	return b
}

func (b *ABCIEventBuilder) SetBlockNum(blockNum int) *ABCIEventBuilder {
	b.blockNum = blockNum
	return b
}

func (b *ABCIEventBuilder) SetData(data string) *ABCIEventBuilder {
	b.data = data
	return b
}

func (b *ABCIEventBuilder) SetIndex(index int) *ABCIEventBuilder {
	b.index = index
	return b
}

func (b *ABCIEventBuilder) SetTxIndex(txIndex int) *ABCIEventBuilder {
	b.txIndex = txIndex
	return b
}

func (b *ABCIEventBuilder) SetRemoved(removed bool) *ABCIEventBuilder {
	b.removed = removed
	return b
}

func (b *ABCIEventBuilder) SetTopics(topics []string) *ABCIEventBuilder {
	b.topics = topics
	return b
}

func (b *ABCIEventBuilder) SetTxHash(txHash common.Hash) *ABCIEventBuilder {
	b.txHash = txHash
	return b
}

func (b *ABCIEventBuilder) Build() abci.Event {
	return abci.Event{
		Type: types.EventTypeEVMLog,
		Attributes: []abci.EventAttribute{{
			Key:   []byte(types.AttributeTypeContractAddress),
			Value: []byte(b.contractAddress),
		}, {
			Key:   []byte(types.AttributeTypeBlockHash),
			Value: []byte(b.blockHash),
		}, {
			Key:   []byte(types.AttributeTypeBlockNumber),
			Value: []byte(fmt.Sprintf("%d", b.blockNum)),
		}, {
			Key:   []byte(types.AttributeTypeData),
			Value: []byte(b.data),
		}, {
			Key:   []byte(types.AttributeTypeIndex),
			Value: []byte(fmt.Sprintf("%d", b.index)),
		}, {
			Key:   []byte(types.AttributeTypeTxIndex),
			Value: []byte(fmt.Sprintf("%d", b.txIndex)),
		}, {
			Key:   []byte(types.AttributeTypeRemoved),
			Value: []byte(fmt.Sprintf("%t", b.removed)),
		}, {
			Key:   []byte(types.AttributeTypeTopics),
			Value: []byte(strings.Join(b.topics, ",")),
		}, {
			Key:   []byte(types.AttributeTypeTxHash),
			Value: []byte(b.txHash.Hex()),
		}},
	}
}

func TestEcho(t *testing.T) {
	// Test HTTP server
	body := "{\"jsonrpc\": \"2.0\",\"method\": \"echo_echo\",\"params\":[\"something\"],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":\"something\"}\n", string(resBody))

	// Test WS server
	headers := make(http.Header)
	headers.Set("Origin", "localhost")
	headers.Set("Content-Type", "application/json")
	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://%s:%d", TestAddr, TestWSPort), headers)
	require.Nil(t, err)
	require.Nil(t, conn.WriteMessage(websocket.TextMessage, []byte(body)))
	_, buf, err := conn.ReadMessage()
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":\"something\"}\n", string(buf))
}
