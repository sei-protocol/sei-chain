package keeper

import (
	"context"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
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
	seiAddr, err := sdk.AccAddressFromBech32(req.SeiAddress)
	if err != nil {
		return nil, err
	}
	addr, found := q.Keeper.GetEVMAddress(ctx, seiAddr)
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
		ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, q.QueryConfig.GasLimit))
	}
	to := common.HexToAddress(req.To)
	res, err := q.Keeper.StaticCallEVM(ctx, q.Keeper.AccountKeeper().GetModuleAddress(types.ModuleName), &to, req.Data)
	if err != nil {
		return nil, err
	}
	return &types.QueryStaticCallResponse{Data: res}, nil
}

func (q Querier) Pointer(c context.Context, req *types.QueryPointerRequest) (*types.QueryPointerResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	switch req.PointerType {
	case types.PointerType_NATIVE:
		p, v, e := q.Keeper.GetERC20NativePointer(ctx, req.Pointee)
		return &types.QueryPointerResponse{
			Pointer: p.Hex(),
			Version: uint32(v),
			Exists:  e,
		}, nil
	case types.PointerType_CW20:
		p, v, e := q.Keeper.GetERC20CW20Pointer(ctx, req.Pointee)
		return &types.QueryPointerResponse{
			Pointer: p.Hex(),
			Version: uint32(v),
			Exists:  e,
		}, nil
	case types.PointerType_CW721:
		p, v, e := q.Keeper.GetERC721CW721Pointer(ctx, req.Pointee)
		return &types.QueryPointerResponse{
			Pointer: p.Hex(),
			Version: uint32(v),
			Exists:  e,
		}, nil
	case types.PointerType_ERC20:
		p, v, e := q.Keeper.GetCW20ERC20Pointer(ctx, common.HexToAddress(req.Pointee))
		return &types.QueryPointerResponse{
			Pointer: p.String(),
			Version: uint32(v),
			Exists:  e,
		}, nil
	case types.PointerType_ERC721:
		p, v, e := q.Keeper.GetCW721ERC721Pointer(ctx, common.HexToAddress(req.Pointee))
		return &types.QueryPointerResponse{
			Pointer: p.String(),
			Version: uint32(v),
			Exists:  e,
		}, nil
	default:
		return nil, errors.ErrUnsupported
	}
}

func (q Querier) PointerVersion(c context.Context, req *types.QueryPointerVersionRequest) (*types.QueryPointerVersionResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	switch req.PointerType {
	case types.PointerType_NATIVE:
		return &types.QueryPointerVersionResponse{
			Version: uint32(native.CurrentVersion),
		}, nil
	case types.PointerType_CW20:
		return &types.QueryPointerVersionResponse{
			Version: uint32(cw20.CurrentVersion),
		}, nil
	case types.PointerType_CW721:
		return &types.QueryPointerVersionResponse{
			Version: uint32(cw721.CurrentVersion),
		}, nil
	case types.PointerType_ERC20:
		return &types.QueryPointerVersionResponse{
			Version:  uint32(erc20.CurrentVersion),
			CwCodeId: q.GetStoredPointerCodeID(ctx, types.PointerType_ERC20),
		}, nil
	case types.PointerType_ERC721:
		return &types.QueryPointerVersionResponse{
			Version:  uint32(erc721.CurrentVersion),
			CwCodeId: q.GetStoredPointerCodeID(ctx, types.PointerType_ERC721),
		}, nil
	default:
		return nil, errors.ErrUnsupported
	}
}
