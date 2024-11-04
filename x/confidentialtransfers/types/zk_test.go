package types

import (
	crand "crypto/rand"
	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTransferProofs_Validate(t *testing.T) {
	tests := []struct {
		name    string
		proofs  TransferMsgProofs
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid proofs",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
				RecipientTransferAmountLoValidityProof:  &CiphertextValidityProof{},
				RecipientTransferAmountHiValidityProof:  &CiphertextValidityProof{},
				RemainingBalanceRangeProof:              &RangeProof{},
				RemainingBalanceEqualityProof:           &CiphertextCommitmentEqualityProof{},
				TransferAmountLoEqualityProof:           &CiphertextCiphertextEqualityProof{},
				TransferAmountHiEqualityProof:           &CiphertextCiphertextEqualityProof{},
			},
			wantErr: false,
		},
		{
			name:    "missing RemainingBalanceCommitmentValidityProof",
			proofs:  TransferMsgProofs{},
			wantErr: true,
			errMsg:  "remaining balance commitment validity proof is required",
		},
		{
			name: "missing SenderTransferAmountLoValidityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "sender transfer amount lo validity proof is required",
		},
		{
			name: "missing SenderTransferAmountHiValidityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "sender transfer amount hi validity proof is required",
		},
		{
			name: "missing RecipientTransferAmountLoValidityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "recipient transfer amount lo validity proof is required",
		},
		{
			name: "missing RecipientTransferAmountHiValidityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
				RecipientTransferAmountLoValidityProof:  &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "recipient transfer amount hi validity proof is required",
		},
		{
			name: "missing RemainingBalanceRangeProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
				RecipientTransferAmountLoValidityProof:  &CiphertextValidityProof{},
				RecipientTransferAmountHiValidityProof:  &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "remaining balance range proof is required",
		},
		{
			name: "missing RemainingBalanceEqualityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
				RecipientTransferAmountLoValidityProof:  &CiphertextValidityProof{},
				RecipientTransferAmountHiValidityProof:  &CiphertextValidityProof{},
				RemainingBalanceRangeProof:              &RangeProof{},
			},
			wantErr: true,
			errMsg:  "remaining balance equality proof is required",
		},
		{
			name: "missing TransferAmountLoEqualityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
				RecipientTransferAmountLoValidityProof:  &CiphertextValidityProof{},
				RecipientTransferAmountHiValidityProof:  &CiphertextValidityProof{},
				RemainingBalanceRangeProof:              &RangeProof{},
				RemainingBalanceEqualityProof:           &CiphertextCommitmentEqualityProof{},
			},
			wantErr: true,
			errMsg:  "transfer amount lo equality proof is required",
		},
		{
			name: "missing TransferAmountHiEqualityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
				RecipientTransferAmountLoValidityProof:  &CiphertextValidityProof{},
				RecipientTransferAmountHiValidityProof:  &CiphertextValidityProof{},
				RemainingBalanceRangeProof:              &RangeProof{},
				RemainingBalanceEqualityProof:           &CiphertextCommitmentEqualityProof{},
				TransferAmountLoEqualityProof:           &CiphertextCiphertextEqualityProof{},
			},
			wantErr: true,
			errMsg:  "transfer amount hi equality proof is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.proofs.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTransferMsgProofs_FromProto(t *testing.T) {
	tests := []struct {
		name    string
		proofs  TransferMsgProofs
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing RemainingBalanceCommitmentValidityProof",
			proofs:  TransferMsgProofs{},
			wantErr: true,
			errMsg:  "remaining balance commitment validity proof is required",
		},
		{
			name: "missing SenderTransferAmountLoValidityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "sender transfer amount lo validity proof is required",
		},
		{
			name: "missing SenderTransferAmountHiValidityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "sender transfer amount hi validity proof is required",
		},
		{
			name: "missing RecipientTransferAmountLoValidityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "recipient transfer amount lo validity proof is required",
		},
		{
			name: "missing RecipientTransferAmountHiValidityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
				RecipientTransferAmountLoValidityProof:  &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "recipient transfer amount hi validity proof is required",
		},
		{
			name: "missing RemainingBalanceRangeProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
				RecipientTransferAmountLoValidityProof:  &CiphertextValidityProof{},
				RecipientTransferAmountHiValidityProof:  &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "remaining balance range proof is required",
		},
		{
			name: "missing RemainingBalanceEqualityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
				RecipientTransferAmountLoValidityProof:  &CiphertextValidityProof{},
				RecipientTransferAmountHiValidityProof:  &CiphertextValidityProof{},
				RemainingBalanceRangeProof:              &RangeProof{},
			},
			wantErr: true,
			errMsg:  "remaining balance equality proof is required",
		},
		{
			name: "missing TransferAmountLoEqualityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
				RecipientTransferAmountLoValidityProof:  &CiphertextValidityProof{},
				RecipientTransferAmountHiValidityProof:  &CiphertextValidityProof{},
				RemainingBalanceRangeProof:              &RangeProof{},
				RemainingBalanceEqualityProof:           &CiphertextCommitmentEqualityProof{},
			},
			wantErr: true,
			errMsg:  "transfer amount lo equality proof is required",
		},
		{
			name: "missing TransferAmountHiEqualityProof",
			proofs: TransferMsgProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
				RecipientTransferAmountLoValidityProof:  &CiphertextValidityProof{},
				RecipientTransferAmountHiValidityProof:  &CiphertextValidityProof{},
				RemainingBalanceRangeProof:              &RangeProof{},
				RemainingBalanceEqualityProof:           &CiphertextCommitmentEqualityProof{},
				TransferAmountLoEqualityProof:           &CiphertextCiphertextEqualityProof{},
			},
			wantErr: true,
			errMsg:  "transfer amount hi equality proof is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.proofs.FromProto()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestInitializeAccountMsgProofs_Validate(t *testing.T) {
	tests := []struct {
		name    string
		proofs  InitializeAccountMsgProofs
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid proofs",
			proofs: InitializeAccountMsgProofs{
				PubkeyValidityProof: &PubkeyValidityProof{},
			},
			wantErr: false,
		},
		{
			name:    "missing PubkeyValidityProof",
			proofs:  InitializeAccountMsgProofs{},
			wantErr: true,
			errMsg:  "pubkey validity proof is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.proofs.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestInitializeAccountMsgProofs_FromProto(t *testing.T) {
	tests := []struct {
		name    string
		proofs  InitializeAccountMsgProofs
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid proofs",
			proofs: InitializeAccountMsgProofs{
				PubkeyValidityProof: &PubkeyValidityProof{
					Y: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z: curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
			},
			wantErr: false,
		},
		{
			name:    "missing PubkeyValidityProof",
			proofs:  InitializeAccountMsgProofs{},
			wantErr: true,
			errMsg:  "pubkey validity proof is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.proofs.FromProto()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
