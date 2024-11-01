package types

import (
	"github.com/coinbase/kryptology/pkg/core/curves"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
)

func (m *CtAccount) ValidateBasic() error {
	if m.PublicKey == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "PublicKey is required")
	}

	if m.PendingBalanceLo == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "PendingBalanceLo is required")
	}

	if m.PendingBalanceHi == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "PendingBalanceHi is required")
	}

	if m.AvailableBalance == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "AvailableBalance is required")
	}

	if m.DecryptableAvailableBalance == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "DecryptableAvailableBalance is required")
	}

	return nil
}

func (m *CtAccount) FromProto() (*Account, error) {
	var err error

	ed25519Curve := curves.ED25519()
	pubkey, err := ed25519Curve.Point.FromAffineCompressed(c.PublicKey)
	if err != nil {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidPubKey, "Invalid public key (%s)", err)
	}

	var pendingBalanceLo *elgamal.Ciphertext
	if m.PendingBalanceLo != nil {
		pendingBalanceLo, err = m.PendingBalanceLo.FromProto()
		if err != nil {
			return nil, err
		}
	}

	var pendingBalanceHi *elgamal.Ciphertext
	if m.PendingBalanceHi != nil {
		pendingBalanceHi, err = m.PendingBalanceHi.FromProto()
		if err != nil {
			return nil, err
		}
	}

	var availableBalance *elgamal.Ciphertext
	if m.AvailableBalance != nil {
		availableBalance, err = m.AvailableBalance.FromProto()
		if err != nil {
			return nil, err
		}
	}

	return &Account{
		PublicKey:                   pubkey,
		PendingBalanceLo:            pendingBalanceLo,
		PendingBalanceHi:            pendingBalanceHi,
		AvailableBalance:            availableBalance,
		DecryptableAvailableBalance: m.DecryptableAvailableBalance,
		PendingBalanceCreditCounter: uint16(m.PendingBalanceCreditCounter),
	}, nil
}

func ToProto(account *Account) *CtAccount {
	pubkeyCompressed := account.PublicKey.ToAffineCompressed()

	pendingBalanceLo := account.PendingBalanceLo.ToProto()
	pendingBalanceHi := account.PendingBalanceHi.ToProto()
	availableBalance := account.AvailableBalance.ToProto()

	return &CtAccount{
		PublicKey:                   pubkeyCompressed,
		PendingBalanceLo:            pendingBalanceLo,
		PendingBalanceHi:            pendingBalanceHi,
		AvailableBalance:            availableBalance,
		DecryptableAvailableBalance: account.DecryptableAvailableBalance,
		PendingBalanceCreditCounter: uint32(account.PendingBalanceCreditCounter),
	}
}
