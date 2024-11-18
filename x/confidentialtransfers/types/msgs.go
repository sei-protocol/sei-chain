package types

import (
	"crypto/ecdsa"
	"errors"

	"github.com/coinbase/kryptology/pkg/core/curves"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
)

// confidential transfers message types
const (
	TypeMsgTransfer            = "transfer"
	TypeMsgInitializeAccount   = "initialize_account"
	TypeMsgDeposit             = "deposit"
	TypeMsgWithdraw            = "withdraw"
	TypeMsgApplyPendingBalance = "apply_pending_balance"
	TypeMsgCloseAccount        = "close_account"
)

var _ sdk.Msg = &MsgTransfer{}

// Route Implements Msg.
func (m *MsgTransfer) Route() string { return RouterKey }

// Type Implements Msg.
func (m *MsgTransfer) Type() string { return TypeMsgTransfer }

func (m *MsgTransfer) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.FromAddress)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address (%s)", err)
	}

	_, err = sdk.AccAddressFromBech32(m.ToAddress)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid recipient address (%s)", err)
	}

	err = sdk.ValidateDenom(m.Denom)
	if err != nil {
		return err
	}

	if m.FromAmountLo == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "from amount lo is required")
	}

	if m.FromAmountHi == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "from amount hi is required")
	}

	if m.ToAmountLo == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "to amount lo is required")
	}

	if m.ToAmountHi == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "to amount hi is required")
	}

	if m.RemainingBalance == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "remaining balance is required")
	}

	if m.Proofs == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "proofs is required")
	}

	err = m.Proofs.Validate()
	if err != nil {
		return err
	}

	return nil
}

func (m *MsgTransfer) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

func (m *MsgTransfer) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.FromAddress)
	return []sdk.AccAddress{sender}
}

func (m *MsgTransfer) FromProto() (*Transfer, error) {
	err := m.ValidateBasic()
	if err != nil {
		return nil, err
	}
	senderTransferAmountLo, err := m.FromAmountLo.FromProto()
	if err != nil {
		return nil, err
	}

	senderTransferAmountHi, err := m.FromAmountHi.FromProto()
	if err != nil {
		return nil, err
	}

	recipientTransferAmountLo, err := m.ToAmountLo.FromProto()
	if err != nil {
		return nil, err
	}

	recipientTransferAmountHi, err := m.ToAmountHi.FromProto()
	if err != nil {
		return nil, err
	}

	remainingBalanceCommitment, err := m.RemainingBalance.FromProto()
	if err != nil {
		return nil, err
	}

	proofs, err := m.Proofs.FromProto()
	if err != nil {
		return nil, err
	}

	// iterate over m.Auditors and convert them to types.Auditor
	auditors := make([]*TransferAuditor, 0, len(m.Auditors))
	for _, auditor := range m.Auditors {
		auditorData, e := auditor.FromProto()
		if e != nil {
			return nil, e
		}
		auditors = append(auditors, auditorData)
	}

	return &Transfer{
		FromAddress:                m.FromAddress,
		ToAddress:                  m.ToAddress,
		Denom:                      m.Denom,
		SenderTransferAmountLo:     senderTransferAmountLo,
		SenderTransferAmountHi:     senderTransferAmountHi,
		RecipientTransferAmountLo:  recipientTransferAmountLo,
		RecipientTransferAmountHi:  recipientTransferAmountHi,
		RemainingBalanceCommitment: remainingBalanceCommitment,
		DecryptableBalance:         m.DecryptableBalance,
		Proofs:                     proofs,
		Auditors:                   auditors,
	}, nil
}

