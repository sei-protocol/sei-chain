package evmrpc_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
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
	"github.com/tendermint/tendermint/version"
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
var TxNonEvm sdk.Tx
var UnconfirmedTx sdk.Tx

var SConfig = evmrpc.SimulateConfig{GasCap: 10000000}

var filterTimeoutDuration = 500 * time.Millisecond
var TotalTxCount int = 11

var MockBlockID = tmtypes.BlockID{
	Hash: bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000001")),
}

type MockClient struct {
	mock.Client
}

func mustHexToBytes(h string) []byte {
	bz, err := hex.DecodeString(h)
	if err != nil {
		panic(err)
	}
	return bz
}

func mockBlockHeader(height int64) tmtypes.Header {
	return tmtypes.Header{
		ChainID:         "test",
		Height:          height,
		Time:            time.Unix(1696941649, 0),
		DataHash:        bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000002")),
		AppHash:         bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000003")),
		LastResultsHash: bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000004")),
		ProposerAddress: tmtypes.Address(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000005")),
		LastBlockID: tmtypes.BlockID{
			Hash: bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000006")),
		},
		LastCommitHash:     bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000007")),
		ValidatorsHash:     bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000009")),
		NextValidatorsHash: bytes.HexBytes(mustHexToBytes("000000000000000000000000000000000000000000000000000000000000000A")),
		ConsensusHash:      bytes.HexBytes(mustHexToBytes("000000000000000000000000000000000000000000000000000000000000000B")),
		EvidenceHash:       bytes.HexBytes(mustHexToBytes("000000000000000000000000000000000000000000000000000000000000000E")),
	}
}

func (c *MockClient) mockBlock(height int64) *coretypes.ResultBlock {
	return &coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(height),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{
					func() []byte {
						bz, _ := Encoder(Tx)
						return bz
					}(),
					func() []byte {
						bz, _ := Encoder(TxNonEvm)
						return bz
					}(),
				},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight - 1,
			},
		},
	}
}

func (c *MockClient) mockEventDataNewBlockHeader(mockHeight uint64) *tmtypes.EventDataNewBlockHeader {
	return &tmtypes.EventDataNewBlockHeader{
		Header: tmtypes.Header{
			Version: version.Consensus{
				Block: mockHeight,
				App:   10,
			},
			ChainID: "1",
			Height:  int64(mockHeight),
			Time:    time.Now(),

			// prev block info
			LastBlockID: tmtypes.BlockID{
				Hash: bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000006")),
			},

			// hashes of block data
			LastCommitHash: bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000001")),
			DataHash:       bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000002")),

			ValidatorsHash:     bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000009")),
			NextValidatorsHash: bytes.HexBytes(mustHexToBytes("000000000000000000000000000000000000000000000000000000000000000A")),
			ConsensusHash:      bytes.HexBytes(mustHexToBytes("000000000000000000000000000000000000000000000000000000000000000B")),
			EvidenceHash:       bytes.HexBytes(mustHexToBytes("000000000000000000000000000000000000000000000000000000000000000E")),
			AppHash:            bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000003")),
			LastResultsHash:    bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000004")),
			ProposerAddress:    tmtypes.Address(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000005")),
		},
		NumTxs: 5,
		ResultFinalizeBlock: abci.ResponseFinalizeBlock{
			TxResults: mockTxResult(),
			AppHash:   bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000006")),
		},
	}
}

func mockTxResult() []*abci.ExecTxResult {
	return []*abci.ExecTxResult{
		{
			Data:      []byte("abc"),
			Log:       "log1",
			GasUsed:   10,
			GasWanted: 11,
		},
		{
			Data:      []byte("def"),
			Log:       "log2",
			GasUsed:   20,
			GasWanted: 21,
		},
	}
}

func (c *MockClient) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	return &coretypes.ResultGenesis{Genesis: &tmtypes.GenesisDoc{InitialHeight: 1}}, nil
}

func (c *MockClient) Block(_ context.Context, h *int64) (*coretypes.ResultBlock, error) {
	height := int64(MockHeight)
	if h != nil {
		height = *h
	}
	return c.mockBlock(height), nil
}

