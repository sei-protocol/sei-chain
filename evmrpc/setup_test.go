package evmrpc_test

import (
	"context"
	"crypto/sha256"
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

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/websocket"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/bytes"
	"github.com/tendermint/tendermint/libs/log"
	types2 "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/rpc/client/mock"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
	"github.com/tendermint/tendermint/version"
)

const TestAddr = "127.0.0.1"
const TestPort = 7777
const TestWSPort = 7778
const TestBadPort = 7779

const GenesisBlockHeight = 0
const MockHeight8 = 8
const MockHeight2 = 2
const MockHeight103 = 103
const MockHeight101 = 101
const MockHeight100 = 100

var DebugTraceHashHex = "0x1234567890123456789023456789012345678901234567890123456789000004"
var DebugTraceBlockHash = "0xBE17E0261E539CB7E9A91E123A6D794E0163D656FCF9B8EAC07823F7ED28512B"
var DebugTracePanicBlockHash = "0x0000000000000000000000000000000000000000000000000000000000000003"
var MultiTxBlockHash = "0x0000000000000000000000000000000000000000000000000000000000000002"

var TestCosmosTxHash = "690D39ADF56D4C811B766DFCD729A415C36C4BFFE80D63E305373B9518EBFB14"
var TestEvmTxHash = "0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e"

var TestNonPanicTxHash = "0x566f1c956c74b089643a1e6f880ac65745de0e5cd8cfc3c7482d20a486576219"
var TestPanicTxHash = "0x0ea197de8403de9c2e8cf9ec724e43734e9dbd3a8294a09d031acd67914b73e4"
var TestBlockHash = "0x0000000000000000000000000000000000000000000000000000000000000001"

var EncodingConfig = app.MakeEncodingConfig()
var TxConfig = EncodingConfig.TxConfig
var Encoder = TxConfig.TxEncoder()
var Decoder = TxConfig.TxDecoder()
var Tx1 sdk.Tx
var MultiTxBlockTx1 sdk.Tx
var MultiTxBlockTx2 sdk.Tx
var MultiTxBlockTx3 sdk.Tx
var MultiTxBlockTx4 sdk.Tx
var MultiTxBlockSynthTx sdk.Tx
var tx1 *ethtypes.Transaction
var multiTxBlockTx1 *ethtypes.Transaction
var multiTxBlockTx2 *ethtypes.Transaction
var multiTxBlockTx3 *ethtypes.Transaction
var multiTxBlockTx4 *ethtypes.Transaction
var multiTxBlockSynthTx *ethtypes.Transaction

var DebugTraceTx sdk.Tx
var DebugTracePanicTx sdk.Tx
var DebugTraceNonPanicTx sdk.Tx
var TxNonEvm sdk.Tx
var TxNonEvmWithSyntheticLog sdk.Tx
var UnconfirmedTx sdk.Tx

var SConfig = evmrpc.SimulateConfig{GasCap: 10000000}

var filterTimeoutDuration = 500 * time.Millisecond
var TotalTxCount int = 11

var MockBlockID = tmtypes.BlockID{
	Hash: bytes.HexBytes(mustHexToBytes(TestBlockHash[2:])),
}

var MockBlockIDMultiTx = tmtypes.BlockID{
	Hash: bytes.HexBytes(mustHexToBytes(MultiTxBlockHash[2:])),
}

