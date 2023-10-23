package evmrpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/app"
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

var EncodingConfig = app.MakeEncodingConfig()
var TxConfig = EncodingConfig.TxConfig
var Encoder = TxConfig.TxEncoder()
var Decoder = TxConfig.TxDecoder()
var Tx sdk.Tx

type MockClient struct {
	mock.Client
}

func (c *MockClient) mockBlock() *coretypes.ResultBlock {
	return &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{
			Hash: bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000001")),
		},
		Block: &tmtypes.Block{
			Header: tmtypes.Header{
				ChainID:         "test",
				Height:          8,
				Time:            time.Unix(1696941649, 0),
				DataHash:        bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000002")),
				AppHash:         bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000003")),
				LastResultsHash: bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000004")),
				ProposerAddress: tmtypes.Address([]byte("0000000000000000000000000000000000000000000000000000000000000005")),
				LastBlockID: tmtypes.BlockID{
					Hash: bytes.HexBytes([]byte("0000000000000000000000000000000000000000000000000000000000000006")),
				},
			},
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{func() []byte {
					bz, _ := Encoder(Tx)
					return bz
				}()},
			},
		},
	}
}

func (c *MockClient) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	return &coretypes.ResultGenesis{Genesis: &tmtypes.GenesisDoc{InitialHeight: 1}}, nil
}

func (c *MockClient) Block(context.Context, *int64) (*coretypes.ResultBlock, error) {
	return c.mockBlock(), nil
}

func (c *MockClient) BlockByHash(context.Context, bytes.HexBytes) (*coretypes.ResultBlock, error) {
	return c.mockBlock(), nil
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
				Events: []abci.Event{{
					Type: types.EventTypeEVMLog,
					Attributes: []abci.EventAttribute{{
						Key:   []byte(types.AttributeTypeContractAddress),
						Value: []byte("0x1111111111111111111111111111111111111111111111111111111111111111"),
					}, {
						Key:   []byte(types.AttributeTypeBlockHash),
						Value: []byte("0x1111111111111111111111111111111111111111111111111111111111111111"),
					}, {
						Key:   []byte(types.AttributeTypeBlockNumber),
						Value: []byte("8"),
					}, {
						Key:   []byte(types.AttributeTypeData),
						Value: []byte("xyz"),
					}, {
						Key:   []byte(types.AttributeTypeIndex),
						Value: []byte("1"),
					}, {
						Key:   []byte(types.AttributeTypeTxIndex),
						Value: []byte("2"),
					}, {
						Key:   []byte(types.AttributeTypeRemoved),
						Value: []byte("true"),
					}, {
						Key:   []byte(types.AttributeTypeTopics),
						Value: []byte("0x1111111111111111111111111111111111111111111111111111111111111111,0x1111111111111111111111111111111111111111111111111111111111111112"),
					}, {
						Key:   []byte(types.AttributeTypeTxHash),
						Value: []byte("0x1111111111111111111111111111111111111111111111111111111111111113"),
					}},
				}},
			},
		},
	}, nil
}

func (c *MockClient) Subscribe(context.Context, string, string, ...int) (<-chan coretypes.ResultEvent, error) {
	return make(chan coretypes.ResultEvent, 1), nil
}

func (c *MockClient) BroadcastTx(context.Context, tmtypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	return &coretypes.ResultBroadcastTx{Code: 0}, nil
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
	EVMKeeper, _, Ctx = keeper.MockEVMKeeper()
	httpServer, err := NewEVMHTTPServer(log.NewNopLogger(), TestAddr, TestPort, rpc.DefaultHTTPTimeouts, &MockClient{}, EVMKeeper, func(int64) sdk.Context { return Ctx }, TxConfig)
	if err != nil {
		panic(err)
	}
	if err := httpServer.Start(); err != nil {
		panic(err)
	}
	badHTTPServer, err := NewEVMHTTPServer(log.NewNopLogger(), TestAddr, TestBadPort, rpc.DefaultHTTPTimeouts, &MockBadClient{}, EVMKeeper, func(int64) sdk.Context { return Ctx }, TxConfig)
	if err != nil {
		panic(err)
	}
	if err := badHTTPServer.Start(); err != nil {
		panic(err)
	}
	wsServer, err := NewEVMWebSocketServer(log.NewNopLogger(), TestAddr, TestWSPort, []string{"localhost"}, rpc.DefaultHTTPTimeouts)
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
	switch v := p.(type) {
	case int:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%f", v)
	case string:
		return fmt.Sprintf("\"%s\"", v)
	case []interface{}:
		return fmt.Sprintf("[%s]", strings.Join(utils.Map(v, formatParam), ","))
	default:
		return fmt.Sprintf("%s", p)
	}
}
