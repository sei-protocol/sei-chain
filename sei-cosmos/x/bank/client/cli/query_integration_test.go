package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	bankprecompile "github.com/sei-protocol/sei-chain/precompiles/bank"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	clitestutil "github.com/sei-protocol/sei-chain/sei-cosmos/testutil/cli"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	bankcli "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/client/cli"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	tmcli "github.com/sei-protocol/sei-chain/sei-tendermint/libs/cli"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	rpcclientmock "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/mock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	evmstate "github.com/sei-protocol/sei-chain/x/evm/state"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
)

func TestBankQueryCLIBackendsMatch(t *testing.T) {
	fixture := newBankQueryCLIFixture(t)
	outputJSON := fmt.Sprintf("--%s=json", tmcli.OutputFlag)

	testCases := []struct {
		name      string
		command   func() *cobra.Command
		args      []string
		expectErr bool
	}{
		{
			name:    "balances all",
			command: bankcli.GetBalancesCmd,
			args:    []string{fixture.queryAddr.String(), outputJSON},
		},
		{
			name:    "balances single denom",
			command: bankcli.GetBalancesCmd,
			args:    []string{fixture.queryAddr.String(), outputJSON, fmt.Sprintf("--%s=usei", bankcli.FlagDenom)},
		},
		{
			name:    "balances missing denom returns zero coin",
			command: bankcli.GetBalancesCmd,
			args:    []string{fixture.queryAddr.String(), outputJSON, fmt.Sprintf("--%s=umissing", bankcli.FlagDenom)},
		},
		{
			name:    "balances paginated",
			command: bankcli.GetBalancesCmd,
			args: []string{
				fixture.queryAddr.String(),
				outputJSON,
				fmt.Sprintf("--%s=1", flags.FlagLimit),
				fmt.Sprintf("--%s=true", flags.FlagCountTotal),
			},
		},
		{
			name:    "balances page key",
			command: bankcli.GetBalancesCmd,
			args: []string{
				fixture.queryAddr.String(),
				outputJSON,
				fmt.Sprintf("--%s=uhist", flags.FlagPageKey),
				fmt.Sprintf("--%s=1", flags.FlagLimit),
			},
		},
		{
			name:    "balances reverse",
			command: bankcli.GetBalancesCmd,
			args: []string{
				fixture.queryAddr.String(),
				outputJSON,
				fmt.Sprintf("--%s=2", flags.FlagLimit),
				fmt.Sprintf("--%s=true", flags.FlagCountTotal),
				fmt.Sprintf("--%s=true", flags.FlagReverse),
			},
		},
		{
			name:      "balances invalid address",
			command:   bankcli.GetBalancesCmd,
			args:      []string{"not-a-sei-address", outputJSON},
			expectErr: true,
		},
		{
			name:    "balances invalid offset and key pagination",
			command: bankcli.GetBalancesCmd,
			args: []string{
				fixture.queryAddr.String(),
				outputJSON,
				fmt.Sprintf("--%s=1", flags.FlagOffset),
				fmt.Sprintf("--%s=usei", flags.FlagPageKey),
			},
			expectErr: true,
		},
		{
			name:    "total supply",
			command: bankcli.GetCmdQueryTotalSupply,
			args:    []string{outputJSON},
		},
		{
			name:    "total supply single denom",
			command: bankcli.GetCmdQueryTotalSupply,
			args:    []string{outputJSON, fmt.Sprintf("--%s=usei", bankcli.FlagDenom)},
		},
		{
			name:    "total supply missing denom returns zero coin",
			command: bankcli.GetCmdQueryTotalSupply,
			args:    []string{outputJSON, fmt.Sprintf("--%s=umissing", bankcli.FlagDenom)},
		},
		{
			name:    "total supply paginated reverse",
			command: bankcli.GetCmdQueryTotalSupply,
			args: []string{
				outputJSON,
				fmt.Sprintf("--%s=2", flags.FlagLimit),
				fmt.Sprintf("--%s=true", flags.FlagCountTotal),
				fmt.Sprintf("--%s=true", flags.FlagReverse),
			},
		},
		{
			name:    "total supply page key",
			command: bankcli.GetCmdQueryTotalSupply,
			args: []string{
				outputJSON,
				fmt.Sprintf("--%s=uhist", flags.FlagPageKey),
				fmt.Sprintf("--%s=1", flags.FlagLimit),
			},
		},
		{
			name:    "total supply invalid offset and key pagination",
			command: bankcli.GetCmdQueryTotalSupply,
			args: []string{
				outputJSON,
				fmt.Sprintf("--%s=1", flags.FlagOffset),
				fmt.Sprintf("--%s=usei", flags.FlagPageKey),
			},
			expectErr: true,
		},
		{
			name:    "denom metadata all",
			command: bankcli.GetCmdDenomsMetadata,
			args:    []string{outputJSON},
		},
		{
			name:    "denom metadata single denom",
			command: bankcli.GetCmdDenomsMetadata,
			args:    []string{outputJSON, fmt.Sprintf("--%s=usei", bankcli.FlagDenom)},
		},
		{
			name:      "denom metadata missing denom",
			command:   bankcli.GetCmdDenomsMetadata,
			args:      []string{outputJSON, fmt.Sprintf("--%s=umissing", bankcli.FlagDenom)},
			expectErr: true,
		},
	}

	heightModes := []struct {
		name   string
		height int64
		args   []string
	}{
		{name: "latest", height: fixture.latestHeight},
		{
			name:   "with height",
			height: fixture.historicalHeight,
			args:   []string{fmt.Sprintf("--%s=%d", flags.FlagHeight, fixture.historicalHeight)},
		},
	}

	for _, tc := range testCases {
		tc := tc
		for _, mode := range heightModes {
			mode := mode
			t.Run(tc.name+"/"+mode.name, func(t *testing.T) {
				args := append([]string{}, tc.args...)
				args = append(args, mode.args...)

				fixture.legacyRPC.reset()
				legacyOut, legacyErr := fixture.exec(t, tc.command, bankcli.QueryClientLegacy, args)

				fixture.precompileRPC.reset()
				precompileOut, precompileErr := fixture.exec(t, tc.command, bankcli.QueryClientPrecompile, args)

				if tc.expectErr {
					require.Error(t, legacyErr)
					require.Error(t, precompileErr)
					return
				}

				require.NoError(t, legacyErr)
				require.NoError(t, precompileErr)
				require.JSONEq(t, legacyOut, precompileOut)
				require.Equal(t, mode.height, fixture.legacyRPC.lastHeight(t))
				require.Equal(t, mode.height, fixture.precompileRPC.lastHeight(t))
			})
		}
	}
}

