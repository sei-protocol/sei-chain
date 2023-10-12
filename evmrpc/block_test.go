package evmrpc

import (
	"context"
	"encoding/hex"
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

const BlockTestPort = 7779
const BadBlockTestPort = 7780

var EncodingConfig = app.MakeEncodingConfig()
var TxConfig = EncodingConfig.TxConfig
var Encoder = TxConfig.TxEncoder()
var Decoder = TxConfig.TxDecoder()
var Tx sdk.Tx

type TestTx struct{ msg sdk.Msg }

func (t TestTx) ValidateBasic() error { return nil }
func (t TestTx) GetMsgs() []sdk.Msg   { return []sdk.Msg{t.msg} }

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

func (c *MockClient) Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error) {
	return c.mockBlock(), nil
}

func (c *MockClient) BlockByHash(ctx context.Context, hash bytes.HexBytes) (*coretypes.ResultBlock, error) {
	return c.mockBlock(), nil
}

func (c *MockClient) BlockResults(ctx context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
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

type MockBadClient struct {
	MockClient
}

func (m *MockBadClient) Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error) {
	return nil, errors.New("error block")
}

func (m *MockBadClient) BlockByHash(ctx context.Context, hash bytes.HexBytes) (*coretypes.ResultBlock, error) {
	return nil, errors.New("error block")
}

func (m *MockBadClient) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	return nil, errors.New("error genesis")
}

var EVMKeeper *keeper.Keeper
var Ctx sdk.Context

func init() {
	EVMKeeper, _, Ctx = keeper.MockEVMKeeper()
	httpServer, err := NewEVMHTTPServer(log.NewNopLogger(), TestAddr, BlockTestPort, rpc.DefaultHTTPTimeouts, &MockClient{}, EVMKeeper, func() sdk.Context { return Ctx }, Decoder)
	if err != nil {
		panic(err)
	}
	if err := httpServer.Start(); err != nil {
		panic(err)
	}
	badHttpServer, err := NewEVMHTTPServer(log.NewNopLogger(), TestAddr, BadBlockTestPort, rpc.DefaultHTTPTimeouts, &MockBadClient{}, EVMKeeper, func() sdk.Context { return Ctx }, Decoder)
	if err != nil {
		panic(err)
	}
	if err := badHttpServer.Start(); err != nil {
		panic(err)
	}
	time.Sleep(1 * time.Second)
}

