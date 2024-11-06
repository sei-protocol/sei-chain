package keeper

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

func (m msgServer) InitializeAccount(goCtx context.Context, req *types.MsgInitializeAccount) (*types.MsgInitializeAccountResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	instruction, err := req.FromProto()
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Invalid Msg")
	}

	// Check if the account already exists
	address, err := sdk.AccAddressFromBech32(instruction.FromAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Invalid Address")
	}

	_, exists := m.Keeper.GetAccount(ctx, address, instruction.Denom)
	if exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Account already exists")
	}

	// Validate the public key
	validated := zkproofs.VerifyPubKeyValidity(*instruction.Pubkey, *instruction.Proofs.PubkeyValidityProof)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid public key")
	}

	// Validate the pending balance lo
	validated = zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroPendingBalanceLoProof, instruction.Pubkey, instruction.PendingAmountLo)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid pending balance lo")
	}

	// Validate the pending balance hi
	validated = zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroPendingBalanceHiProof, instruction.Pubkey, instruction.PendingAmountHi)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid pending balance hi")
	}

	// Validate the available balance
	validated = zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroAvailableBalanceProof, instruction.Pubkey, instruction.AvailableBalance)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid available balance")
	}

	// Create the account
	account := types.Account{
		PublicKey:                   *instruction.Pubkey,
		PendingBalanceLo:            instruction.PendingAmountLo,
		PendingBalanceHi:            instruction.PendingAmountHi,
		AvailableBalance:            instruction.AvailableBalance,
		DecryptableAvailableBalance: instruction.DecryptableBalance,
		PendingBalanceCreditCounter: 0,
	}

	// Store the account
	m.Keeper.SetAccount(ctx, address, req.Denom, &account)

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeInitializeAccount,
			sdk.NewAttribute(types.AttributeDenom, instruction.Denom),
			sdk.NewAttribute(types.AttributeAddress, instruction.FromAddress),
		),
	})

	return &types.MsgInitializeAccountResponse{}, nil
}

func (m msgServer) Deposit(goCtx context.Context, req *types.MsgDeposit) (*types.MsgDepositResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Check if the account already exists
	address, err := sdk.AccAddressFromBech32(req.FromAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Invalid Address")
	}

	_, exists := m.Keeper.GetAccount(ctx, address, instruction.Denom)
	if exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Account already exists")
	}
}