func (c *MockClient) BlockByHash(context.Context, bytes.HexBytes) (*coretypes.ResultBlock, error) {
	return c.mockBlock(MockHeight), nil
}

func (c *MockClient) BlockResults(context.Context, *int64) (*coretypes.ResultBlockResults, error) {
	return &coretypes.ResultBlockResults{
		TxsResults: []*abci.ExecTxResult{
			{
				Data: func() []byte {
					bz, _ := Encoder(Tx)
					return bz
				}(),
				GasWanted: 10,
				GasUsed:   5,
			},
		},
	}, nil
}

func (c *MockClient) Subscribe(ctx context.Context, subscriber string, query string, outCapacity ...int) (<-chan coretypes.ResultEvent, error) {
	if query == "tm.event = 'NewBlockHeader'" {
		resCh := make(chan coretypes.ResultEvent, 5)
		go func() {
			for i := uint64(0); i < 5; i++ {
				resCh <- coretypes.ResultEvent{
					SubscriptionID: subscriber,
					Query:          query,
					Data:           *c.mockEventDataNewBlockHeader(i + 1),
					Events:         c.mockEventDataNewBlockHeader(i + 1).ABCIEvents(),
				}
				time.Sleep(20 * time.Millisecond) // sleep a little to simulate real events
			}
		}()
		return resCh, nil
		// hardcoded test case for simplicity
	}
	return nil, errors.New("unknown query")
}

func (c *MockClient) Unsubscribe(_ context.Context, _, _ string) error {
	return nil
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
	} else {
		panic("unknown query")
	}
}

func buildSingleResultEvent(data interface{}, more bool, cursor string, event string) *coretypes.ResultEvents {
	eventData, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	wrappedData := evmrpc.EventItemDataWrapper{
		Type:  "NewBlock",
		Value: eventData,
	}
	bz, err := json.Marshal(wrappedData)
	if err != nil {
		panic(err)
	}
	return &coretypes.ResultEvents{
		Items: []*coretypes.EventItem{
			{
				Cursor: cursor,
				Event:  event,
				Data:   bz,
			},
		},
		More:   more,
		Oldest: cursor,
		Newest: cursor,
	}
}

func (c *MockClient) BroadcastTx(context.Context, tmtypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	return &coretypes.ResultBroadcastTx{Code: 0, Hash: []byte("0x123")}, nil
}

