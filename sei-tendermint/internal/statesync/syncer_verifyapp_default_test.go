//go:build !mock_chain_validation && !mock_block_validation

// verifyApp halts on an appHash mismatch only in the default build; a mock
// validation build swallows ErrAppHash, so this assertion is default-build only.
package statesync

import (
	"context"
	"errors"
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

func TestSyncer_verifyApp(t *testing.T) {
	boom := errors.New("boom")
	const appVersion = 9
	appVersionMismatchErr := errors.New("app version mismatch. Expected: 9, got: 2")
	s := &snapshot{Height: 3, Format: 1, Chunks: 5, Hash: []byte{1, 2, 3}, trustedAppHash: []byte("app_hash")}

	testcases := map[string]struct {
		response  *abci.ResponseInfo
		err       error
		expectErr error
	}{
		"verified": {&abci.ResponseInfo{
			LastBlockHeight:  3,
			LastBlockAppHash: []byte("app_hash"),
			AppVersion:       appVersion,
		}, nil, nil},
		"invalid app version": {&abci.ResponseInfo{
			LastBlockHeight:  3,
			LastBlockAppHash: []byte("app_hash"),
			AppVersion:       2,
		}, nil, appVersionMismatchErr},
		"invalid height": {&abci.ResponseInfo{
			LastBlockHeight:  5,
			LastBlockAppHash: []byte("app_hash"),
			AppVersion:       appVersion,
		}, nil, errVerifyFailed},
		"invalid hash": {&abci.ResponseInfo{
			LastBlockHeight:  3,
			LastBlockAppHash: []byte("xxx"),
			AppVersion:       appVersion,
		}, nil, errVerifyFailed},
		"error": {nil, boom, boom},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			rts := setup(t, nil, nil, true)

			app := rts.conn
			app.info.Push(func(_ context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
				utils.OrPanic(utils.TestDiff(&version.RequestInfo, req))
				return tc.response, tc.err
			})
			err := rts.reactor.syncer.verifyApp(ctx, s, appVersion)
			unwrapped := errors.Unwrap(err)
			if unwrapped != nil {
				err = unwrapped
			}
			require.Equal(t, tc.expectErr, err)
			app.AssertExpectations(t)
		})
	}
}