func TestGetBlockByHash(t *testing.T) {
	types.RegisterInterfaces(EncodingConfig.InterfaceRegistry)

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
	tx, err := ethtypes.SignTx(tx, signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(msg)
	Tx = b.GetTx()
	require.Nil(t, EVMKeeper.SetReceipt(Ctx, tx.Hash(), &types.Receipt{
		From:             "56789",
		TransactionIndex: 5,
	}))

	body := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockByHash\",\"params\":[\"0x0000000000000000000000000000000000000000000000000000000000000001\",true],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, BlockTestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":{\"difficulty\":\"0x0\",\"extraData\":\"0x\",\"gasLimit\":\"0xa\",\"gasUsed\":\"0x5\",\"hash\":\"0x0000000000000000000000000000000000000000000000000000000000000001\",\"logsBloom\":\"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000\",\"miner\":\"0x0000000000000000000000000000000000000005\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"nonce\":\"0x0000000000000000\",\"number\":\"0x8\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000006\",\"receiptsRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000004\",\"sha3Uncles\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"size\":\"0x260\",\"stateRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000003\",\"timestamp\":\"0x65254651\",\"transactions\":[{\"blockHash\":\"0x0000000000000000000000000000000000000000000000000000000000000001\",\"blockNumber\":\"0x8\",\"from\":\"0x0000000000000000000000000000000000056789\",\"gas\":\"0x3e8\",\"gasPrice\":\"0xa\",\"maxFeePerGas\":\"0xa\",\"maxPriorityFeePerGas\":\"0x0\",\"hash\":\"0x78b0bd7fe9ccc8ae8a61eae9315586cf2a406dacf129313e6c5769db7cd14372\",\"input\":\"0x616263\",\"nonce\":\"0x1\",\"to\":\"0x0000000000000000000000000000000000010203\",\"transactionIndex\":\"0x5\",\"value\":\"0x3e8\",\"type\":\"0x0\",\"accessList\":[],\"chainId\":\"0x1\",\"v\":\"0x1\",\"r\":\"0x34125c09c6b1a57f5f571a242572129057b22612dd56ee3519c4f68bece0db03\",\"s\":\"0x3f4fe6f2512219bac6f9b4e4be1aa11d3ef79c5c2f1000ef6fa37389de0ff523\",\"yParity\":\"0x1\"}],\"transactionsRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000002\",\"uncles\":[]}}\n", string(resBody))
}

func TestGetBlockByNumber(t *testing.T) {
	types.RegisterInterfaces(EncodingConfig.InterfaceRegistry)

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
	tx, err := ethtypes.SignTx(tx, signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(msg)
	Tx = b.GetTx()
	require.Nil(t, EVMKeeper.SetReceipt(Ctx, tx.Hash(), &types.Receipt{
		From:             "56789",
		TransactionIndex: 5,
	}))

	for _, num := range []string{"0x8", "earliest", "latest", "pending", "finalized", "safe"} {
		body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockByNumber\",\"params\":[\"%s\",true],\"id\":\"test\"}", num)
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, BlockTestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":{\"difficulty\":\"0x0\",\"extraData\":\"0x\",\"gasLimit\":\"0xa\",\"gasUsed\":\"0x5\",\"hash\":\"0x0000000000000000000000000000000000000000000000000000000000000001\",\"logsBloom\":\"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000\",\"miner\":\"0x0000000000000000000000000000000000000005\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"nonce\":\"0x0000000000000000\",\"number\":\"0x8\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000006\",\"receiptsRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000004\",\"sha3Uncles\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"size\":\"0x260\",\"stateRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000003\",\"timestamp\":\"0x65254651\",\"transactions\":[{\"blockHash\":\"0x0000000000000000000000000000000000000000000000000000000000000001\",\"blockNumber\":\"0x8\",\"from\":\"0x0000000000000000000000000000000000056789\",\"gas\":\"0x3e8\",\"gasPrice\":\"0xa\",\"maxFeePerGas\":\"0xa\",\"maxPriorityFeePerGas\":\"0x0\",\"hash\":\"0x78b0bd7fe9ccc8ae8a61eae9315586cf2a406dacf129313e6c5769db7cd14372\",\"input\":\"0x616263\",\"nonce\":\"0x1\",\"to\":\"0x0000000000000000000000000000000000010203\",\"transactionIndex\":\"0x5\",\"value\":\"0x3e8\",\"type\":\"0x0\",\"accessList\":[],\"chainId\":\"0x1\",\"v\":\"0x1\",\"r\":\"0x34125c09c6b1a57f5f571a242572129057b22612dd56ee3519c4f68bece0db03\",\"s\":\"0x3f4fe6f2512219bac6f9b4e4be1aa11d3ef79c5c2f1000ef6fa37389de0ff523\",\"yParity\":\"0x1\"}],\"transactionsRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000002\",\"uncles\":[]}}\n", string(resBody))
	}

	body := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockByNumber\",\"params\":[\"bad_num\",true],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, BlockTestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"error\":{\"code\":-32602,\"message\":\"invalid argument 0: hex string without 0x prefix\"}}\n", string(resBody))
}

func TestGetBlockTransactionCount(t *testing.T) {
	types.RegisterInterfaces(EncodingConfig.InterfaceRegistry)

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
	tx, err := ethtypes.SignTx(tx, signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(msg)
	Tx = b.GetTx()
	require.Nil(t, EVMKeeper.SetReceipt(Ctx, tx.Hash(), &types.Receipt{
		From:             "56789",
		TransactionIndex: 5,
	}))

	// get by block number
	for _, num := range []string{"0x8", "earliest", "latest", "pending", "finalized", "safe"} {
		body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockTransactionCountByNumber\",\"params\":[\"%s\"],\"id\":\"test\"}", num)
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, BlockTestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":\"0x1\"}\n", string(resBody))
	}

	// get error returns null
	body := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockTransactionCountByNumber\",\"params\":[\"0x8\"],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, BadBlockTestPort), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":null}\n", string(resBody))
	body = "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockTransactionCountByNumber\",\"params\":[\"earliest\"],\"id\":\"test\"}"
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, BadBlockTestPort), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":null}\n", string(resBody))
	body = "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockTransactionCountByHash\",\"params\":[\"0x0000000000000000000000000000000000000000000000000000000000000001\"],\"id\":\"test\"}"
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, BadBlockTestPort), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":null}\n", string(resBody))

	// get by hash
	body = "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockTransactionCountByHash\",\"params\":[\"0x0000000000000000000000000000000000000000000000000000000000000001\"],\"id\":\"test\"}"
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, BlockTestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":\"0x1\"}\n", string(resBody))
}