func (c *MockClient) UnconfirmedTxs(ctx context.Context, page, perPage *int) (*coretypes.ResultUnconfirmedTxs, error) {
	tx, _ := Encoder(UnconfirmedTx)
	return &coretypes.ResultUnconfirmedTxs{
		Count:      1,
		Total:      1,
		TotalBytes: int64(len(tx)),
		Txs:        []tmtypes.Tx{tx},
	}, nil
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
	return &coretypes.ResultBroadcastTx{Code: 3, Codespace: "test", Log: "log"}, nil
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
	infoLog, err := log.NewDefaultLogger("text", "info")
	if err != nil {
		panic(err)
	}
	HttpServer, err := evmrpc.NewEVMHTTPServer(infoLog, goodConfig, &MockClient{}, EVMKeeper, func(int64) sdk.Context { return Ctx }, TxConfig, "")
	if err != nil {
		panic(err)
	}
	if err := HttpServer.Start(); err != nil {
		panic(err)
	}
	badConfig := evmrpc.DefaultConfig
	badConfig.HTTPPort = TestBadPort
	badConfig.FilterTimeout = 500 * time.Millisecond
	badHTTPServer, err := evmrpc.NewEVMHTTPServer(infoLog, badConfig, &MockBadClient{}, EVMKeeper, func(int64) sdk.Context { return Ctx }, TxConfig, "")
	if err != nil {
		panic(err)
	}
	if err := badHTTPServer.Start(); err != nil {
		panic(err)
	}
	wsServer, err := evmrpc.NewEVMWebSocketServer(infoLog, goodConfig, &MockClient{}, EVMKeeper, func(int64) sdk.Context { return Ctx }, TxConfig, "")
	if err != nil {
		panic(err)
	}
	if err := wsServer.Start(); err != nil {
		panic(err)
	}
	fmt.Printf("wsServer started with config = %+v\n", goodConfig)
	time.Sleep(1 * time.Second)

	chainId := big.NewInt(types.DefaultChainID.Int64())
	to := common.HexToAddress("010203")
	txData := ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainId,
	}
	mnemonic := "fish mention unlock february marble dove vintage sand hub ordinary fade found inject room embark supply fabric improve spike stem give current similar glimpse"
	derivedPriv, _ := hd.Secp256k1.Derive()(mnemonic, "", "")
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	evmParams := EVMKeeper.GetParams(Ctx)
	ethCfg := evmParams.GetChainConfig().EthereumConfig(chainId)
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
	TxNonEvm = app.TestTx{}
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
		Logs: []*types.Log{{
			Address: "0x1111111111111111111111111111111111111111",
			Topics:  []string{"0x1111111111111111111111111111111111111111111111111111111111111111", "0x1111111111111111111111111111111111111111111111111111111111111112"},
		}},
	}); err != nil {
		panic(err)
	}
	seiAddr, err := sdk.AccAddressFromHex(common.Bytes2Hex([]byte("seiAddr")))
	if err != nil {
		panic(err)
	}
	evmAddr := common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))
	EVMKeeper.SetAddressMapping(Ctx, seiAddr, evmAddr)
	unassociatedAddr := common.HexToAddress("0x1234567890123456789023456789012345678901")
	amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(1000)))
	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, amts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(Ctx, types.ModuleName, sdk.AccAddress(unassociatedAddr[:]), amts)
	EVMKeeper.SetCode(Ctx, common.HexToAddress("0x1234567890123456789023456789012345678901"), []byte("abc"))
	EVMKeeper.SetState(
		Ctx,
		common.HexToAddress("0x1234567890123456789023456789012345678901"),
		common.BytesToHash([]byte("key")),
		common.BytesToHash([]byte("value")),
	)
	EVMKeeper.SetAddressMapping(
		Ctx,
		sdk.MustAccAddressFromBech32("sei1mf0llhmqane5w2y8uynmghmk2w4mh0xll9seym"),
		common.HexToAddress("0x1df809C639027b465B931BD63Ce71c8E5834D9d6"),
	)
	EVMKeeper.SetNonce(Ctx, common.HexToAddress("0x1234567890123456789012345678901234567890"), 1)

	unconfirmedTxData := ethtypes.DynamicFeeTx{
		Nonce:     2,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(2000),
		Data:      []byte("abc"),
		ChainID:   chainId,
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

	setupLogs()
}

func setupLogs() {
	// block height 2
	bloom1 := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: []*ethtypes.Log{{
		Address: common.HexToAddress("0x1111111111111111111111111111111111111112"),
		Topics: []common.Hash{
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123"),
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000456"),
		},
	}, {
		Address: common.HexToAddress("0x1111111111111111111111111111111111111112"),
		Topics: []common.Hash{
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123"),
		},
	}}}})
	EVMKeeper.SetReceipt(Ctx, common.HexToHash("0x123456789012345678902345678901234567890123456789012345678900001"), &types.Receipt{
		BlockNumber: 2,
		LogsBloom:   bloom1[:],
		Logs: []*types.Log{{
			Address: "0x1111111111111111111111111111111111111112",
			Topics:  []string{"0x0000000000000000000000000000000000000000000000000000000000000123", "0x0000000000000000000000000000000000000000000000000000000000000456"},
		}, {
			Address: "0x1111111111111111111111111111111111111112",
			Topics:  []string{"0x0000000000000000000000000000000000000000000000000000000000000123"},
		}},
	})
	bloom2 := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: []*ethtypes.Log{{
		Address: common.HexToAddress("0x1111111111111111111111111111111111111113"),
		Topics: []common.Hash{
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123"),
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000456"),
		},
	}}}})
	EVMKeeper.SetReceipt(Ctx, common.HexToHash("0x123456789012345678902345678901234567890123456789012345678900002"), &types.Receipt{
		BlockNumber: 2,
		LogsBloom:   bloom2[:],
		Logs: []*types.Log{{
			Address: "0x1111111111111111111111111111111111111113",
			Topics:  []string{"0x0000000000000000000000000000000000000000000000000000000000000123", "0x0000000000000000000000000000000000000000000000000000000000000456"},
		}},
	})
	bloom3 := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: []*ethtypes.Log{{
		Address: common.HexToAddress("0x1111111111111111111111111111111111111114"),
		Topics: []common.Hash{
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123"),
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000456"),
		},
	}}}})
	EVMKeeper.SetReceipt(Ctx, common.HexToHash("0x123456789012345678902345678901234567890123456789012345678900003"), &types.Receipt{
		BlockNumber: 2,
		LogsBloom:   bloom3[:],
		Logs: []*types.Log{{
			Address: "0x1111111111111111111111111111111111111114",
			Topics:  []string{"0x0000000000000000000000000000000000000000000000000000000000000123", "0x0000000000000000000000000000000000000000000000000000000000000456"},
		}},
	})
	EVMKeeper.SetTxHashesOnHeight(Ctx, 2, []common.Hash{
		common.HexToHash("0x123456789012345678902345678901234567890123456789012345678900001"),
		common.HexToHash("0x123456789012345678902345678901234567890123456789012345678900002"),
		common.HexToHash("0x123456789012345678902345678901234567890123456789012345678900003"),
	})
	EVMKeeper.SetBlockBloom(Ctx, 2, []ethtypes.Bloom{bloom1, bloom2, bloom3})

}