func NewMsgTransferProto(transfer *Transfer) *MsgTransfer {
	fromAmountLo := NewCiphertextProto(transfer.SenderTransferAmountLo)
	fromAmountHi := NewCiphertextProto(transfer.SenderTransferAmountHi)
	toAmountLo := NewCiphertextProto(transfer.RecipientTransferAmountLo)
	toAmountHi := NewCiphertextProto(transfer.RecipientTransferAmountHi)
	remainingBalance := NewCiphertextProto(transfer.RemainingBalanceCommitment)
	proofs := NewTransferMsgProofs(transfer.Proofs)

	// iterate over transfer.Auditors and convert them to types.Auditor
	auditors := make([]*Auditor, 0, len(transfer.Auditors))
	for _, auditorData := range transfer.Auditors {
		auditor := NewAuditorProto(auditorData)
		auditors = append(auditors, auditor)
	}

	return &MsgTransfer{
		FromAddress:        transfer.FromAddress,
		ToAddress:          transfer.ToAddress,
		Denom:              transfer.Denom,
		FromAmountLo:       fromAmountLo,
		FromAmountHi:       fromAmountHi,
		ToAmountLo:         toAmountLo,
		ToAmountHi:         toAmountHi,
		RemainingBalance:   remainingBalance,
		DecryptableBalance: transfer.DecryptableBalance,
		Proofs:             proofs,
		Auditors:           auditors,
	}
}

var _ sdk.Msg = &MsgInitializeAccount{}

// Route Implements Msg.
func (m *MsgInitializeAccount) Route() string { return RouterKey }

// Type Implements Msg.
func (m *MsgInitializeAccount) Type() string { return TypeMsgInitializeAccount }

func (m *MsgInitializeAccount) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.FromAddress)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address (%s)", err)
	}

	err = sdk.ValidateDenom(m.Denom)
	if err != nil {
		return err
	}

	if m.PublicKey == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "public key is required")
	}

	if m.DecryptableBalance == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "decryptable balance is required")
	}

	if m.PendingBalanceLo == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "pending amount lo is required")
	}

	if m.PendingBalanceHi == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "pending amount hi is required")
	}

	if m.AvailableBalance == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "available balance is required")
	}

	if m.Proofs == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "proofs is required")
	}

	err = m.Proofs.Validate()
	if err != nil {
		return err
	}

	return nil
}

func (m *MsgInitializeAccount) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

func (m *MsgInitializeAccount) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.FromAddress)
	return []sdk.AccAddress{sender}
}

func (m *MsgInitializeAccount) FromProto() (*InitializeAccount, error) {
	err := m.ValidateBasic()
	if err != nil {
		return nil, err
	}
	ed25519Curve := curves.ED25519()

	pubkey, err := ed25519Curve.Point.FromAffineCompressed(m.PublicKey)
	if err != nil {
		return nil, err
	}

	pendingBalanceLo, err := m.PendingBalanceLo.FromProto()
	if err != nil {
		return nil, err
	}

	pendingBalanceHi, err := m.PendingBalanceHi.FromProto()
	if err != nil {
		return nil, err
	}

	availableBalance, err := m.AvailableBalance.FromProto()
	if err != nil {
		return nil, err
	}

	proofs, err := m.Proofs.FromProto()
	if err != nil {
		return nil, err
	}

	return &InitializeAccount{
		FromAddress:        m.FromAddress,
		Denom:              m.Denom,
		Pubkey:             &pubkey,
		PendingBalanceLo:   pendingBalanceLo,
		PendingBalanceHi:   pendingBalanceHi,
		AvailableBalance:   availableBalance,
		DecryptableBalance: m.DecryptableBalance,
		Proofs:             proofs,
	}, nil
}

// convert the InitializeAccount to MsgInitializeAccount
func NewMsgInitializeAccountProto(initializeAccount *InitializeAccount) *MsgInitializeAccount {
	pubkeyRaw := *initializeAccount.Pubkey
	pubkey := pubkeyRaw.ToAffineCompressed()

	pendingBalanceLo := NewCiphertextProto(initializeAccount.PendingBalanceLo)

	pendingBalanceHi := NewCiphertextProto(initializeAccount.PendingBalanceHi)

	availableBalance := NewCiphertextProto(initializeAccount.AvailableBalance)

	proofs := NewInitializeAccountMsgProofs(initializeAccount.Proofs)

	return &MsgInitializeAccount{
		FromAddress:        initializeAccount.FromAddress,
		Denom:              initializeAccount.Denom,
		PublicKey:          pubkey,
		DecryptableBalance: initializeAccount.DecryptableBalance,
		PendingBalanceLo:   pendingBalanceLo,
		PendingBalanceHi:   pendingBalanceHi,
		AvailableBalance:   availableBalance,
		Proofs:             proofs,
	}
}

