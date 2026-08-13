package common

import (
	"errors"
	"time"

	gogotypes "github.com/gogo/protobuf/types"

	"github.com/sei-protocol/sei-chain/precompiles/utils"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	authztypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
)

// GrantGenericAuthorizations creates one scoped grant for each message type.
// Cosmos authz keys grants by message type, so a single EVM-facing permission
// that covers several actions must be represented by several native grants.
func GrantGenericAuthorizations(
	ctx sdk.Context,
	msgServer utils.AuthzMsgServer,
	granter sdk.AccAddress,
	grantee sdk.AccAddress,
	expiration time.Time,
	authorizedMsgs ...sdk.Msg,
) error {
	authorizations := make([]authztypes.Authorization, len(authorizedMsgs))
	for i, authorizedMsg := range authorizedMsgs {
		authorizations[i] = authztypes.NewGenericAuthorization(sdk.MsgTypeURL(authorizedMsg))
	}
	return GrantAuthorizations(ctx, msgServer, granter, grantee, expiration, authorizations...)
}

// GrantAuthorizations creates one native grant for each authorization.
func GrantAuthorizations(
	ctx sdk.Context,
	msgServer utils.AuthzMsgServer,
	granter sdk.AccAddress,
	grantee sdk.AccAddress,
	expiration time.Time,
	authorizations ...authztypes.Authorization,
) error {
	if len(authorizations) == 0 {
		return sdkerrors.ErrInvalidRequest.Wrap("at least one authorization is required")
	}
	if err := validateAuthorizationExpiration(ctx, expiration); err != nil {
		return err
	}

	for _, authorization := range authorizations {
		grant, err := authztypes.NewMsgGrant(granter, grantee, authorization, expiration)
		if err != nil {
			return err
		}
		if err := grant.ValidateBasic(); err != nil {
			return err
		}
		if _, err := msgServer.Grant(sdk.WrapSDKContext(ctx), grant); err != nil {
			return err
		}
	}

	return nil
}

// validateAuthorizationExpiration keeps the user-provided time within the
// protobuf Timestamp range used to marshal native authz grants.
func validateAuthorizationExpiration(ctx sdk.Context, expiration time.Time) error {
	if !expiration.After(ctx.BlockTime()) {
		return sdkerrors.ErrInvalidRequest.Wrap("authorization expiration must be after the current block time")
	}
	if _, err := gogotypes.TimestampProto(expiration); err != nil {
		return sdkerrors.ErrInvalidRequest.Wrapf("authorization expiration cannot be encoded as a protobuf timestamp: %v", err)
	}
	return nil
}

// ExecuteAuthorization routes a concrete message through the native authz
// server, preserving its message-type scope and normal grant consumption.
func ExecuteAuthorization(ctx sdk.Context, msgServer utils.AuthzMsgServer, grantee sdk.AccAddress, msg sdk.Msg) (*authztypes.MsgExecResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	exec := authztypes.NewMsgExec(grantee, []sdk.Msg{msg})
	if err := exec.ValidateBasic(); err != nil {
		return nil, err
	}
	return msgServer.Exec(sdk.WrapSDKContext(ctx), &exec)
}

// RevokeAuthorizations removes every extant message-type grant in a shared
// EVM-facing permission. Missing members are ignored so grants created before
// a permission was expanded can still be revoked as a group.
func RevokeAuthorizations(
	ctx sdk.Context,
	msgServer utils.AuthzMsgServer,
	granter sdk.AccAddress,
	grantee sdk.AccAddress,
	authorizedMsgs ...sdk.Msg,
) error {
	if len(authorizedMsgs) == 0 {
		return sdkerrors.ErrInvalidRequest.Wrap("at least one authorized message is required")
	}

	revoked := false
	for _, authorizedMsg := range authorizedMsgs {
		revoke := authztypes.NewMsgRevoke(granter, grantee, sdk.MsgTypeURL(authorizedMsg))
		if err := revoke.ValidateBasic(); err != nil {
			return err
		}
		if _, err := msgServer.Revoke(sdk.WrapSDKContext(ctx), &revoke); err != nil {
			if errors.Is(err, sdkerrors.ErrNotFound) {
				continue
			}
			return err
		}
		revoked = true
	}

	if !revoked {
		return sdkerrors.ErrNotFound.Wrap("authorization not found")
	}
	return nil
}
