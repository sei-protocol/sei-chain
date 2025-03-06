package keeper

import (
	"bytes"
	"context"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

type msgServer struct {
	Keeper
	*zkproofs.CachedRangeVerifierFactory
}

const (
	AddScalarDescriptor                                = "add scalar"
	AddWithLoHiDescriptor                              = "add with lo hi"
	AddCiphertextDescriptor                            = "add ciphertext"
	SubScalarDescriptor                                = "subtract scalar"
	SubCiphertextDescriptor                            = "subtract ciphertext"
	SubWithLoHiDescriptor                              = "sub with lo hi"
	PubKeyVerificationDescriptor                       = "public key verification"
	CiphertextCommitmentEqualityVerificationDescriptor = "ciphertext commitment equality verification"
	ZeroBalanceVerificationDescriptor                  = "zero balance verification"
	TransferProofVerificationDescriptor                = "transfer proof verification"
	AuditorProofVerificationDescriptor                 = "auditor proof verification"
	RangedProofVerificationDescriptor                  = "range proof verification"
)

// NewMsgServerImpl returns an implementation of the MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	ed25519RangeVerifierFactory := zkproofs.Ed25519RangeVerifierFactory{}
	rangeVerifierFactory := zkproofs.NewCachedRangeVerifierFactory(&ed25519RangeVerifierFactory)
	return msgServer{keeper, rangeVerifierFactory}
}

var _ types.MsgServer = msgServer{}