var _ sdk.Msg = &MsgApplyPendingBalance{}

// Route Implements Msg.
func (m *MsgApplyPendingBalance) Route() string { return RouterKey }

// Type Implements Msg.
func (m *MsgApplyPendingBalance) Type() string { return TypeMsgApplyPendingBalance }

func (m *MsgApplyPendingBalance) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.Address)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid address (%s)", err)
	}

	err = sdk.ValidateDenom(m.Denom)
	if err != nil {
		return err
	}

	if len(m.NewDecryptableAvailableBalance) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "new decryptable available balance is required")
	}
	return nil
}

func (m *MsgApplyPendingBalance) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

func (m *MsgApplyPendingBalance) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.Address)
	return []sdk.AccAddress{sender}
}

// NewMsgApplyPendingBalance creates a new MsgApplyPendingBalance instance
// TODO: If pending balance changes between when this instruction is generated and when it is received by server,
// the new pending balance will be incorrect. This is a potential attack vector.
// We should consider adding the pending balances being applied to the instruction to circumvent this.
// 1. Account state: Pending: 100, Available: 1000
// 2. Create Instruction: DecryptableAvailableBalance = 1100
// 3. Transfer Received: Pending: 200, Available: 1000
// 4. Apply Instruction: Applies pending balance of 200 -> AvailableBalance = 1200 while DecryptableAvailableBalance = 1100
func NewMsgApplyPendingBalance(
	privKey ecdsa.PrivateKey,
	address, denom,
	currentDecryptableBalance string,
	currentPendingBalanceCounter uint16,
	currentAvailableBalance,
	currentPendingBalanceLo,
	currentPendingBalanceHi *elgamal.Ciphertext) (*MsgApplyPendingBalance, error) {
	aesKey, err := encryption.GetAESKey(privKey, denom)
	if err != nil {
		return nil, err
	}

	// Get the current balance from the decryptable balance.
	currentBalance, err := encryption.DecryptAESGCM(currentDecryptableBalance, aesKey)
	if err != nil {
		return nil, err
	}

	teg := elgamal.NewTwistedElgamal()
	keyPair, err := teg.KeyGen(privKey, denom)
	if err != nil {
		return nil, err
	}

	// Calculate the pending balances that we need to add to the available balance.
	loBalance, err := teg.Decrypt(keyPair.PrivateKey, currentPendingBalanceLo, elgamal.MaxBits32)
	if err != nil {
		return nil, err
	}

	hiBalance, err := teg.DecryptLargeNumber(keyPair.PrivateKey, currentPendingBalanceHi, elgamal.MaxBits48)
	if err != nil {
		return nil, err
	}

	// Get the pending balance by combining the lo and hi bits
	pendingBalance := utils.CombineTransferAmount(uint16(loBalance), uint32(hiBalance))

	// Sum the balances to get the new available balance
	newDecryptedAvailableBalance := currentBalance + pendingBalance

	// Check for overflow: if the sum is less than one of the operands, an overflow has occurred.
	if newDecryptedAvailableBalance < currentBalance {
		return nil, errors.New("addition overflow: total balance exceeds uint64")
	}

	// Encrypt the new available balance
	newDecryptableAvailableBalance, err := encryption.EncryptAESGCM(newDecryptedAvailableBalance, aesKey)
	if err != nil {
		return nil, err
	}

	return &MsgApplyPendingBalance{
		Address:                        address,
		Denom:                          denom,
		NewDecryptableAvailableBalance: newDecryptableAvailableBalance,
		CurrentPendingBalanceCounter:   uint32(currentPendingBalanceCounter),
		CurrentAvailableBalance:        NewCiphertextProto(currentAvailableBalance),
	}, nil
}

var _ sdk.Msg = &MsgCloseAccount{}

// Route Implements Msg.
func (m *MsgCloseAccount) Route() string { return RouterKey }

// Type Implements Msg.
func (m *MsgCloseAccount) Type() string { return TypeMsgCloseAccount }