func TestBankQueryCLIRejectsUnknownBackend(t *testing.T) {
	fixture := newBankQueryCLIFixture(t)
	out, err := fixture.exec(t, bankcli.GetBalancesCmd, "wat", []string{
		fixture.queryAddr.String(),
		fmt.Sprintf("--%s=json", tmcli.OutputFlag),
	})
	require.ErrorContains(t, err, `unsupported bank query client backend "wat"`)
	require.Contains(t, out, `unsupported bank query client backend "wat"`)
}

type bankQueryCLIFixture struct {
	clientCtx        client.Context
	queryAddr        sdk.AccAddress
	legacyRPC        *bankQueryRPC
	precompileRPC    *bankPrecompileRPC
	historicalHeight int64
	latestHeight     int64
}

func newBankQueryCLIFixture(t *testing.T) *bankQueryCLIFixture {
	t.Helper()

	encodingConfig := seiapp.MakeEncodingConfig()
	queryAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	historicalHeight := int64(7)
	latestHeight := int64(11)

	states := map[int64]*bankCLIState{
		historicalHeight: newBankCLIState(t, historicalHeight, queryAddr, sdk.NewCoins(
			sdk.NewCoin("uhist", sdk.NewInt(10)),
			sdk.NewCoin("usei", sdk.NewInt(20)),
		), []banktypes.Metadata{
			metadata("uhist", "hist", "Historical Coin", "HIST"),
			metadata("usei", "sei", "Sei", "SEI"),
		}),
		latestHeight: newBankCLIState(t, latestHeight, queryAddr, sdk.NewCoins(
			sdk.NewCoin("uhist", sdk.NewInt(30)),
			sdk.NewCoin("ulatest", sdk.NewInt(40)),
			sdk.NewCoin("usei", sdk.NewInt(50)),
		), []banktypes.Metadata{
			metadata("uhist", "hist", "Historical Coin", "HIST"),
			metadata("ulatest", "latest", "Latest Coin", "LATEST"),
			metadata("usei", "sei", "Sei", "SEI"),
		}),
	}

	legacyRPC := &bankQueryRPC{
		Client:       rpcclientmock.New(),
		states:       states,
		latestHeight: latestHeight,
	}
	clientCtx := client.Context{}.
		WithChainID("sei-test").
		WithCodec(encodingConfig.Marshaler).
		WithLegacyAmino(encodingConfig.Amino).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithClient(legacyRPC)

	return &bankQueryCLIFixture{
		clientCtx:        clientCtx,
		queryAddr:        queryAddr,
		legacyRPC:        legacyRPC,
		precompileRPC:    newBankPrecompileRPC(t, states, latestHeight),
		historicalHeight: historicalHeight,
		latestHeight:     latestHeight,
	}
}

