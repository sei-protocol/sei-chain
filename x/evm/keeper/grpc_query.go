package keeper

import (
	"context"
	"errors"

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
		return &types.QuerySeiAddressByEVMAddressResponse{Associated: false}, nil
	}

	return &types.QuerySeiAddressByEVMAddressResponse{SeiAddress: addr.String(), Associated: true}, nil
}

func (q Querier) EVMAddressBySeiAddress(c context.Context, req *types.QueryEVMAddressBySeiAddressRequest) (*types.QueryEVMAddressBySeiAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	if req.SeiAddress == "" {
		return nil, sdkerrors.ErrInvalidRequest
	}
	addr, found := q.Keeper.GetEVMAddress(ctx, sdk.MustAccAddressFromBech32(req.SeiAddress))
	if !found {
		return &types.QueryEVMAddressBySeiAddressResponse{Associated: false}, nil
	}

	return &types.QueryEVMAddressBySeiAddressResponse{EvmAddress: addr.Hex(), Associated: true}, nil
}

func (q Querier) StaticCall(c context.Context, req *types.QueryStaticCallRequest) (*types.QueryStaticCallResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	if req.To == "" {
		return nil, errors.New("cannot use static call to create contracts")
	}
	if ctx.GasMeter().Limit() == 0 {
		ctx = ctx.WithGasMeter(sdk.NewGasMeter(q.QueryConfig.GasLimit))
	}
	to := common.HexToAddress(req.To)
	res, err := q.Keeper.StaticCallEVM(ctx, q.Keeper.AccountKeeper().GetModuleAddress(types.ModuleName), &to, req.Data)
	if err != nil {
		return nil, err
	}
	return &types.QueryStaticCallResponse{Data: res}, nil
}
