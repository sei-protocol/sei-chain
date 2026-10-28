package rpc

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// A state override is refused once it names more accounts, or sets more slots of one account, than
// the limits allow; a zero limit is unbounded.
func TestOverrideLimits(t *testing.T) {
	slots := func(n int) map[common.Hash]common.Hash {
		m := make(map[common.Hash]common.Hash, n)
		for i := range n {
			m[common.Hash{byte(i)}] = common.Hash{} //nolint:gosec // G115: small test count.
		}
		return m
	}
	for _, tc := range []struct {
		name    string
		limits  overrideLimits
		state   *export.StateOverride
		wantErr error
	}{
		{name: "no override", limits: overrideLimits{accounts: 1, slots: 1}},
		{name: "within limits", limits: overrideLimits{accounts: 2, slots: 2}, state: &export.StateOverride{{1}: {State: slots(2)}, {2}: {StateDiff: slots(2)}}},
		{name: "too many accounts", limits: overrideLimits{accounts: 1}, state: &export.StateOverride{{1}: {}, {2}: {}}, wantErr: errStateOverrideTooManyAccounts},
		{name: "too many state slots", limits: overrideLimits{slots: 1}, state: &export.StateOverride{{1}: {State: slots(2)}}, wantErr: errStateOverrideTooManySlots},
		{name: "too many state diff slots", limits: overrideLimits{slots: 1}, state: &export.StateOverride{{1}: {StateDiff: slots(2)}}, wantErr: errStateOverrideTooManySlots},
		{name: "unbounded", state: &export.StateOverride{{1}: {State: slots(3)}, {2}: {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, tc.limits.check(tc.state), tc.wantErr)
		})
	}
}

// eth_call and eth_estimateGas hand the state and block overrides they are given to the backend, at
// the latest block and at a past one, and refuse an override above the limits.
func TestHandlerPassesCallOverrides(t *testing.T) {
	address := common.HexToAddress("0x7000000000000000000000000000000000000007")
	code := hexutil.Bytes{0x60, 0x00}
	number := (*hexutil.Big)(common.Big2)
	want := evmonly.CallOverrides{
		State: export.StateOverride{address: {Code: &code, StateDiff: map[common.Hash]common.Hash{{1}: {2}}}},
		Block: utils.Some(export.BlockOverrides{Number: number}),
	}
	stateArg := map[string]any{address.Hex(): map[string]any{"code": code, "stateDiff": map[string]any{common.Hash{1}.Hex(): common.Hash{2}}}}
	blockArg := map[string]any{"number": number}
	backend := pastBlocksBackend(5)
	backend.call = func(context.Context, *core.Message) (*core.ExecutionResult, error) {
		return &core.ExecutionResult{}, nil
	}
	backend.callAt = func(context.Context, *core.Message, *ethtypes.Header) (*core.ExecutionResult, error) {
		return &core.ExecutionResult{}, nil
	}
	backend.estimateGas = func(context.Context, *core.Message, uint64) (uint64, []byte, error) { return 30_000, nil, nil }
	backend.estimateGasAt = func(context.Context, *core.Message, uint64, *ethtypes.Header) (uint64, []byte, error) {
		return 30_000, nil, nil
	}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), gigaconfig.RPCConfig{
		MaxRequestBodyBytes: testRequestBodyBytes, MaxStateOverrideAccounts: 1, MaxStateOverrideSlots: 1,
	})
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	call := map[string]any{"to": address}

	for _, method := range []string{"eth_call", "eth_estimateGas"} {
		for _, block := range []string{"latest", "0x3"} {
			t.Run(method+" at "+block, func(t *testing.T) {
				backend.gotOverrides = evmonly.CallOverrides{}
				var got json.RawMessage
				require.NoError(t, client.CallContext(t.Context(), &got, method, call, block, stateArg, blockArg))
				require.Equal(t, want, backend.gotOverrides)

				tooMany := map[string]any{address.Hex(): map[string]any{}, common.Address{1}.Hex(): map[string]any{}}
				require.ErrorContains(t, client.CallContext(t.Context(), &got, method, call, block, tooMany),
					errStateOverrideTooManyAccounts.Error())
			})
		}
	}
}