var NewHeadsCalled = make(chan struct{})

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
	if height == MockHeight2 {
		return &coretypes.ResultBlock{
			BlockID: MockBlockIDMultiTx,
			Block: &tmtypes.Block{
				Header: mockBlockHeader(height),
				Data: tmtypes.Data{
					Txs: []tmtypes.Tx{
						func() []byte {
							bz, _ := Encoder(TxNonEvm) // non evm tx
							return bz
						}(),
						func() []byte {
							bz, _ := Encoder(MultiTxBlockTx1)
							return bz
						}(),
						func() []byte {
							bz, _ := Encoder(TxNonEvm) // non evm tx
							return bz
						}(),
						func() []byte {
							bz, _ := Encoder(MultiTxBlockTx2)
							return bz
						}(),
						func() []byte {
							bz, _ := Encoder(MultiTxBlockTx3)
							return bz
						}(),
						func() []byte {
							bz, _ := Encoder(MultiTxBlockSynthTx)
							return bz
						}(),
					},
				},
				LastCommit: &tmtypes.Commit{
					Height: height,
				},
			},
		}
	}
	if height == MockHeight100 {
		return &coretypes.ResultBlock{
			BlockID: MockBlockID,
			Block: &tmtypes.Block{
				Header: mockBlockHeader(height),
				Data: tmtypes.Data{
					Txs: []tmtypes.Tx{
						func() []byte {
							bz, _ := Encoder(MultiTxBlockSynthTx)
							return bz
						}(),
						func() []byte {
							bz, _ := Encoder(MultiTxBlockTx1)
							return bz
						}(),
					},
				},
			},
		}
	}
	res := &coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(height),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{
					func() []byte {
						bz, _ := Encoder(Tx1)
						return bz
					}(),
					func() []byte {
						bz, _ := Encoder(TxNonEvmWithSyntheticLog)
						return bz
					}(),
				},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}
	if height == MockHeight101 {
		res.Block.Data.Txs = []tmtypes.Tx{
			func() []byte {
				bz, _ := Encoder(DebugTraceTx)
				return bz
			}(),
		}
	}
	if height == MockHeight103 {
		res.Block.Data.Txs = []tmtypes.Tx{
			func() []byte {
				bz, _ := Encoder(DebugTraceNonPanicTx) // reuse the same tx for non-panic tx
				return bz
			}(),
			func() []byte {
				bz, _ := Encoder(DebugTracePanicTx)
				return bz
			}(),
		}
	}
	return res
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
	if h == nil {
		return c.mockBlock(MockHeight8), nil
	}
	return c.mockBlock(*h), nil
}

func (c *MockClient) BlockByHash(_ context.Context, hash bytes.HexBytes) (*coretypes.ResultBlock, error) {
	if hash.String() == DebugTraceBlockHash[2:] {
		return c.mockBlock(MockHeight101), nil
	}
	if hash.String() == DebugTracePanicBlockHash[2:] {
		return c.mockBlock(MockHeight103), nil
	}
	if hash.String() == MultiTxBlockHash[2:] {
		return c.mockBlock(MockHeight2), nil
	}
	return c.mockBlock(MockHeight8), nil
}

func (c *MockClient) BlockResults(_ context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
	if *height == GenesisBlockHeight {
		return &coretypes.ResultBlockResults{
			TxsResults: []*abci.ExecTxResult{
				{
					Data:      []byte{},
					GasWanted: 0,
					GasUsed:   0,
				},
			},
			ConsensusParamUpdates: &types2.ConsensusParams{
				Block: &types2.BlockParams{
					MaxBytes: 100000000,
					MaxGas:   200000000,
				},
			},
		}, nil
	}
	if *height == MockHeight103 {
		TxResults := []*abci.ExecTxResult{
			{
				Data: func() []byte {
					bz, _ := Encoder(DebugTraceTx)
					return bz
				}(),
				GasWanted: 10,
				GasUsed:   5,
			},
			{
				Data: func() []byte {
					bz, _ := Encoder(DebugTracePanicTx)
					return bz
				}(),
				GasWanted: 10,
				GasUsed:   5,
			},
		}
		return &coretypes.ResultBlockResults{TxsResults: TxResults, ConsensusParamUpdates: &types2.ConsensusParams{
			Block: &types2.BlockParams{
				MaxGas: 100000000,
			},
		}}, nil
	}
	return &coretypes.ResultBlockResults{
		TxsResults: []*abci.ExecTxResult{
			{
				Data: func() []byte {
					bz, _ := Encoder(MultiTxBlockTx1)
					return bz
				}(),
				GasWanted: 10,
				GasUsed:   5,
			},
		},
		ConsensusParamUpdates: &types2.ConsensusParams{
			Block: &types2.BlockParams{
				MaxBytes: 100000000,
				MaxGas:   200000000,
			},
		},
	}, nil
}

