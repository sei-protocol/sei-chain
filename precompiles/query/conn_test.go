package query_test

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	pquery "github.com/sei-protocol/sei-chain/precompiles/query"
	grpctypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/grpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const echoABI = `[
	{
		"name": "echo",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{"name": "value", "type": "uint256"}],
		"outputs": [{"name": "response", "type": "uint256"}]
	}
]`

type echoReq struct {
	Value *big.Int
}

type echoResp struct {
	Value *big.Int
}

type fakeCaller struct {
	t         *testing.T
	abi       abi.ABI
	to        common.Address
	from      common.Address
	block     *big.Int
	callCount int
}

func (f *fakeCaller) CallContract(_ context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	f.callCount++
	require.Equal(f.t, f.to, *msg.To)
	require.Equal(f.t, f.from, msg.From)
	require.Equal(f.t, f.block, blockNumber)

	method, err := f.abi.MethodById(msg.Data[:4])
	require.NoError(f.t, err)
	require.Equal(f.t, "echo", method.Name)
	args, err := method.Inputs.Unpack(msg.Data[4:])
	require.NoError(f.t, err)
	require.Len(f.t, args, 1)
	value := args[0].(*big.Int)
	return method.Outputs.Pack(new(big.Int).Mul(value, big.NewInt(2)))
}

func TestConnInvokeRoutesBindingThroughEthCall(t *testing.T) {
	contractABI, err := abi.JSON(strings.NewReader(echoABI))
	require.NoError(t, err)
	precompile := common.HexToAddress("0x0000000000000000000000000000000000000420")
	from := common.HexToAddress("0x0000000000000000000000000000000000000042")
	caller := &fakeCaller{
		t:     t,
		abi:   contractABI,
		to:    precompile,
		from:  from,
		block: big.NewInt(77),
	}

	conn := pquery.NewConn(
		caller,
		pquery.NewRegistry(pquery.Bind(pquery.Binding[echoReq, echoResp]{
			FullMethod: "/test.Query/Echo",
			Precompile: precompile,
			ABI:        contractABI,
			ABIMethod:  "echo",
			Pack: func(_ context.Context, _ *pquery.Env, req *echoReq) ([]interface{}, error) {
				return []interface{}{req.Value}, nil
			},
			Unpack: func(_ context.Context, _ *pquery.Env, _ *echoReq, out []interface{}, resp *echoResp) error {
				resp.Value = out[0].(*big.Int)
				return nil
			},
			ResponseShape: pquery.ExactProtobufShape,
		})),
		pquery.WithDefaultBlockNumber(55),
		pquery.WithDefaultFrom(from),
	)

	ctx := metadata.AppendToOutgoingContext(context.Background(), grpctypes.GRPCBlockHeightHeader, "77")
	resp := &echoResp{}
	err = conn.Invoke(ctx, "/test.Query/Echo", &echoReq{Value: big.NewInt(21)}, resp)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(42), resp.Value)
	require.Equal(t, 1, caller.callCount)
}

func TestConnInvokeRejectsUnsupportedBinding(t *testing.T) {
	conn := pquery.NewConn(&fakeCaller{}, pquery.NewRegistry())
	err := conn.Invoke(context.Background(), "/test.Query/Missing", &echoReq{}, &echoResp{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}
