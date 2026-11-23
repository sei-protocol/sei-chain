package parity

import (
	"context"
	"encoding/json"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// sentRequest is a request as the reference node received it.
type sentRequest struct {
	method string
	params string
}

func recordFixture(t *testing.T, atTip bool) (*Fixture, []sentRequest) {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	to := common.HexToAddress("0x0000000000000000000000000000000000000abc")
	tx, err := ethtypes.SignNewTx(key, ethtypes.LatestSignerForChainID(big.NewInt(1)), &ethtypes.LegacyTx{To: &to, Gas: 21_000, GasPrice: big.NewInt(1)})
	require.NoError(t, err)
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)
	fixture := &Fixture{
		Name:     "record",
		ChainID:  1,
		Prestate: map[common.Address]Account{to: {Balance: new(hexutil.Big)}},
		Blocks:   []Block{{Header: Header{Number: 7}, Txs: []hexutil.Bytes{raw}}},
	}
	var sent []sentRequest
	reference := func(_ context.Context, method string, params []json.RawMessage) (Response, error) {
		parts := make([]string, len(params))
		for i, param := range params {
			parts[i] = string(param)
		}
		sent = append(sent, sentRequest{method: method, params: strings.Join(parts, ",")})
		if method == "eth_getTransactionReceipt" && strings.Contains(parts[0], tx.Hash().Hex()[2:]) {
			sender := crypto.PubkeyToAddress(key.PublicKey)
			return Response{Result: json.RawMessage(`{"from":"` + sender.Hex() + `","to":"` + to.Hex() + `","logs":[]}`)}, nil
		}
		return Response{Result: json.RawMessage("null")}, nil
	}
	require.NoError(t, Record(t.Context(), fixture, reference, RecordOptions{AtTip: atTip}))
	return fixture, sent
}

func TestRecordReplacesLatestWhenNotAtTip(t *testing.T) {
	fixture, sent := recordFixture(t, false)
	require.Len(t, sent, len(fixture.Requests))
	sawLatest := false
	for i, request := range fixture.Requests {
		for _, param := range request.Params {
			if string(param) == `"latest"` {
				sawLatest = true
				require.NotContains(t, sent[i].params, `"latest"`, request.Method)
				require.Contains(t, sent[i].params, `"0x7"`, request.Method)
			}
		}
	}
	require.True(t, sawLatest, "state requests are recorded with the latest tag")
	methods := make([]string, len(sent))
	for i, request := range sent {
		methods[i] = request.method
	}
	require.NotContains(t, methods, "eth_blockNumber", "the head is not the fixture's tip")
}

func TestRecordKeepsLatestAtTip(t *testing.T) {
	fixture, sent := recordFixture(t, true)
	for i, request := range fixture.Requests {
		require.Equal(t, joinParams(request.Params), sent[i].params)
	}
	require.True(t, slices.ContainsFunc(sent, func(r sentRequest) bool { return r.method == "eth_blockNumber" }))
}

func joinParams(params []json.RawMessage) string {
	parts := make([]string, len(params))
	for i, param := range params {
		parts[i] = string(param)
	}
	return strings.Join(parts, ",")
}
