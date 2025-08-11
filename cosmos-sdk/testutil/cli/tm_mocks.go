package cli

import (
	"context"
	abci "github.com/sei-protocol/sei-chain/tendermint/abci/types"
	tmbytes "github.com/sei-protocol/sei-chain/tendermint/libs/bytes"
	rpcclient "github.com/sei-protocol/sei-chain/tendermint/rpc/client"
	rpcclientmock "github.com/sei-protocol/sei-chain/tendermint/rpc/client/mock"
	"github.com/sei-protocol/sei-chain/tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/tendermint/types"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/client"
)

var _ client.TendermintRPC = (*MockTendermintRPC)(nil)

type MockTendermintRPC struct {
	rpcclientmock.Client

	responseQuery abci.ResponseQuery
}

// NewMockTendermintRPC returns a mock TendermintRPC implementation.
// It is used for CLI testing.
func NewMockTendermintRPC(respQuery abci.ResponseQuery, client rpcclientmock.Client) MockTendermintRPC {
	return MockTendermintRPC{
		Client:        client,
		responseQuery: respQuery,
	}
}

func (MockTendermintRPC) BroadcastTxSync(context.Context, tmtypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	return &coretypes.ResultBroadcastTx{Code: 0}, nil
}

func (m MockTendermintRPC) ABCIQueryWithOptions(
	_ context.Context,
	_ string,
	_ tmbytes.HexBytes,
	_ rpcclient.ABCIQueryOptions,
) (*coretypes.ResultABCIQuery, error) {
	return &coretypes.ResultABCIQuery{Response: m.responseQuery}, nil
}