func (m msgServer) InitializeAccount(goCtx context.Context, req *types.MsgInitializeAccount) (*types.MsgInitializeAccountResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !m.Keeper.IsCtModuleEnabled(ctx) {
		return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "feature is disabled by governance")
	}

	// Convert the instruction from proto. This also validates the request.
	instruction, err := req.FromProto()
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid msg")
	}

	// Check if the account already exists
	_, exists := m.Keeper.GetAccount(ctx, req.FromAddress, instruction.Denom)
	if exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "account already exists")
	}

	// Check if denom already exists.
	denomHasSupply := m.Keeper.BankKeeper().HasSupply(ctx, instruction.Denom)
	_, denomMetadataExists := m.Keeper.BankKeeper().GetDenomMetaData(ctx, instruction.Denom)
	if !denomMetadataExists && !denomHasSupply {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "denom does not exist")
	}

	// Validate the public key
	m.consumeGasForProofVerification(ctx, PubKeyVerificationDescriptor)
	validated := zkproofs.VerifyPubKeyValidity(*instruction.Pubkey, instruction.Proofs.PubkeyValidityProof)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid public key")
	}

	// Validate the pending balance lo is zero.
	m.consumeGasForProofVerification(ctx, ZeroBalanceVerificationDescriptor)
	validated = zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroPendingBalanceLoProof, instruction.Pubkey, instruction.PendingBalanceLo)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid pending balance lo")
	}

	// Validate the pending balance hi is zero.
	m.consumeGasForProofVerification(ctx, ZeroBalanceVerificationDescriptor)
	validated = zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroPendingBalanceHiProof, instruction.Pubkey, instruction.PendingBalanceHi)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid pending balance hi")
	}

	// Validate the available balance is zero.
	m.consumeGasForProofVerification(ctx, ZeroBalanceVerificationDescriptor)
	validated = zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroAvailableBalanceProof, instruction.Pubkey, instruction.AvailableBalance)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid available balance")
	}

	// Create the account
	account := types.Account{
		PublicKey:                   *instruction.Pubkey,
		PendingBalanceLo:            instruction.PendingBalanceLo,
		PendingBalanceHi:            instruction.PendingBalanceHi,
		AvailableBalance:            instruction.AvailableBalance,
		DecryptableAvailableBalance: instruction.DecryptableBalance,
		PendingBalanceCreditCounter: 0,
	}

	// Store the account
	err = m.Keeper.SetAccount(ctx, req.FromAddress, req.Denom, account)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error setting account")
	}

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

	if !m.Keeper.IsCtModuleEnabled(ctx) {
		return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "feature is disabled by governance")
	}

	// Validate request
	err := req.ValidateBasic()
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid request")
	}

	// Check if the account exists
	address, err := sdk.AccAddressFromBech32(req.FromAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid address")
	}

	account, exists := m.Keeper.GetAccount(ctx, req.FromAddress, req.Denom)
	if !exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrNotFound, "account does not exist")
	}

	// The maximum transfer amount is 2^48
	if req.Amount > uint64((1<<48)-1) {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "exceeded maximum deposit amount of 2^48")
	}

	// Check that account does not have the maximum limit of pending transactions.
	if account.PendingBalanceCreditCounter == math.MaxUint16 {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "account has too many pending transactions")
	}

	// Deduct amount from user's token balance.
	// Define the amount to be transferred as sdk.Coins
	coins := sdk.NewCoins(sdk.NewCoin(req.Denom, sdk.NewIntFromUint64(req.Amount)))

	// Transfer the amount from the sender's account to the module account
	if err := m.Keeper.BankKeeper().SendCoinsFromAccountToModule(ctx, address, types.ModuleName, coins); err != nil {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrInsufficientFunds, "insufficient funds to deposit %d %s", req.Amount, req.Denom)
	}

	// Split the deposit amount into lo and hi bits.
	// Extract the bottom 16 bits (rightmost 16 bits)
	bottom16, next32, err := utils.SplitTransferBalance(req.Amount)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error splitting transfer balance")
	}

	// Compute the new balances
	teg := elgamal.NewTwistedElgamal()
	m.consumeGasForCiphertext(ctx, AddScalarDescriptor)
	newPendingBalanceLo, err := teg.AddScalar(account.PendingBalanceLo, new(big.Int).SetUint64(uint64(bottom16)))
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error adding pending balance lo")
	}

	m.consumeGasForCiphertext(ctx, AddScalarDescriptor)
	newPendingBalanceHi, err := teg.AddScalar(account.PendingBalanceHi, new(big.Int).SetUint64(uint64(next32)))
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error adding pending balance hi")
	}

	// Update the account state
	account.PendingBalanceLo = newPendingBalanceLo
	account.PendingBalanceHi = newPendingBalanceHi
	account.PendingBalanceCreditCounter += 1

	// Save the changes to the account state
	err = m.Keeper.SetAccount(ctx, req.FromAddress, req.Denom, account)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error setting account")
	}

	// Emit any required events
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeDeposit,
			sdk.NewAttribute(types.AttributeDenom, req.Denom),
			sdk.NewAttribute(types.AttributeAddress, req.FromAddress),
			sdk.NewAttribute(sdk.AttributeKeyAmount, sdk.NewCoin(req.Denom, sdk.NewIntFromUint64(req.Amount)).String()),
		),
	})

	return &types.MsgDepositResponse{}, nil
}

