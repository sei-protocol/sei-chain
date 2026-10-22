package v2record

import (
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// erc20Dir holds the wrapped-SEI ERC20 the v2 RPC tests deploy.
var erc20Dir = filepath.Join("..", "..", "..", "..", "..", "evmrpc", "tests")

var (
	gasPrice = big.NewInt(100_000_000_000)
	tipCap   = big.NewInt(1_000_000_000)
)

// contextLogger logs, as one LOG0 of nine words, the block context it runs
// under: TIMESTAMP, NUMBER, COINBASE, BASEFEE, PREVRANDAO, GASLIMIT, CHAINID,
// BLOCKHASH(NUMBER-1), and GASPRICE.
var contextLogger = []byte{
	0x42, 0x60, 0x00, 0x52, // TIMESTAMP at 0x00
	0x43, 0x60, 0x20, 0x52, // NUMBER at 0x20
	0x41, 0x60, 0x40, 0x52, // COINBASE at 0x40
	0x48, 0x60, 0x60, 0x52, // BASEFEE at 0x60
	0x44, 0x60, 0x80, 0x52, // PREVRANDAO at 0x80
	0x45, 0x60, 0xa0, 0x52, // GASLIMIT at 0xa0
	0x46, 0x60, 0xc0, 0x52, // CHAINID at 0xc0
	0x60, 0x01, 0x43, 0x03, 0x40, 0x60, 0xe0, 0x52, // BLOCKHASH(NUMBER-1) at 0xe0
	0x3a, 0x61, 0x01, 0x00, 0x52, // GASPRICE at 0x100
	0x61, 0x01, 0x20, 0x60, 0x00, 0xa0, // LOG0(0, 0x120)
	0x00, // STOP
}

// topicLogger emits its calldata as one LOG4 with topics 1, 2, 3, and 4.
var topicLogger = []byte{
	0x36, 0x60, 0x00, 0x60, 0x00, 0x37, // CALLDATACOPY(0, 0, CALLDATASIZE)
	0x60, 0x04, 0x60, 0x03, 0x60, 0x02, 0x60, 0x01, // topics
	0x36, 0x60, 0x00, 0xa4, // LOG4(0, CALLDATASIZE, ...)
	0x00, // STOP
}

// create2Factory deploys its calldata as init code with CREATE2 at salt 0,
// forwarding the call's value, and returns the created address.
var create2Factory = []byte{
	0x36, 0x60, 0x00, 0x60, 0x00, 0x37, // CALLDATACOPY(0, 0, CALLDATASIZE)
	0x60, 0x00, 0x36, 0x60, 0x00, 0x34, 0xf5, // CREATE2(CALLVALUE, 0, CALLDATASIZE, 0)
	0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3, // RETURN the address
}

// selfDestructor sends its balance to the caller and self-destructs.
var selfDestructor = []byte{0x33, 0xff}

// deployCode returns init code that deploys runtime.
func deployCode(runtime []byte) []byte {
	size := byte(len(runtime))
	initCode := []byte{0x60, size, 0x60, 0x0c, 0x60, 0x00, 0x39, 0x60, size, 0x60, 0x00, 0xf3}
	return append(initCode, runtime...)
}

// erc20 returns the wrapped-SEI ERC20's ABI and init code.
func erc20(t *testing.T) (abi.ABI, []byte) {
	abiFile, err := os.Open(filepath.Join(erc20Dir, "ERC20.abi"))
	require.NoError(t, err)
	defer func() { _ = abiFile.Close() }()
	parsed, err := abi.JSON(abiFile)
	require.NoError(t, err)
	bin, err := os.ReadFile(filepath.Join(erc20Dir, "ERC20.bin"))
	require.NoError(t, err)
	code, err := hex.DecodeString(strings.TrimSpace(string(bin)))
	require.NoError(t, err)
	return parsed, code
}

func pack(t *testing.T, contract abi.ABI, method string, args ...any) []byte {
	input, err := contract.Pack(method, args...)
	require.NoError(t, err)
	return input
}

// signedTx is a transaction from key whose nonce is assigned when its block
// is built.
type signedTx struct {
	key  *ecdsa.PrivateKey
	data func(nonce uint64, chainID *big.Int) ethtypes.TxData
}

func legacyTx(key *ecdsa.PrivateKey, to *common.Address, value *big.Int, input []byte, gas uint64) signedTx {
	return signedTx{key: key, data: func(nonce uint64, _ *big.Int) ethtypes.TxData {
		return &ethtypes.LegacyTx{Nonce: nonce, GasPrice: gasPrice, Gas: gas, To: to, Value: value, Data: input}
	}}
}

func accessListTx(key *ecdsa.PrivateKey, to *common.Address, value *big.Int, input []byte, gas uint64, accessList ethtypes.AccessList) signedTx {
	return signedTx{key: key, data: func(nonce uint64, chainID *big.Int) ethtypes.TxData {
		return &ethtypes.AccessListTx{ChainID: chainID, Nonce: nonce, GasPrice: gasPrice, Gas: gas, To: to, Value: value, Data: input, AccessList: accessList}
	}}
}

func dynamicFeeTx(key *ecdsa.PrivateKey, to *common.Address, value *big.Int, input []byte, gas uint64) signedTx {
	return signedTx{key: key, data: func(nonce uint64, chainID *big.Int) ethtypes.TxData {
		return &ethtypes.DynamicFeeTx{ChainID: chainID, Nonce: nonce, GasTipCap: tipCap, GasFeeCap: gasPrice, Gas: gas, To: to, Value: value, Data: input}
	}}
}