func (f *bankQueryCLIFixture) exec(t *testing.T, command func() *cobra.Command, backend string, args []string) (string, error) {
	t.Helper()

	fullArgs := append([]string{}, args...)
	fullArgs = append(fullArgs, fmt.Sprintf("--%s=%s", bankcli.FlagQueryClientBackend, backend))
	if backend == bankcli.QueryClientPrecompile {
		fullArgs = append(fullArgs, fmt.Sprintf("--%s=%s", bankcli.FlagEVMRPC, f.precompileRPC.URL()))
	}

	out, err := clitestutil.ExecTestCLICmd(f.clientCtx, command(), fullArgs)
	return out.String(), err
}

type bankCLIState struct {
	app *seiapp.App
	ctx sdk.Context
}

func newBankCLIState(t *testing.T, height int64, queryAddr sdk.AccAddress, coins sdk.Coins, metadatas []banktypes.Metadata) *bankCLIState {
	t.Helper()

	testApp := seiapp.Setup(t, false, false, false)
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{
		ChainID: "sei-test",
		Height:  height,
		Time:    time.Unix(height, 0),
	}).WithEventManager(sdk.NewEventManager())

	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, queryAddr, coins))
	for _, denomMetadata := range metadatas {
		testApp.BankKeeper.SetDenomMetaData(ctx, denomMetadata)
	}
	testApp.BankKeeper.SetParams(ctx, banktypes.Params{
		SendEnabled:        []*banktypes.SendEnabled{{Denom: "usei", Enabled: true}},
		DefaultSendEnabled: false,
	})

	return &bankCLIState{app: testApp, ctx: ctx}
}

func metadata(base, display, name, symbol string) banktypes.Metadata {
	return banktypes.Metadata{
		Description: name + " description",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: base, Exponent: 0, Aliases: []string{"micro" + display}},
			{Denom: display, Exponent: 6, Aliases: []string{symbol}},
		},
		Base:    base,
		Display: display,
		Name:    name,
		Symbol:  symbol,
	}
}

type bankQueryRPC struct {
	rpcclientmock.Client

	states       map[int64]*bankCLIState
	latestHeight int64

	mu      sync.Mutex
	heights []int64
}

func (r *bankQueryRPC) ABCIQueryWithOptions(
	_ context.Context,
	path string,
	data tmbytes.HexBytes,
	opts rpcclient.ABCIQueryOptions,
) (*coretypes.ResultABCIQuery, error) {
	height := opts.Height
	if height == 0 {
		height = r.latestHeight
	}
	r.recordHeight(height)

	state, ok := r.states[height]
	if !ok {
		return r.errorResponse(height, fmt.Errorf("no test state for height %d", height)), nil
	}

	queryHelper := baseapp.NewQueryServerTestHelper(state.ctx, state.app.InterfaceRegistry())
	banktypes.RegisterQueryServer(queryHelper, state.app.BankKeeper)
	querier := queryHelper.Route(path)
	if querier == nil {
		return r.errorResponse(height, fmt.Errorf("handler not found for %s", path)), nil
	}

	res, err := querier(state.ctx, abci.RequestQuery{Path: path, Data: data, Height: height})
	if err != nil {
		return r.errorResponse(height, err), nil
	}

	return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{
		Code:   0,
		Height: height,
		Value:  res.Value,
	}}, nil
}

func (r *bankQueryRPC) errorResponse(height int64, err error) *coretypes.ResultABCIQuery {
	return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{
		Code:   sdkerrors.ErrInvalidRequest.ABCICode(),
		Height: height,
		Log:    err.Error(),
	}}
}

func (r *bankQueryRPC) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.heights = nil
}

func (r *bankQueryRPC) recordHeight(height int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.heights = append(r.heights, height)
}

func (r *bankQueryRPC) lastHeight(t *testing.T) int64 {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.heights)
	return r.heights[len(r.heights)-1]
}

type bankPrecompileRPC struct {
	server *httptest.Server

	states       map[int64]*bankCLIState
	latestHeight int64

	mu      sync.Mutex
	heights []int64
}

