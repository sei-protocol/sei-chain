//go:build !mock_chain_validation && !mock_block_validation

// verifyApp halts on an appHash mismatch only in the default build; a mock
// validation build swallows ErrAppHash, so this assertion is default-build only.
package statesync

import (
	"errors"
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestSyncer_verifyApp(t *testing.T) {
	const appVersion = 9
	appVersionMismatchErr := errors.New("app version mismatch. Expected: 9, got: 2")
	s := &snapshot{Height: 3, Format: 1, Chunks: 5, Hash: []byte{1, 2, 3}, trustedAppHash: []byte("app_hash")}

	const chainID = "verify-app"
	overridden := types.AppHashOverride{ChainID: chainID, Height: 3, Recorded: []byte("app_hash"), Replacement: []byte("replaced")}
	otherChain := overridden
	otherChain.ChainID = "other-chain"
	otherChain.Replacement = []byte("other")

	testcases := map[string]struct {
		response  *abci.ResponseInfo
		expectErr error
	}{
		"verified": {&abci.ResponseInfo{
			LastBlockHeight:  3,
			LastBlockAppHash: []byte("app_hash"),
			AppVersion:       appVersion,
		}, nil},
		"invalid app version": {&abci.ResponseInfo{
			LastBlockHeight:  3,
			LastBlockAppHash: []byte("app_hash"),
			AppVersion:       2,
		}, appVersionMismatchErr},
		"invalid height": {&abci.ResponseInfo{
			LastBlockHeight:  5,
			LastBlockAppHash: []byte("app_hash"),
			AppVersion:       appVersion,
		}, errVerifyFailed},
		"invalid hash": {&abci.ResponseInfo{
			LastBlockHeight:  3,
			LastBlockAppHash: []byte("xxx"),
			AppVersion:       appVersion,
		}, errVerifyFailed},
		"overridden hash": {&abci.ResponseInfo{
			LastBlockHeight:  3,
			LastBlockAppHash: []byte("replaced"),
			AppVersion:       appVersion,
		}, nil},
		"hash overridden on another chain": {&abci.ResponseInfo{
			LastBlockHeight:  3,
			LastBlockAppHash: []byte("other"),
			AppVersion:       appVersion,
		}, errVerifyFailed},
	}
	t.Cleanup(types.ReplaceAppHashOverrides([]types.AppHashOverride{overridden, otherChain}))

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			rts := setup(t, nil, nil, true)

			app := rts.conn
			app.info.Push(mkConst(tc.response))
			err := rts.reactor.syncer.verifyApp(ctx, s, chainID, appVersion)
			unwrapped := errors.Unwrap(err)
			if unwrapped != nil {
				err = unwrapped
			}
			require.Equal(t, tc.expectErr, err)
			app.AssertExpectations(t)
		})
	}
}