func (m msgServer) Withdraw(goCtx context.Context, req *types.MsgWithdraw) (*types.MsgWithdrawResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !m.Keeper.IsCtModuleEnabled(ctx) {
		return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "feature is disabled by governance")
	}

	// Get the requested address.
	address, err := sdk.AccAddressFromBech32(req.FromAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid address")
	}

	// Get the user's account
	account, exists := m.Keeper.GetAccount(ctx, req.FromAddress, req.Denom)
	if !exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrNotFound, "account does not exist")
	}

	// Convert the struct to a usable form. This also validates the request.
	instruction, err := req.FromProto()
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid msg")
	}

	// Verify that the account has sufficient funds (Remaining balance after making the transfer is greater than or equal to zero.)
	// This range proof verification is performed on the RemainingBalanceCommitment sent by the user.
	// An additional check is required to ensure that this matches the remaining balance calculated by the server.

	// Consume additional gas as range proofs are computationally expensive.
	cost := m.Keeper.GetRangeProofGasCost(ctx)
	if cost > 0 {
		ctx.GasMeter().ConsumeGas(cost, RangedProofVerificationDescriptor)
	}

	verified, _ := zkproofs.VerifyRangeProof(instruction.Proofs.RemainingBalanceRangeProof, instruction.RemainingBalanceCommitment, 128, m.CachedRangeVerifierFactory)
	if !verified {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "range proof verification failed")
	}

	// Verify that the remaining balance sent by the user matches the remaining balance calculated by the server.
	teg := elgamal.NewTwistedElgamal()
	m.consumeGasForCiphertext(ctx, SubScalarDescriptor)
	remainingBalanceCalculated, err := teg.SubScalar(account.AvailableBalance, instruction.Amount)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error subtracting amount")
	}

	m.consumeGasForProofVerification(ctx, CiphertextCommitmentEqualityVerificationDescriptor)
	verified = zkproofs.VerifyCiphertextCommitmentEquality(
		instruction.Proofs.RemainingBalanceEqualityProof,
		&account.PublicKey, remainingBalanceCalculated,
		&instruction.RemainingBalanceCommitment.C)
	if !verified {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "ciphertext commitment equality verification failed")
	}

	// Update the account state
	account.DecryptableAvailableBalance = instruction.DecryptableBalance
	account.AvailableBalance = remainingBalanceCalculated

	// Save the account state
	err = m.Keeper.SetAccount(ctx, req.FromAddress, req.Denom, account)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error setting account")
	}

	// Return the tokens to the sender
	coin := sdk.NewCoin(instruction.Denom, sdk.NewIntFromBigInt(instruction.Amount))
	coins := sdk.NewCoins(coin)
	if err := m.Keeper.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, address, coins); err != nil {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrInsufficientFunds, "insufficient funds to withdraw %s %s", req.Amount, req.Denom)
	}

	// Emit any required events
	//TODO: Look into whether we can use EmitTypedEvents instead since EmitEvents is deprecated
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeWithdraw,
			sdk.NewAttribute(types.AttributeDenom, instruction.Denom),
			sdk.NewAttribute(types.AttributeAddress, instruction.FromAddress),
			sdk.NewAttribute(sdk.AttributeKeyAmount, coin.String()),
		),
	})

	return &types.MsgWithdrawResponse{}, nil
}

