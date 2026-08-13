package client

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/rpc/client/mock"
	ctypes "github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/cosmos/cosmos-sdk/client/flags"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

type MockClient struct {
	mock.Client
	err error
}

func (c MockClient) BroadcastTxCommit(ctx context.Context, tx tmtypes.Tx) (*ctypes.ResultBroadcastTxCommit, error) {
	return nil, c.err
}

func (c MockClient) BroadcastTxAsync(ctx context.Context, tx tmtypes.Tx) (*ctypes.ResultBroadcastTx, error) {
	return nil, c.err
}

func (c MockClient) BroadcastTxSync(ctx context.Context, tx tmtypes.Tx) (*ctypes.ResultBroadcastTx, error) {
	return nil, c.err
}

func CreateContextWithErrorAndMode(err error, mode string) Context {
	return Context{
		Client:        MockClient{err: err},
		BroadcastMode: mode,
	}
}

// Test the correct code is returned when
func TestBroadcastError(t *testing.T) {
	errors := map[error]uint32{
		ErrTxInCache:       sdkerrors.ErrTxInMempoolCache.ABCICode(),
		ErrTxTooLarge{}:    sdkerrors.ErrTxTooLarge.ABCICode(),
		ErrMempoolIsFull{}: sdkerrors.ErrMempoolIsFull.ABCICode(),
	}

	modes := []string{
		flags.BroadcastAsync,
		flags.BroadcastBlock,
		flags.BroadcastSync,
	}

	txBytes := []byte{0xA, 0xB}
	txHash := fmt.Sprintf("%X", sha256.Sum256(txBytes))

	for _, mode := range modes {
		for err, code := range errors {
			ctx := CreateContextWithErrorAndMode(err, mode)
			resp, returnedErr := ctx.BroadcastTx(txBytes)
			require.NoError(t, returnedErr)
			require.Equal(t, code, resp.Code)
			require.NotEmpty(t, resp.Codespace)
			require.Equal(t, txHash, resp.TxHash)
		}
	}

}
