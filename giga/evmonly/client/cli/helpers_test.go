package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	govprecompile "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	upgradeprecompile "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/rpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
)

// testMnemonic is the anvil test mnemonic; testKey derives anvil account 0.
const (
	testMnemonic = "test test test test test test test test test test test junk"
	testKey      = "validator"
	testChainID  = 31337
)

var (
	testSender   = common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	testGasPrice = big.NewInt(1_000_000_000)
)

// testEstimate is the gas the stub estimates for every call.
const testEstimate = 100_000

// precompileCall answers a call of one precompile method with its outputs.
type precompileCall func(args []any) ([]any, error)

// rpcStub is an EVM JSON-RPC endpoint serving the precompiles from canned
// answers and recording the transactions sent to it.
type rpcStub struct {
	t     *testing.T
	url   string
	calls map[string]precompileCall // by method signature

	mu       sync.Mutex
	sent     []*ethtypes.Transaction
	requests int
	// logs returns the logs of a sent transaction's receipt.
	logs func(*ethtypes.Transaction) []*ethtypes.Log
	// failed makes every receipt report a failed transaction.
	failed bool
	// lastCallBlock is the block argument of the last eth_call.
	lastCallBlock string
	// head is the latest block. Blocks above it do not exist unless head is 0.
	head uint64
}