func newBankPrecompileRPC(t *testing.T, states map[int64]*bankCLIState, latestHeight int64) *bankPrecompileRPC {
	t.Helper()

	rpc := &bankPrecompileRPC{
		states:       states,
		latestHeight: latestHeight,
	}
	rpc.server = httptest.NewServer(http.HandlerFunc(rpc.handle))
	t.Cleanup(rpc.server.Close)
	return rpc
}

func (r *bankPrecompileRPC) URL() string {
	return r.server.URL
}

type jsonRPCRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ethCallArgs struct {
	From  *common.Address `json:"from,omitempty"`
	To    *common.Address `json:"to"`
	Data  *hexutil.Bytes  `json:"data"`
	Input *hexutil.Bytes  `json:"input"`
}

func (a ethCallArgs) callData() []byte {
	if a.Input != nil {
		return *a.Input
	}
	if a.Data != nil {
		return *a.Data
	}
	return nil
}

func (r *bankPrecompileRPC) handle(w http.ResponseWriter, req *http.Request) {
	defer func() { _ = req.Body.Close() }()
	w.Header().Set("Content-Type", "application/json")

	var rpcReq jsonRPCRequest
	if err := json.NewDecoder(req.Body).Decode(&rpcReq); err != nil {
		_ = json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", Error: &jsonRPCError{Code: -32700, Message: err.Error()}})
		return
	}
	if rpcReq.Method != "eth_call" {
		r.writeError(w, rpcReq.ID, fmt.Errorf("unexpected JSON-RPC method %s", rpcReq.Method))
		return
	}
	if len(rpcReq.Params) == 0 {
		r.writeError(w, rpcReq.ID, fmt.Errorf("missing eth_call params"))
		return
	}

	var call ethCallArgs
	if err := json.Unmarshal(rpcReq.Params[0], &call); err != nil {
		r.writeError(w, rpcReq.ID, err)
		return
	}
	height, err := r.blockHeight(rpcReq.Params)
	if err != nil {
		r.writeError(w, rpcReq.ID, err)
		return
	}
	r.recordHeight(height)

	ret, err := r.callBankPrecompile(height, call)
	if err != nil {
		r.writeError(w, rpcReq.ID, err)
		return
	}
	_ = json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: rpcReq.ID, Result: hexutil.Encode(ret)})
}

func (r *bankPrecompileRPC) blockHeight(params []json.RawMessage) (int64, error) {
	if len(params) < 2 || string(params[1]) == "null" {
		return r.latestHeight, nil
	}

	var block string
	if err := json.Unmarshal(params[1], &block); err != nil {
		return 0, err
	}
	switch block {
	case "", "latest", "pending", "safe", "finalized":
		return r.latestHeight, nil
	default:
		height, err := hexutil.DecodeBig(block)
		if err != nil {
			return 0, err
		}
		if !height.IsInt64() {
			return 0, fmt.Errorf("block height %s overflows int64", block)
		}
		return height.Int64(), nil
	}
}

func (r *bankPrecompileRPC) callBankPrecompile(height int64, call ethCallArgs) ([]byte, error) {
	if call.To == nil || *call.To != common.HexToAddress(bankprecompile.BankAddress) {
		return nil, fmt.Errorf("unexpected eth_call target %v", call.To)
	}
	state, ok := r.states[height]
	if !ok {
		return nil, fmt.Errorf("no test state for height %d", height)
	}

	precompile, err := bankprecompile.NewPrecompile(state.app.GetPrecompileKeepers())
	if err != nil {
		return nil, err
	}
	from := common.Address{}
	if call.From != nil {
		from = *call.From
	}
	evm := vm.EVM{StateDB: evmstate.NewDBImpl(state.ctx, &state.app.EvmKeeper, true)}
	ret, _, err := precompile.RunAndCalculateGas(&evm, from, from, call.callData(), 10_000_000, (*big.Int)(nil), nil, true, false)
	return ret, err
}

func (r *bankPrecompileRPC) writeError(w http.ResponseWriter, id json.RawMessage, err error) {
	_ = json.NewEncoder(w).Encode(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: -32000, Message: err.Error()},
	})
}

func (r *bankPrecompileRPC) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.heights = nil
}

func (r *bankPrecompileRPC) recordHeight(height int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.heights = append(r.heights, height)
}

func (r *bankPrecompileRPC) lastHeight(t *testing.T) int64 {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.heights)
	return r.heights[len(r.heights)-1]
}
