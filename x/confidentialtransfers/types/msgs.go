package types

import (
	"crypto/ecdsa"
	"github.com/armon/go-metrics"
	"time"

	"github.com/coinbase/kryptology/pkg/core/curves"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/gogo/protobuf/proto"
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

const NotDecrypted = "not decrypted"

type Decryptable interface {
	Decrypt(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey, decryptAvailableBalance bool, address string) (proto.Message, error)
}

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
	defer metrics.MeasureSince(
		[]string{"ct", "msg", "transfer", "from", "proto", "milliseconds"},
		time.Now().UTC())
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

func (m *MsgTransfer) Decrypt(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey, decryptAvailableBalance bool, address string) (proto.Message, error) {
	if decryptor == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "decryptor is required")
	}

	transfer, err := m.FromProto()
	if err != nil {
		return nil, err
	}

	return transfer.Decrypt(decryptor, privKey, decryptAvailableBalance, address)
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

func (m *MsgInitializeAccount) Decrypt(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey, decryptAvailableBalance bool) (proto.Message, error) {
	if decryptor == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "decryptor is required")
	}

	initialize, err := m.FromProto()
	if err != nil {
		return nil, err
	}

	return initialize.Decrypt(decryptor, privKey, decryptAvailableBalance)
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

	if m.CurrentPendingBalanceCounter == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "current pending balance counter should be greater than 0")
	}

	if m.CurrentAvailableBalance == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "current available balance is required")
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

func (m *MsgApplyPendingBalance) FromProto() (*ApplyPendingBalance, error) {
	err := m.ValidateBasic()
	if err != nil {
		return nil, err
	}

	currentAvailableBalance, err := m.CurrentAvailableBalance.FromProto()
	if err != nil {
		return nil, err
	}

	return &ApplyPendingBalance{
		Address:                        m.Address,
		Denom:                          m.Denom,
		NewDecryptableAvailableBalance: m.NewDecryptableAvailableBalance,
		CurrentPendingBalanceCounter:   m.CurrentPendingBalanceCounter,
		CurrentAvailableBalance:        currentAvailableBalance,
	}, nil
}

func (m *MsgApplyPendingBalance) Decrypt(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey, decryptAvailableBalance bool) (proto.Message, error) {
	if decryptor == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "decryptor is required")
	}

	apply, err := m.FromProto()
	if err != nil {
		return nil, err
	}

	return apply.Decrypt(decryptor, privKey, decryptAvailableBalance)
}

func NewMsgApplyPendingBalanceProto(applyPendingBalance *ApplyPendingBalance) *MsgApplyPendingBalance {
	currentAvailableBalance := NewCiphertextProto(applyPendingBalance.CurrentAvailableBalance)

	return &MsgApplyPendingBalance{
		Address:                        applyPendingBalance.Address,
		Denom:                          applyPendingBalance.Denom,
		NewDecryptableAvailableBalance: applyPendingBalance.NewDecryptableAvailableBalance,
		CurrentPendingBalanceCounter:   applyPendingBalance.CurrentPendingBalanceCounter,
		CurrentAvailableBalance:        currentAvailableBalance,
	}
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

func (m *MsgWithdraw) Decrypt(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey, decryptAvailableBalance bool) (proto.Message, error) {
	if decryptor == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "decryptor is required")
	}

	withdraw, err := m.FromProto()
	if err != nil {
		return nil, err
	}

	return withdraw.Decrypt(decryptor, privKey, decryptAvailableBalance)
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