func (m msgServer) ApplyPendingBalance(goCtx context.Context, req *types.MsgApplyPendingBalance) (*types.MsgApplyPendingBalanceResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !m.Keeper.IsCtModuleEnabled(ctx) {
		return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "feature is disabled by governance")
	}

	// Check if the account exists
	account, exists := m.Keeper.GetAccount(ctx, req.Address, req.Denom)
	if !exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrNotFound, "account does not exist")
	}

	if account.PendingBalanceCreditCounter == 0 {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "no pending balances to apply")
	}

	// Validate that the balances sent by the user match the balances stored on the server.
	// If the balances do not match, the state has changed since the user created the apply balances.
	// If the pending balance has changed, the account received a transfer or deposit after the user created the apply balances.
	if uint16(req.CurrentPendingBalanceCounter) != account.PendingBalanceCreditCounter {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "pending balance mismatch")
	}
	// If the available balance has changed, the account submitted a withdraw after the user created the apply balances.
	protoAvailableBalance := types.NewCiphertextProto(account.AvailableBalance)
	if !bytes.Equal(protoAvailableBalance.GetC(), req.CurrentAvailableBalance.C) ||
		!bytes.Equal(protoAvailableBalance.GetD(), req.CurrentAvailableBalance.D) {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "available balance mismatch")
	}

	// Calculate updated balances
	teg := elgamal.NewTwistedElgamal()
	// AddWithLoHi uses 3 operations
	m.consumeGasForCiphertextWithMultiplier(ctx, 3, AddWithLoHiDescriptor)
	newAvailableBalance, err := teg.AddWithLoHi(account.AvailableBalance, account.PendingBalanceLo, account.PendingBalanceHi)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error summing balances")
	}

	m.consumeGasForCiphertext(ctx, SubCiphertextDescriptor)
	zeroCiphertextLo, err := elgamal.SubtractCiphertext(account.PendingBalanceLo, account.PendingBalanceLo)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error zeroing pending balance lo")
	}
	m.consumeGasForCiphertext(ctx, SubCiphertextDescriptor)
	zeroCiphertextHi, err := elgamal.SubtractCiphertext(account.PendingBalanceHi, account.PendingBalanceHi)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error zeroing pending balance hi")
	}

	// Apply the changes to the account state
	account.AvailableBalance = newAvailableBalance
	account.DecryptableAvailableBalance = req.NewDecryptableAvailableBalance
	account.PendingBalanceLo = zeroCiphertextLo
	account.PendingBalanceHi = zeroCiphertextHi
	account.PendingBalanceCreditCounter = 0

	// Save the changes to the account state
	err = m.Keeper.SetAccount(ctx, req.Address, req.Denom, account)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error setting account")
	}

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

	if !m.Keeper.IsCtModuleEnabled(ctx) {
		return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "feature is disabled by governance")
	}

	// Check if the account exists
	account, exists := m.Keeper.GetAccount(ctx, req.Address, req.Denom)
	if !exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrNotFound, "account does not exist")
	}

	instruction, err := req.FromProto()
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid msg")
	}

	// Validate proof that pending balance lo is zero.
	m.consumeGasForProofVerification(ctx, ZeroBalanceVerificationDescriptor)
	validated := zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroPendingBalanceLoProof, &account.PublicKey, account.PendingBalanceLo)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "pending balance lo must be 0")
	}

	// Validate proof that pending balance hi is zero.
	m.consumeGasForProofVerification(ctx, ZeroBalanceVerificationDescriptor)
	validated = zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroPendingBalanceHiProof, &account.PublicKey, account.PendingBalanceHi)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "pending balance hi must be 0")
	}

	// Validate proof that available balance is zero.
	m.consumeGasForProofVerification(ctx, ZeroBalanceVerificationDescriptor)
	validated = zkproofs.VerifyZeroBalance(instruction.Proofs.ZeroAvailableBalanceProof, &account.PublicKey, account.AvailableBalance)
	if !validated {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "available balance must be 0")
	}

	// Delete the account
	err = m.Keeper.DeleteAccount(ctx, req.Address, req.Denom)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error deleting account")
	}

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

	if !m.Keeper.IsCtModuleEnabled(ctx) {
		return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "feature is disabled by governance")
	}

	instruction, err := req.FromProto()
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	if instruction.FromAddress == instruction.ToAddress {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "sender and recipient addresses must be different")
	}

	// Check that sender and recipient accounts exist.
	senderAccount, exists := m.Keeper.GetAccount(ctx, req.FromAddress, req.Denom)
	if !exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrNotFound, "sender account does not exist")
	}

	recipientAccount, exists := m.Keeper.GetAccount(ctx, req.ToAddress, req.Denom)
	if !exists {
		return nil, sdkerrors.Wrap(sdkerrors.ErrNotFound, "recipient account does not exist")
	}

	// Check that account does not have the maximum limit of pending transactions.
	if recipientAccount.PendingBalanceCreditCounter == math.MaxUint16 {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "recipient account has too many pending transactions")
	}

	if len(req.Auditors) > types.MaxAuditors {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "maximum number of auditors exceeded")
	}

	// Calculate senders new available balance.
	teg := elgamal.NewTwistedElgamal()
	m.consumeGasForCiphertextWithMultiplier(ctx, 3, SubWithLoHiDescriptor)
	newSenderBalanceCiphertext, err := teg.SubWithLoHi(senderAccount.AvailableBalance, instruction.SenderTransferAmountLo, instruction.SenderTransferAmountHi)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error subtracting sender transfer amount")
	}

	// Validate proofs
	rangeProofGasCost := m.Keeper.GetRangeProofGasCost(ctx)

	// Consume additional gas as range proofs are computationally expensive.
	if rangeProofGasCost > 0 {
		// We charge for 2x for range proof verifications since we verify the available balance range proof and the smaller transfer amount range proofs
		ctx.GasMeter().ConsumeGas(rangeProofGasCost*2, RangedProofVerificationDescriptor)
	}
	// 8 more verification operations are required for the transfer proof.
	m.consumeGasForProofVerificationWithMultiplier(ctx, 8, TransferProofVerificationDescriptor)
	err = types.VerifyTransferProofs(instruction, &senderAccount.PublicKey, &recipientAccount.PublicKey, newSenderBalanceCiphertext, m.CachedRangeVerifierFactory)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	// Validate proofs for each auditor
	for _, auditorParams := range instruction.Auditors {

		auditorAccount, exists := m.Keeper.GetAccount(ctx, auditorParams.Address, instruction.Denom)
		if !exists {
			return nil, sdkerrors.Wrap(sdkerrors.ErrNotFound, "auditor account does not exist")
		}

		// the auditor proof verification involves 4 proofs.
		m.consumeGasForProofVerificationWithMultiplier(ctx, 4, AuditorProofVerificationDescriptor)
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
	m.consumeGasForCiphertext(ctx, AddCiphertextDescriptor)
	recipientPendingBalanceLo, err := elgamal.AddCiphertext(recipientAccount.PendingBalanceLo, instruction.RecipientTransferAmountLo)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error adding recipient transfer amount lo")
	}

	m.consumeGasForCiphertext(ctx, AddCiphertextDescriptor)
	recipientPendingBalanceHi, err := elgamal.AddCiphertext(recipientAccount.PendingBalanceHi, instruction.RecipientTransferAmountHi)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error adding recipient transfer amount hi")
	}

	recipientAccount.PendingBalanceLo = recipientPendingBalanceLo
	recipientAccount.PendingBalanceHi = recipientPendingBalanceHi
	recipientAccount.PendingBalanceCreditCounter += 1

	senderAccount.DecryptableAvailableBalance = instruction.DecryptableBalance
	senderAccount.AvailableBalance = newSenderBalanceCiphertext

	// Save the account states
	err = m.Keeper.SetAccount(ctx, req.FromAddress, req.Denom, senderAccount)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error setting sender account")
	}

	err = m.Keeper.SetAccount(ctx, req.ToAddress, req.Denom, recipientAccount)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "error setting recipient account")
	}

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

func (m msgServer) consumeGas(ctx sdk.Context, gasCost uint64, multiplier uint64, descriptor string) {
	if multiplier < 1 {
		multiplier = 1
	}
	if gasCost > 0 {
		ctx.GasMeter().ConsumeGas(gasCost*multiplier, descriptor)
	}
}

func (m msgServer) consumeGasForCiphertext(ctx sdk.Context, descriptor string) {
	m.consumeGas(ctx, m.Keeper.GetCipherTextGasCost(ctx), 1, descriptor)
}

func (m msgServer) consumeGasForCiphertextWithMultiplier(ctx sdk.Context, multiplier uint64, descriptor string) {
	m.consumeGas(ctx, m.Keeper.GetCipherTextGasCost(ctx), multiplier, descriptor)
}

func (m msgServer) consumeGasForProofVerification(ctx sdk.Context, descriptor string) {
	m.consumeGas(ctx, m.Keeper.GetProofVerificationGasCost(ctx), 1, descriptor)
}

func (m msgServer) consumeGasForProofVerificationWithMultiplier(ctx sdk.Context, multiplier uint64, descriptor string) {
	m.consumeGas(ctx, m.Keeper.GetProofVerificationGasCost(ctx), multiplier, descriptor)
}