func (c *MockClient) Subscribe(ctx context.Context, subscriber string, query string, outCapacity ...int) (<-chan coretypes.ResultEvent, error) {
	if query == "tm.event = 'NewBlockHeader'" {
		resCh := make(chan coretypes.ResultEvent, 5)
		go func() {
			<-NewHeadsCalled
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
			cursor = MockHeight8
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

func (c *MockClient) Tx(context.Context, bytes.HexBytes, bool) (*coretypes.ResultTx, error) {
	return &coretypes.ResultTx{Hash: bytes.HexBytes(TestCosmosTxHash), Height: MockHeight8, TxResult: abci.ExecTxResult{EvmTxInfo: &abci.EvmTxInfo{TxHash: TestEvmTxHash}}}, nil
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
var MultiTxCtx sdk.Context

func init() {
	types.RegisterInterfaces(EncodingConfig.InterfaceRegistry)
	testApp := app.Setup(false, false)
	Ctx = testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8)
	MultiTxCtx, _ = Ctx.CacheContext()
	EVMKeeper = &testApp.EvmKeeper
	EVMKeeper.InitGenesis(Ctx, *evmtypes.DefaultGenesis())
	seiAddr, err := sdk.AccAddressFromHex(common.Bytes2Hex([]byte("seiAddr")))
	if err != nil {
		panic(err)
	}
	err = testApp.BankKeeper.MintCoins(Ctx, "evm", sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	err = testApp.BankKeeper.SendCoinsFromModuleToAccount(Ctx, "evm", seiAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	testApp.Commit(context.Background())
	ctxProvider := func(height int64) sdk.Context {
		if height == MockHeight2 {
			return MultiTxCtx
		}
		return Ctx
	}
	// Start good http server
	goodConfig := evmrpc.DefaultConfig
	goodConfig.HTTPPort = TestPort
	goodConfig.WSPort = TestWSPort
	goodConfig.FilterTimeout = 500 * time.Millisecond
	goodConfig.MaxLogNoBlock = 4
	infoLog, err := log.NewDefaultLogger("text", "info")
	if err != nil {
		panic(err)
	}
	HttpServer, err := evmrpc.NewEVMHTTPServer(infoLog, goodConfig, &MockClient{}, EVMKeeper, ctxProvider, TxConfig, "", isPanicTxFunc)
	if err != nil {
		panic(err)
	}
	if err := HttpServer.Start(); err != nil {
		panic(err)
	}

	// Start bad http server
	badConfig := evmrpc.DefaultConfig
	badConfig.HTTPPort = TestBadPort
	badConfig.FilterTimeout = 500 * time.Millisecond
	badHTTPServer, err := evmrpc.NewEVMHTTPServer(infoLog, badConfig, &MockBadClient{}, EVMKeeper, ctxProvider, TxConfig, "", nil)
	if err != nil {
		panic(err)
	}
	if err := badHTTPServer.Start(); err != nil {
		panic(err)
	}

	// Start ws server
	wsServer, err := evmrpc.NewEVMWebSocketServer(infoLog, goodConfig, &MockClient{}, EVMKeeper, ctxProvider, TxConfig, "")
	if err != nil {
		panic(err)
	}
	if err := wsServer.Start(); err != nil {
		panic(err)
	}
	fmt.Printf("wsServer started with config = %+v\n", goodConfig)
	time.Sleep(1 * time.Second)

	// Generate data
	generateTxData()

	// Setup logs
	setupLogs()
}

func generateTxData() {
	chainId := big.NewInt(config.DefaultChainID)
	to := common.HexToAddress("010203")
	var txBuilder1, txBuilder1_5, txBuilder2, txBuilder3, txBuilder4, synthTxBuilder client.TxBuilder
	txBuilder1, tx1 = buildTx(ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainId,
	})
	txBuilder1_5, multiTxBlockTx1 = buildTx(ethtypes.DynamicFeeTx{
		Nonce:     2,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainId,
	})
	txBuilder2, multiTxBlockTx2 = buildTx(ethtypes.DynamicFeeTx{
		Nonce:     3,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainId,
	})
	txBuilder3, multiTxBlockTx3 = buildTx(ethtypes.DynamicFeeTx{
		Nonce:     4,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainId,
	})
	txBuilder4, multiTxBlockTx4 = buildTx(ethtypes.DynamicFeeTx{
		Nonce:     5,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainId,
	})
	synthTxBuilder, multiTxBlockSynthTx = buildTx(ethtypes.DynamicFeeTx{
		Nonce:     6,
		GasFeeCap: big.NewInt(20),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("synthetic"),
		ChainID:   chainId,
	})
	debugTraceTxBuilder, _ := buildTx(ethtypes.DynamicFeeTx{
		Nonce:     0,
		GasFeeCap: big.NewInt(10),
		Gas:       22000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainId,
	})
	debugTraceNonPanicTxBuilder, _ := buildTx(ethtypes.DynamicFeeTx{
		Nonce:     0,
		GasFeeCap: big.NewInt(10),
		Gas:       22000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainId,
	})
	debugTracePanicTxBuilder, _ := buildTx(ethtypes.DynamicFeeTx{
		Nonce:     100, // set a high nonce to make sure it will panic upon simulation
		GasFeeCap: big.NewInt(10),
		Gas:       22000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainId,
	})
	Tx1 = txBuilder1.GetTx()
	MultiTxBlockTx1 = txBuilder1_5.GetTx()
	MultiTxBlockTx2 = txBuilder2.GetTx()
	MultiTxBlockTx3 = txBuilder3.GetTx()
	MultiTxBlockTx4 = txBuilder4.GetTx()
	MultiTxBlockSynthTx = synthTxBuilder.GetTx()
	DebugTraceTx = debugTraceTxBuilder.GetTx()
	DebugTracePanicTx = debugTracePanicTxBuilder.GetTx()
	DebugTraceNonPanicTx = debugTraceNonPanicTxBuilder.GetTx()
	TxNonEvm = app.TestTx{}
	TxNonEvmWithSyntheticLog = app.TestTx{}
	bloomTx1 := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: []*ethtypes.Log{{
		Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Topics: []common.Hash{common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111112")},
	}}}})
	if err := EVMKeeper.MockReceipt(Ctx, tx1.Hash(), &types.Receipt{
		From:              "0x1234567890123456789012345678901234567890",
		To:                "0x1234567890123456789012345678901234567890",
		TransactionIndex:  0,
		BlockNumber:       MockHeight8,
		TxType:            1,
		ContractAddress:   "0x1234567890123456789012345678901234567890",
		CumulativeGasUsed: 124,
		TxHashHex:         tx1.Hash().Hex(),
		GasUsed:           56,
		Status:            0,
		EffectiveGasPrice: 100000000000,
		Logs: []*types.Log{{
			Address: "0x1111111111111111111111111111111111111111",
			Topics:  []string{"0x1111111111111111111111111111111111111111111111111111111111111111", "0x1111111111111111111111111111111111111111111111111111111111111112"},
		}},
		LogsBloom: bloomTx1[:],
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
	debugTraceAddr := common.HexToAddress("0x5B4eba929F3811980f5AE0c5D04fa200f837DF4E")
	amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(1000000)))
	balanceAmts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(1000)))
	debugTraceAmts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(100000)))

	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, amts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(Ctx, types.ModuleName, sdk.AccAddress(unassociatedAddr[:]), balanceAmts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(Ctx, types.ModuleName, sdk.AccAddress(debugTraceAddr[:]), debugTraceAmts)
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
	unconfirmedTxBuilder, _ := buildTx(ethtypes.DynamicFeeTx{
		Nonce:     2,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(2000),
		Data:      []byte("abc"),
		ChainID:   chainId,
	})
	UnconfirmedTx = unconfirmedTxBuilder.GetTx()

	tracerTestTxFrom := common.HexToAddress("0x5b4eba929f3811980f5ae0c5d04fa200f837df4e")
	EVMKeeper.SetAddressMapping(Ctx, sdk.AccAddress(tracerTestTxFrom[:]), tracerTestTxFrom)
}

