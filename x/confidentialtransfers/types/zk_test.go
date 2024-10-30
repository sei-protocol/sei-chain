package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransferProofs_Validate(t *testing.T) {
	tests := []struct {
		name    string
		proofs  TransferProofs
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid proofs",
			proofs: TransferProofs{
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
			proofs:  TransferProofs{},
			wantErr: true,
			errMsg:  "remaining balance commitment validity proof is required",
		},
		{
			name: "missing SenderTransferAmountLoValidityProof",
			proofs: TransferProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "sender transfer amount lo validity proof is required",
		},
		{
			name: "missing SenderTransferAmountHiValidityProof",
			proofs: TransferProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "sender transfer amount hi validity proof is required",
		},
		{
			name: "missing RecipientTransferAmountLoValidityProof",
			proofs: TransferProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "recipient transfer amount lo validity proof is required",
		},
		{
			name: "missing RecipientTransferAmountHiValidityProof",
			proofs: TransferProofs{
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
			proofs: TransferProofs{
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
			proofs: TransferProofs{
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
			proofs: TransferProofs{
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
			proofs: TransferProofs{
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

func TestTransferProofs_FromProto(t *testing.T) {
	tests := []struct {
		name    string
		proofs  TransferProofs
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing RemainingBalanceCommitmentValidityProof",
			proofs:  TransferProofs{},
			wantErr: true,
			errMsg:  "remaining balance commitment validity proof is required",
		},
		{
			name: "missing SenderTransferAmountLoValidityProof",
			proofs: TransferProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "sender transfer amount lo validity proof is required",
		},
		{
			name: "missing SenderTransferAmountHiValidityProof",
			proofs: TransferProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "sender transfer amount hi validity proof is required",
		},
		{
			name: "missing RecipientTransferAmountLoValidityProof",
			proofs: TransferProofs{
				RemainingBalanceCommitmentValidityProof: &CiphertextValidityProof{},
				SenderTransferAmountLoValidityProof:     &CiphertextValidityProof{},
				SenderTransferAmountHiValidityProof:     &CiphertextValidityProof{},
			},
			wantErr: true,
			errMsg:  "recipient transfer amount lo validity proof is required",
		},
		{
			name: "missing RecipientTransferAmountHiValidityProof",
			proofs: TransferProofs{
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
			proofs: TransferProofs{
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
			proofs: TransferProofs{
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
			proofs: TransferProofs{
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
			proofs: TransferProofs{
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
