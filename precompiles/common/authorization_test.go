package common

import (
	"bytes"
	"context"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	authztypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	"github.com/stretchr/testify/require"
)

func TestGrantAuthorizationsValidatesExpirationTimestampRange(t *testing.T) {
	ctx := sdk.Context{}.WithContext(context.Background()).WithBlockTime(time.Unix(1_700_000_000, 0).UTC())
	granter := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	grantee := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	authorization := authztypes.NewGenericAuthorization("/cosmos.gov.v1beta1.MsgVote")

	t.Run("accepts latest whole-second protobuf timestamp", func(t *testing.T) {
		msgServer := &recordingAuthzMsgServer{}
		expiration := time.Unix(253_402_300_799, 0).UTC()

		err := GrantAuthorizations(ctx, msgServer, granter, grantee, expiration, authorization)
		require.NoError(t, err)
		require.Len(t, msgServer.grants, 1)
		require.Equal(t, expiration, msgServer.grants[0].Grant.Expiration)
	})

	t.Run("rejects first second outside protobuf timestamp range", func(t *testing.T) {
		msgServer := &recordingAuthzMsgServer{}
		expiration := time.Unix(253_402_300_800, 0).UTC()

		err := GrantAuthorizations(ctx, msgServer, granter, grantee, expiration, authorization)
		require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
		require.ErrorContains(t, err, "authorization expiration cannot be encoded as a protobuf timestamp")
		require.Empty(t, msgServer.grants)
	})
}

type recordingAuthzMsgServer struct {
	grants []*authztypes.MsgGrant
}

func (s *recordingAuthzMsgServer) Grant(_ context.Context, msg *authztypes.MsgGrant) (*authztypes.MsgGrantResponse, error) {
	s.grants = append(s.grants, msg)
	return &authztypes.MsgGrantResponse{}, nil
}

func (s *recordingAuthzMsgServer) Exec(context.Context, *authztypes.MsgExec) (*authztypes.MsgExecResponse, error) {
	return &authztypes.MsgExecResponse{}, nil
}

func (s *recordingAuthzMsgServer) Revoke(context.Context, *authztypes.MsgRevoke) (*authztypes.MsgRevokeResponse, error) {
	return &authztypes.MsgRevokeResponse{}, nil
}