//nolint:deadcode
func sendRequestGood(t *testing.T, method string, params ...interface{}) map[string]interface{} {
	return sendRequest(t, TestPort, method, params...)
}

//nolint:deadcode
func sendRequestBad(t *testing.T, method string, params ...interface{}) map[string]interface{} {
	return sendRequest(t, TestBadPort, method, params...)
}

// nolint:deadcode
func sendRequestGoodWithNamespace(t *testing.T, namespace string, method string, params ...interface{}) map[string]interface{} {
	return sendRequestWithNamespace(t, namespace, TestPort, method, params...)
}

func sendRequest(t *testing.T, port int, method string, params ...interface{}) map[string]interface{} {
	return sendRequestWithNamespace(t, "eth", port, method, params...)
}

func sendRequestWithNamespace(t *testing.T, namespace string, port int, method string, params ...interface{}) map[string]interface{} {
	paramsFormatted := ""
	if len(params) > 0 {
		paramsFormatted = strings.Join(utils.Map(params, formatParam), ",")
	}
	body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_%s\",\"params\":[%s],\"id\":\"test\"}", namespace, method, paramsFormatted)
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

func sendWSRequestGood(t *testing.T, method string, params ...interface{}) (chan map[string]interface{}, chan struct{}) {
	return sendWSRequestAndListen(t, TestWSPort, method, params...)
}

func sendWSRequestBad(t *testing.T, method string, params ...interface{}) (chan map[string]interface{}, chan struct{}) {
	return sendWSRequestAndListen(t, TestBadPort, method, params...)
}

func sendWSRequestAndListen(t *testing.T, port int, method string, params ...interface{}) (chan map[string]interface{}, chan struct{}) {
	paramsFormatted := ""
	if len(params) > 0 {
		paramsFormatted = strings.Join(utils.Map(params, formatParam), ",")
	}
	body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_%s\",\"params\":[%s],\"id\":\"test\"}", method, paramsFormatted)

	headers := make(http.Header)
	headers.Set("Origin", "localhost")
	headers.Set("Content-Type", "application/json")
	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://%s:%d", TestAddr, TestWSPort), headers)
	require.Nil(t, err)

	recv := make(chan map[string]interface{})
	done := make(chan struct{})

	err = conn.WriteMessage(websocket.TextMessage, []byte(body))
	require.Nil(t, err)

	go func() {
		defer close(recv)

		// Set a read deadline to prevent blocking forever
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))

		for {
			select {
			case <-done:
				return
			case <-time.After(200 * time.Millisecond):
				_, message, err := conn.ReadMessage()
				if err != nil {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						// It was a timeout error, no data was ready to be read
						continue // Retry the read operation
					}
					recv <- map[string]interface{}{"error": err.Error()}
					return
				}
				res := map[string]interface{}{}
				err = json.Unmarshal(message, &res)
				if err != nil {
					recv <- map[string]interface{}{"error": err.Error()}
					return
				}
				recv <- res
			}
		}
	}()

	return recv, done
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
