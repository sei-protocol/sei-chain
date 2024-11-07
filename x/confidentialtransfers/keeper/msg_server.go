package keeper

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
	"math"
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

func (m msgServer) ApplyPendingBalance(goCtx context.Context, req *types.MsgApplyPendingBalance) (*types.MsgApplyPendingBalanceResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Check if the account exists
	address, err := sdk.AccAddressFromBech32(req.Address)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Invalid Address")
	}

	account, exists := m.Keeper.GetAccount(ctx, address, req.Denom)
	if !exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Account does not exist")
	}

	// Apply the changes to the account state
	teg := elgamal.NewTwistedElgamal()
	newAvailableBalance, err := teg.AddWithLoHi(account.AvailableBalance, account.PendingBalanceLo, account.PendingBalanceHi)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Error summing balances")
	}

	zeroCiphertextLo, err := elgamal.SubtractCiphertext(account.PendingBalanceLo, account.PendingBalanceLo)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Error zeroing pending balance lo")
	}
	zeroCiphertextHi, err := elgamal.SubtractCiphertext(account.PendingBalanceHi, account.PendingBalanceHi)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Error zeroing pending balance hi")
	}

	account.AvailableBalance = newAvailableBalance
	account.DecryptableAvailableBalance = req.NewDecryptableAvailableBalance
	account.PendingBalanceLo = zeroCiphertextLo
	account.PendingBalanceHi = zeroCiphertextHi
	account.PendingBalanceCreditCounter = 0

	// Save the changes to the account state
	m.Keeper.SetAccount(ctx, address, req.Denom, &account)

	// Emit any required events
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeApplyPendingBalance,
			sdk.NewAttribute(types.AttributeDenom, req.Denom),
			sdk.NewAttribute(types.AttributeAddress, req.Address),
		),
	})

	return &types.MsgApplyPendingBalanceResponse{}, nil
}

func (m msgServer) CloseAccount(goCtx context.Context, req *types.MsgCloseAccount) (*types.MsgCloseAccountResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Check if the account exists
	address, err := sdk.AccAddressFromBech32(req.Address)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Invalid Address")
	}

	account, exists := m.Keeper.GetAccount(ctx, address, req.Denom)
	if !exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Account does not exist")
	}

	instruction, err := req.FromProto()
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Invalid Msg")
	}

	// Validate proofs that account is all zeroed out.
	validated := zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroPendingBalanceLoProof, &account.PublicKey, account.PendingBalanceLo)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "pending balance lo must be 0")
	}

	validated = zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroPendingBalanceHiProof, &account.PublicKey, account.PendingBalanceHi)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "pending balance hi must be 0")
	}

	validated = zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroAvailableBalanceProof, &account.PublicKey, account.AvailableBalance)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "available balance must be 0")
	}

	// Delete the account
	m.Keeper.DeleteAccount(ctx, address, req.Denom)

	// Emit any required events
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeCloseAccount,
			sdk.NewAttribute(types.AttributeDenom, req.Denom),
			sdk.NewAttribute(types.AttributeAddress, req.Address),
		),
	})

	return &types.MsgCloseAccountResponse{}, nil
}

func (m msgServer) Transfer(goCtx context.Context, req *types.MsgTransfer) (*types.MsgTransferResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	instruction, err := req.FromProto()
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Invalid Msg")
	}

	// Check that sender and recipient accounts exist.
	senderAddress, err := sdk.AccAddressFromBech32(req.FromAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Invalid sender address")
	}

	recipientAddress, err := sdk.AccAddressFromBech32(req.ToAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Invalid recipient address")
	}

	senderAccount, exists := m.Keeper.GetAccount(ctx, senderAddress, req.Denom)
	if !exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "sender account does not exist")
	}

	recipientAccount, exists := m.Keeper.GetAccount(ctx, senderAddress, req.Denom)
	if !exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "recipient account does not exist")
	}

	// Check that account does not have the maximum limit of pending transactions.
	if recipientAccount.PendingBalanceCreditCounter == math.MaxUint16 {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "recipient account has too many pending transactions")
	}

	// Calculate senders new available balance.
	teg := elgamal.NewTwistedElgamal()
	newSenderBalanceCiphertext, err := teg.SubWithLoHi(senderAccount.AvailableBalance, instruction.SenderTransferAmountLo, instruction.SenderTransferAmountHi)

	// Validate proofs
	err = types.VerifyTransferProofs(instruction, &senderAccount.PublicKey, &recipientAccount.PublicKey, newSenderBalanceCiphertext)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	// Validate proofs for each auditor
	for _, auditorParams := range instruction.Auditors {
		auditorAddress, err := sdk.AccAddressFromBech32(auditorParams.Address)
		if err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Invalid auditor address")
		}

		auditorAccount, exists := m.Keeper.GetAccount(ctx, auditorAddress, instruction.Denom)
		if !exists {
			return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "auditor account does not exist")
		}

		err = types.VerifyAuditorProof(
			instruction.SenderTransferAmountLo,
			instruction.SenderTransferAmountHi,
			auditorParams,
			&senderAccount.PublicKey,
			&auditorAccount.PublicKey)

		if err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
		}
	}

	// Calculate and Update the account states.
	recipientPendingBalanceLo, err := elgamal.AddCiphertext(recipientAccount.PendingBalanceLo, instruction.RecipientTransferAmountLo)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Error adding recipient transfer amount lo")
	}

	recipientPendingBalanceHi, err := elgamal.AddCiphertext(recipientAccount.PendingBalanceHi, instruction.RecipientTransferAmountHi)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Error adding recipient transfer amount hi")
	}

	recipientAccount.PendingBalanceLo = recipientPendingBalanceLo
	recipientAccount.PendingBalanceHi = recipientPendingBalanceHi
	recipientAccount.PendingBalanceCreditCounter += 1

	senderAccount.DecryptableAvailableBalance = instruction.DecryptableBalance
	senderAccount.AvailableBalance = newSenderBalanceCiphertext

	// Save the account states
	m.Keeper.SetAccount(ctx, senderAddress, req.Denom, &senderAccount)
	m.Keeper.SetAccount(ctx, recipientAddress, req.Denom, &recipientAccount)

	// Emit any required events
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeTransfer,
			sdk.NewAttribute(types.AttributeDenom, req.Denom),
			sdk.NewAttribute(types.AttributeSender, req.FromAddress),
			sdk.NewAttribute(types.AttributeRecipient, req.ToAddress),
		),
	})

	return &types.MsgTransferResponse{}, nil
}
