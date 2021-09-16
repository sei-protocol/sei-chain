package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cosmos/ibc-go/v2/modules/core/05-port/types"
	host "github.com/cosmos/ibc-go/v2/modules/core/24-host"
)

var _ types.QueryServer = (*Keeper)(nil)

// AppVersion implements the Query/AppVersion gRPC method
func (q Keeper) AppVersion(c context.Context, req *types.QueryAppVersionRequest) (*types.QueryAppVersionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if err := validategRPCRequest(req.PortId); err != nil {
		return nil, err
	}

	ctx := sdk.UnwrapSDKContext(c)
	module, _, err := q.LookupModuleByPort(ctx, req.PortId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, sdkerrors.Wrap(err, "could not retrieve module from port-id").Error())
	}

	ibcModule, found := q.Router.GetRoute(module)
	if !found {
		return nil, status.Errorf(codes.NotFound, sdkerrors.Wrapf(types.ErrInvalidRoute, "route not found to module: %s", module).Error())
	}

	version, err := ibcModule.NegotiateAppVersion(ctx, req.Ordering, req.ConnectionId, req.PortId, *req.Counterparty, req.ProposedVersion)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, sdkerrors.Wrap(err, "version negotation failed").Error())
	}

	return types.NewQueryAppVersionResponse(req.PortId, version), nil
}

func validategRPCRequest(portID string) error {
	if err := host.PortIdentifierValidator(portID); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	return nil
}