func (m *MsgCloseAccount) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.Address)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid address (%s)", err)
	}

	err = sdk.ValidateDenom(m.Denom)
	if err != nil {
		return err
	}

	if m.Proofs == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "proofs is required")
	}

	err = m.Proofs.Validate()

	if err != nil {
		return err
	}

	return nil
}

func (m *MsgCloseAccount) FromProto() (*CloseAccount, error) {
	err := m.ValidateBasic()
	if err != nil {
		return nil, err
	}

	proofs, err := m.Proofs.FromProto()
	if err != nil {
		return nil, err
	}

	return &CloseAccount{
		Address: m.Address,
		Denom:   m.Denom,
		Proofs:  proofs,
	}, nil
}

func (m *MsgCloseAccount) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

func (m *MsgCloseAccount) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.Address)
	return []sdk.AccAddress{sender}
}

func NewMsgCloseAccountProto(closeAccount *CloseAccount) *MsgCloseAccount {
	proofs := NewCloseAccountMsgProofs(closeAccount.Proofs)
	return &MsgCloseAccount{
		Address: closeAccount.Address,
		Denom:   closeAccount.Denom,
		Proofs:  proofs,
	}
}

var _ sdk.Msg = &MsgDeposit{}

// Route Implements Msg.
func (m *MsgDeposit) Route() string { return RouterKey }

// Type Implements Msg.
func (m *MsgDeposit) Type() string { return TypeMsgDeposit }

func (m *MsgDeposit) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.FromAddress)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address (%s)", err)
	}

	err = sdk.ValidateDenom(m.Denom)
	if err != nil {
		return err
	}

	if m.Amount <= 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "positive amount is required")
	}

	return nil
}

func (m *MsgDeposit) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

func (m *MsgDeposit) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.FromAddress)
	return []sdk.AccAddress{sender}
}

var _ sdk.Msg = &MsgWithdraw{}

// Route Implements Msg.
func (m *MsgWithdraw) Route() string { return RouterKey }

// Type Implements Msg.
func (m *MsgWithdraw) Type() string { return TypeMsgWithdraw }

func (m *MsgWithdraw) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.FromAddress)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address (%s)", err)
	}

	err = sdk.ValidateDenom(m.Denom)
	if err != nil {
		return err
	}

	if m.Amount <= 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "positive amount is required")
	}

	if m.RemainingBalanceCommitment == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "remainingBalanceCommitment is required")
	}

	if m.DecryptableBalance == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "decryptableBalance is required")
	}

	if m.Proofs == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "proofs is required")
	}

	err = m.Proofs.Validate()
	if err != nil {
		return err
	}

	return nil
}

func (m *MsgWithdraw) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

func (m *MsgWithdraw) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.FromAddress)
	return []sdk.AccAddress{sender}
}

func (m *MsgWithdraw) FromProto() (*Withdraw, error) {
	err := m.ValidateBasic()
	if err != nil {
		return nil, err
	}

	remainingBalanceCommitment, err := m.RemainingBalanceCommitment.FromProto()
	if err != nil {
		return nil, err
	}

	proofs, err := m.Proofs.FromProto()
	if err != nil {
		return nil, err
	}

	return &Withdraw{
		FromAddress:                m.FromAddress,
		Denom:                      m.Denom,
		Amount:                     m.Amount,
		RemainingBalanceCommitment: remainingBalanceCommitment,
		DecryptableBalance:         m.DecryptableBalance,
		Proofs:                     proofs,
	}, nil
}

func NewMsgWithdrawProto(withdraw *Withdraw) *MsgWithdraw {
	remainingBalanceCommitment := NewCiphertextProto(withdraw.RemainingBalanceCommitment)

	proofs := NewWithdrawMsgProofs(withdraw.Proofs)

	return &MsgWithdraw{
		FromAddress:                withdraw.FromAddress,
		Denom:                      withdraw.Denom,
		Amount:                     withdraw.Amount,
		RemainingBalanceCommitment: remainingBalanceCommitment,
		DecryptableBalance:         withdraw.DecryptableBalance,
		Proofs:                     proofs,
	}
}
