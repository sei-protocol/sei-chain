package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var _ types.QueryServer = Querier{}

// Querier defines a wrapper around the x/mint keeper providing gRPC method
// handlers.
type Querier struct {
	*Keeper
}

func NewQuerier(k *Keeper) Querier {
	return Querier{Keeper: k}
}

func (q Querier) SeiAddressByEVMAddress(c context.Context, req *types.QuerySeiAddressByEVMAddressRequest) (*types.QuerySeiAddressByEVMAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	if req.EvmAddress == "" {
		return nil, sdkerrors.ErrInvalidRequest
	}
	evmAddr := common.HexToAddress(req.EvmAddress)
	addr, found := q.Keeper.GetSeiAddress(ctx, evmAddr)
	if !found {
		return &types.QuerySeiAddressByEVMAddressResponse{SeiAddress: sdk.AccAddress(evmAddr[:]).String(), Associated: false}, nil
	}

	return &types.QuerySeiAddressByEVMAddressResponse{SeiAddress: addr.String(), Associated: true}, nil
}

func (q Querier) EVMAddressBySeiAddress(c context.Context, req *types.QueryEVMAddressBySeiAddressRequest) (*types.QueryEVMAddressBySeiAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	if req.SeiAddress == "" {
		return nil, sdkerrors.ErrInvalidRequest
	}
	seiAddr := sdk.MustAccAddressFromBech32(req.SeiAddress)
	addr, found := q.Keeper.GetEVMAddress(ctx, sdk.MustAccAddressFromBech32(req.SeiAddress))
	if !found {
		addr = common.Address{}
		addr.SetBytes(seiAddr)
		return &types.QueryEVMAddressBySeiAddressResponse{EvmAddress: addr.Hex(), Associated: false}, nil
	}

	return &types.QueryEVMAddressBySeiAddressResponse{EvmAddress: addr.Hex(), Associated: true}, nil
}