func newRPCStub(t *testing.T) *rpcStub {
	s := &rpcStub{t: t, calls: map[string]precompileCall{}}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

// on answers calls of the method with signature sig.
func (s *rpcStub) on(sig string, f precompileCall) { s.calls[sig] = f }

func (s *rpcStub) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	body, _ := io.ReadAll(r.Body)
	var result any
	var rpcErr error
	if rpcErr = json.Unmarshal(body, &req); rpcErr == nil {
		s.mu.Lock()
		s.requests++
		s.mu.Unlock()
		result, rpcErr = s.handle(req.Method, req.Params)
	}
	resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if rpcErr != nil {
		resp["error"] = map[string]any{"code": 3, "message": rpcErr.Error()}
	} else {
		resp["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *rpcStub) handle(method string, params []json.RawMessage) (any, error) {
	switch method {
	case "eth_chainId":
		return hexutil.Uint64(testChainID), nil
	case "eth_getTransactionCount":
		return hexutil.Uint64(0), nil
	case "eth_gasPrice":
		return (*hexutil.Big)(testGasPrice), nil
	case "eth_estimateGas":
		return hexutil.Uint64(testEstimate), nil
	case "eth_sendRawTransaction":
		var raw hexutil.Bytes
		if err := json.Unmarshal(params[0], &raw); err != nil {
			return nil, s.fail(err)
		}
		tx := new(ethtypes.Transaction)
		if err := tx.UnmarshalBinary(raw); err != nil {
			return nil, s.fail(err)
		}
		s.mu.Lock()
		s.sent = append(s.sent, tx)
		s.mu.Unlock()
		return tx.Hash(), nil
	case "eth_getTransactionReceipt":
		return s.receipt(), nil
	case "eth_getBlockByNumber":
		n := new(big.Int).SetUint64(s.head)
		if string(params[0]) != `"latest"` {
			var h hexutil.Big
			if err := json.Unmarshal(params[0], &h); err != nil {
				return nil, s.fail(err)
			}
			n = h.ToInt()
		}
		if s.head != 0 && n.Uint64() > s.head {
			return nil, nil
		}
		return stubBlock(n), nil
	case "eth_call":
		if len(params) > 1 {
			_ = json.Unmarshal(params[1], &s.lastCallBlock)
		}
		return s.call(params[0])
	}
	return nil, s.fail(fmt.Errorf("unexpected RPC method %s", method))
}

// stubBlockHash is the hash the stub gives every block, unlike the Ethereum
// header hash of its fields.
var stubBlockHash = common.HexToHash("0xb10c")

// stubBlock is block n as eth_getBlockByNumber returns it without transactions.
func stubBlock(n *big.Int) map[string]any {
	return map[string]any{
		"number":       (*hexutil.Big)(n),
		"hash":         stubBlockHash,
		"parentHash":   common.Hash{},
		"timestamp":    hexutil.Uint64(testTime / 1_000_000_000),
		"transactions": []common.Hash{{1}},
	}
}

// fail marks the test failed without stopping the server goroutine, which
// would leave the client waiting, and returns err as the RPC error.
func (s *rpcStub) fail(err error) error {
	s.t.Errorf("rpc stub: %v", err)
	return err
}

func (s *rpcStub) receipt() *ethtypes.Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.sent[len(s.sent)-1]
	r := &ethtypes.Receipt{
		Type:        tx.Type(),
		Status:      ethtypes.ReceiptStatusSuccessful,
		TxHash:      tx.Hash(),
		GasUsed:     testEstimate,
		BlockNumber: big.NewInt(42),
		Logs:        []*ethtypes.Log{},
	}
	if s.failed {
		r.Status = ethtypes.ReceiptStatusFailed
	}
	if s.logs != nil {
		r.Logs = s.logs(tx)
	}
	return r
}

// precompileABIs are the ABIs of the precompiles a call may name.
var precompileABIs = map[common.Address]abi.ABI{
	govprecompile.Address:     govprecompile.ABI,
	upgradeprecompile.Address: upgradeprecompile.ABI,
	chainparams.Address:       chainparams.ABI,
}

func (s *rpcStub) call(raw json.RawMessage) (any, error) {
	var msg struct {
		To    common.Address `json:"to"`
		Input hexutil.Bytes  `json:"input"`
		Data  hexutil.Bytes  `json:"data"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, s.fail(err)
	}
	data := msg.Input
	if len(data) == 0 {
		data = msg.Data
	}
	contract, ok := precompileABIs[msg.To]
	if !ok {
		return nil, s.fail(fmt.Errorf("call to %s", msg.To))
	}
	m, err := contract.MethodById(data[:4])
	if err != nil {
		return nil, s.fail(err)
	}
	args, err := m.Inputs.Unpack(data[4:])
	if err != nil {
		return nil, s.fail(err)
	}
	f, ok := s.calls[m.Sig]
	if !ok {
		return nil, s.fail(fmt.Errorf("no answer for %s", m.Sig))
	}
	out, err := f(args)
	if err != nil {
		return nil, err
	}
	packed, err := m.Outputs.Pack(out...)
	if err != nil {
		return nil, s.fail(fmt.Errorf("pack %s: %w", m.Sig, err))
	}
	return hexutil.Bytes(packed), nil
}

// sentTx returns the only transaction sent.
func (s *rpcStub) sentTx() *ethtypes.Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Len(s.t, s.sent, 1)
	return s.sent[0]
}

// govCall decodes a gov precompile call, returning its method signature and
// arguments.
func govCall(t *testing.T, data []byte) (string, []any) {
	m, err := govprecompile.ABI.MethodById(data[:4])
	require.NoError(t, err)
	args, err := m.Inputs.Unpack(data[4:])
	require.NoError(t, err)
	return m.Sig, args
}

// testRoot is a seid-shaped root command with seid's tx and query module
// commands, routed by Route.
type testRoot struct {
	cmd    *cobra.Command
	out    *bytes.Buffer
	errOut *bytes.Buffer
	ctx    client.Context
}

func newTestRoot(t *testing.T) *testRoot {
	enc := app.MakeEncodingConfig()
	kr := keyring.NewInMemory()
	_, err := kr.NewAccount(testKey, testMnemonic, "", "m/44'/60'/0'/0/0", hd.Secp256k1)
	require.NoError(t, err)
	out, errOut := new(bytes.Buffer), new(bytes.Buffer)
	clientCtx := client.Context{}.
		WithCodec(enc.Marshaler).
		WithInterfaceRegistry(enc.InterfaceRegistry).
		WithTxConfig(enc.TxConfig).
		WithLegacyAmino(enc.Amino).
		WithKeyring(kr).
		WithOutput(out)

	root := &cobra.Command{
		Use:           "seid",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return client.SetCmdClientContextHandler(clientCtx, cmd)
		},
	}
	query := &cobra.Command{Use: "query", Aliases: []string{"q"}, DisableFlagParsing: true, RunE: client.ValidateCmd}
	app.ModuleBasics.AddQueryCommands(query)
	query.AddCommand(rpc.BlockCommand())
	query.PersistentFlags().String(flags.FlagChainID, "", "")
	tx := &cobra.Command{Use: "tx", DisableFlagParsing: true, RunE: client.ValidateCmd}
	app.ModuleBasics.AddTxCommands(tx)
	tx.PersistentFlags().String(flags.FlagChainID, "", "")
	root.AddCommand(query, tx)
	Route(root)
	root.SetOut(out)
	root.SetErr(errOut)
	return &testRoot{cmd: root, out: out, errOut: errOut, ctx: clientCtx}
}

// run runs seid with args, reading stdin from in.
func (r *testRoot) run(in string, args ...string) error {
	r.out.Reset()
	r.errOut.Reset()
	r.cmd.SetIn(strings.NewReader(in))
	r.cmd.SetArgs(args)
	ctx := context.WithValue(context.Background(), client.ClientContextKey, &client.Context{})
	return r.cmd.ExecuteContext(ctx)
}

// txArgs are the flags a gov tx needs against stub.
func txArgs(stub *rpcStub) []string {
	return append(askingTxArgs(stub), "--yes")
}

// askingTxArgs are the flags of a transaction that asks before broadcasting.
func askingTxArgs(stub *rpcStub) []string {
	return []string{"--from", testKey, "--evm-rpc", stub.url}
}

// args joins command words and flags.
func args(words string, extra ...string) []string {
	return append(strings.Fields(words), extra...)
}

// convertTo converts an unpacked ABI value into T.
func convertTo[T any](v any) T {
	return *abi.ConvertType(v, new(T)).(*T)
}