func buildTx(txData ethtypes.DynamicFeeTx) (client.TxBuilder, *ethtypes.Transaction) {
	chainId := big.NewInt(config.DefaultChainID)
	mnemonic := "fish mention unlock february marble dove vintage sand hub ordinary fade found inject room embark supply fabric improve spike stem give current similar glimpse"
	derivedPriv, _ := hd.Secp256k1.Derive()(mnemonic, "", "")
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	ethCfg := types.DefaultChainConfig().EthereumConfig(chainId)
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(Ctx.BlockHeight()), uint64(Ctx.BlockTime().Unix()))
	tx := ethtypes.NewTx(&txData)
	tx, err := ethtypes.SignTx(tx, signer, key)
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
	builder := TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		panic(err)
	}
	return builder, tx
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
	CtxMultiTx := Ctx.WithBlockHeight(MockHeight2)
	EVMKeeper.MockReceipt(CtxMultiTx, multiTxBlockTx1.Hash(), &types.Receipt{
		BlockNumber:      MockHeight2,
		TransactionIndex: 1, // start at 1 bc 0 is the non-evm tx
		TxHashHex:        multiTxBlockTx1.Hash().Hex(),
		LogsBloom:        bloom1[:],
		Logs: []*types.Log{{
			Address: "0x1111111111111111111111111111111111111112",
			Topics:  []string{"0x0000000000000000000000000000000000000000000000000000000000000123", "0x0000000000000000000000000000000000000000000000000000000000000456"},
		}, {
			Address: "0x1111111111111111111111111111111111111112",
			Topics:  []string{"0x0000000000000000000000000000000000000000000000000000000000000123"},
		}},
		EffectiveGasPrice: 100,
	})
	bloom2 := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: []*ethtypes.Log{{
		Address: common.HexToAddress("0x1111111111111111111111111111111111111113"),
		Topics: []common.Hash{
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123"),
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000456"),
		},
	}}}})
	EVMKeeper.MockReceipt(CtxMultiTx, multiTxBlockTx2.Hash(), &types.Receipt{
		BlockNumber:      MockHeight2,
		TransactionIndex: 3,
		TxHashHex:        multiTxBlockTx2.Hash().Hex(),
		LogsBloom:        bloom2[:],
		Logs: []*types.Log{{
			Address: "0x1111111111111111111111111111111111111113",
			Topics:  []string{"0x0000000000000000000000000000000000000000000000000000000000000123", "0x0000000000000000000000000000000000000000000000000000000000000456"},
		}},
		EffectiveGasPrice: 100,
	})
	bloom3 := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: []*ethtypes.Log{{
		Address: common.HexToAddress("0x1111111111111111111111111111111111111114"),
		Topics: []common.Hash{
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123"),
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000456"),
		},
	}}}})
	EVMKeeper.MockReceipt(CtxMultiTx, multiTxBlockTx3.Hash(), &types.Receipt{
		BlockNumber:      MockHeight2,
		TransactionIndex: 4,
		TxHashHex:        multiTxBlockTx3.Hash().Hex(),
		LogsBloom:        bloom3[:],
		Logs: []*types.Log{{
			Address: "0x1111111111111111111111111111111111111114",
			Topics:  []string{"0x0000000000000000000000000000000000000000000000000000000000000123", "0x0000000000000000000000000000000000000000000000000000000000000456"},
		}},
		EffectiveGasPrice: 100,
	})
	bloom4 := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: []*ethtypes.Log{{
		Address: common.HexToAddress("0x1111111111111111111111111111111111111115"),
		Topics: []common.Hash{
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123"),
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000456"),
		},
	}}}})
	CtxMock := Ctx.WithBlockHeight(MockHeight8)
	EVMKeeper.MockReceipt(CtxMock, multiTxBlockTx4.Hash(), &types.Receipt{
		BlockNumber:      MockHeight8,
		TransactionIndex: 1,
		TxHashHex:        multiTxBlockTx4.Hash().Hex(),
		LogsBloom:        bloom4[:],
		Logs: []*types.Log{{
			Address: "0x1111111111111111111111111111111111111115",
			Topics:  []string{"0x0000000000000000000000000000000000000000000000000000000000000123", "0x0000000000000000000000000000000000000000000000000000000000000456"},
		}},
		EffectiveGasPrice: 100,
	})
	// create a receipt with a synthetic log
	bloomSynth := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: []*ethtypes.Log{{
		Address: common.HexToAddress("0x1111111111111111111111111111111111111116"),
		Topics: []common.Hash{
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000234"),
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000456"),
		},
	}}}})
	EVMKeeper.MockReceipt(CtxMock, multiTxBlockSynthTx.Hash(), &types.Receipt{
		TxType:           evmrpc.ShellEVMTxType,
		BlockNumber:      MockHeight100,
		TransactionIndex: 0,
		TxHashHex:        multiTxBlockSynthTx.Hash().Hex(),
		LogsBloom:        bloomSynth[:],
		Logs: []*types.Log{{
			Address:   "0x1111111111111111111111111111111111111116",
			Topics:    []string{"0x0000000000000000000000000000000000000000000000000000000000000234", "0x0000000000000000000000000000000000000000000000000000000000000789"},
			Synthetic: true,
		}},
		EffectiveGasPrice: 0,
	})
	CtxDebugTrace := Ctx.WithBlockHeight(MockHeight101)
	EVMKeeper.MockReceipt(CtxDebugTrace, common.HexToHash(DebugTraceHashHex), &types.Receipt{
		BlockNumber:      MockHeight101,
		TransactionIndex: 0,
		TxHashHex:        DebugTraceHashHex,
	})
	CtxDebugTracePanic := Ctx.WithBlockHeight(MockHeight103)
	EVMKeeper.MockReceipt(CtxDebugTracePanic, common.HexToHash(TestNonPanicTxHash), &types.Receipt{
		BlockNumber:      MockHeight103,
		TransactionIndex: 0,
		TxHashHex:        TestNonPanicTxHash,
	})
	EVMKeeper.MockReceipt(CtxDebugTracePanic, common.HexToHash(TestPanicTxHash), &types.Receipt{
		BlockNumber:      MockHeight103,
		TransactionIndex: 1,
		TxHashHex:        TestPanicTxHash,
	})
	txNonEvmBz, _ := Encoder(TxNonEvmWithSyntheticLog)
	txNonEvmHash := sha256.Sum256(txNonEvmBz)
	EVMKeeper.MockReceipt(CtxMultiTx, txNonEvmHash, &types.Receipt{
		BlockNumber:      MockHeight2,
		TransactionIndex: 1,
		TxHashHex:        common.Hash(txNonEvmHash).Hex(),
		LogsBloom:        bloomSynth[:],
		Logs: []*types.Log{{
			Address:   "0x1111111111111111111111111111111111111116",
			Topics:    []string{"0x0000000000000000000000000000000000000000000000000000000000000234", "0x0000000000000000000000000000000000000000000000000000000000000789"},
			Synthetic: true,
		}},
		EffectiveGasPrice: 100,
	})

	// block 2
	EVMKeeper.SetBlockBloom(MultiTxCtx, []ethtypes.Bloom{bloom1, bloom2, bloom3})

	// block 8
	bloomTx1 := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: []*ethtypes.Log{{
		Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Topics: []common.Hash{common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111112")},
	}}}})
	EVMKeeper.SetBlockBloom(Ctx, []ethtypes.Bloom{bloomSynth, bloom4, bloomTx1})
}

//nolint:deadcode
func sendRequestGood(t *testing.T, method string, params ...interface{}) map[string]interface{} {
	return sendRequest(t, TestPort, method, params...)
}

//nolint:deadcode
func sendSeiRequestGood(t *testing.T, method string, params ...interface{}) map[string]interface{} {
	return sendSeiRequest(t, TestPort, method, params...)
}

//nolint:deadcode
func sendRequestBad(t *testing.T, method string, params ...interface{}) map[string]interface{} {
	return sendRequest(t, TestBadPort, method, params...)
}

//nolint:deadcode
func sendSeiRequestBad(t *testing.T, method string, params ...interface{}) map[string]interface{} {
	return sendSeiRequest(t, TestBadPort, method, params...)
}

// nolint:deadcode
func sendRequestGoodWithNamespace(t *testing.T, namespace string, method string, params ...interface{}) map[string]interface{} {
	return sendRequestWithNamespace(t, namespace, TestPort, method, params...)
}

func sendRequest(t *testing.T, port int, method string, params ...interface{}) map[string]interface{} {
	return sendRequestWithNamespace(t, "eth", port, method, params...)
}

func sendSeiRequest(t *testing.T, port int, method string, params ...interface{}) map[string]interface{} {
	return sendRequestWithNamespace(t, "sei", port, method, params...)
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
	case map[string]map[string]interface{}:
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

func isPanicTxFunc(ctx context.Context, hash common.Hash) (bool, error) {
	if hash == common.HexToHash(TestPanicTxHash) {
		return true, nil
	}
	return false, nil
}
