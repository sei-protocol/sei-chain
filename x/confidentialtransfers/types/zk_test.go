package types

import (
	"crypto/ecdsa"
	crand "crypto/rand"
	"math/big"
	"testing"

	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
	"github.com/stretchr/testify/require"
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
				PubkeyValidityProof:       &PubkeyValidityProof{},
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{},
				ZeroPendingBalanceHiProof: &ZeroBalanceProof{},
				ZeroAvailableBalanceProof: &ZeroBalanceProof{},
			},
			wantErr: false,
		},
		{
			name: "missing PubkeyValidityProof",
			proofs: InitializeAccountMsgProofs{
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{},
				ZeroPendingBalanceHiProof: &ZeroBalanceProof{},
				ZeroAvailableBalanceProof: &ZeroBalanceProof{},
			},
			wantErr: true,
			errMsg:  "pubkey validity proof is required",
		},
		{
			name: "missing ZeroPendingBalanceLoProof",
			proofs: InitializeAccountMsgProofs{
				PubkeyValidityProof:       &PubkeyValidityProof{},
				ZeroPendingBalanceHiProof: &ZeroBalanceProof{},
				ZeroAvailableBalanceProof: &ZeroBalanceProof{},
			},
			wantErr: true,
			errMsg:  "zero pending balance lo proof is required",
		},
		{
			name: "missing ZeroPendingBalanceHiProof",
			proofs: InitializeAccountMsgProofs{
				PubkeyValidityProof:       &PubkeyValidityProof{},
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{},
				ZeroAvailableBalanceProof: &ZeroBalanceProof{},
			},
			wantErr: true,
			errMsg:  "zero pending balance hi proof is required",
		},
		{
			name: "missing ZeroAvailableBalanceProof",
			proofs: InitializeAccountMsgProofs{
				PubkeyValidityProof:       &PubkeyValidityProof{},
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{},
				ZeroPendingBalanceHiProof: &ZeroBalanceProof{},
			},
			wantErr: true,
			errMsg:  "zero available balance proof is required",
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
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroPendingBalanceHiProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroAvailableBalanceProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
			},
			wantErr: false,
		},
		{
			name: "missing PubkeyValidityProof",
			proofs: InitializeAccountMsgProofs{
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroPendingBalanceHiProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroAvailableBalanceProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
			},
			wantErr: true,
			errMsg:  "pubkey validity proof is required",
		},
		{
			name: "missing ZeroPendingBalanceLoProof",
			proofs: InitializeAccountMsgProofs{
				PubkeyValidityProof: &PubkeyValidityProof{
					Y: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z: curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroPendingBalanceHiProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroAvailableBalanceProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
			},
			wantErr: true,
			errMsg:  "zero pending balance lo proof is required",
		},
		{
			name: "missing ZeroPendingBalanceHiProof",
			proofs: InitializeAccountMsgProofs{
				PubkeyValidityProof: &PubkeyValidityProof{
					Y: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z: curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroAvailableBalanceProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
			},
			wantErr: true,
			errMsg:  "zero pending balance hi proof is required",
		},
		{
			name: "missing ZeroAvailableBalanceProof",
			proofs: InitializeAccountMsgProofs{
				PubkeyValidityProof: &PubkeyValidityProof{
					Y: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z: curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroPendingBalanceHiProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
			},
			wantErr: true,
			errMsg:  "zero available balance proof is required",
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

func TestWithdrawMsgProofs_FromProto(t *testing.T) {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	sourcePrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	eg := elgamal.NewTwistedElgamal()
	sourceKeypair, _ := utils.GetElGamalKeyPair(*sourcePrivateKey, testDenom)
	value := big.NewInt(100)
	scalarValue, _ := curves.ED25519().Scalar.SetBigInt(value)
	encrypted, randomness, _ := eg.Encrypt(sourceKeypair.PublicKey, value)
	rangeProof, _ := zkproofs.NewRangeProof(128, value, randomness)
	rangeProofProto := NewRangeProofProto(rangeProof)

	equalityProof, _ := zkproofs.NewCiphertextCommitmentEqualityProof(sourceKeypair, encrypted, &randomness, &scalarValue)
	equalityProofProto := NewCiphertextCommitmentEqualityProofProto(equalityProof)

	tests := []struct {
		name    string
		proofs  WithdrawMsgProofs
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid proofs",
			proofs: WithdrawMsgProofs{
				RemainingBalanceRangeProof:    rangeProofProto,
				RemainingBalanceEqualityProof: equalityProofProto,
			},
			wantErr: false,
		},
		{
			name: "missing RangeProof",
			proofs: WithdrawMsgProofs{
				RemainingBalanceEqualityProof: equalityProofProto,
			},
			wantErr: true,
			errMsg:  "range proof is required",
		},
		{
			name: "missing CiphertextCommitmentEqualityProof",
			proofs: WithdrawMsgProofs{
				RemainingBalanceRangeProof: rangeProofProto,
			},
			wantErr: true,
			errMsg:  "remaining balance equality proof is required",
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

func TestWithdrawMsgProofs_Validate(t *testing.T) {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	sourcePrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	eg := elgamal.NewTwistedElgamal()
	sourceKeypair, _ := utils.GetElGamalKeyPair(*sourcePrivateKey, testDenom)
	value := big.NewInt(100)
	scalarValue, _ := curves.ED25519().Scalar.SetBigInt(value)
	encrypted, randomness, _ := eg.Encrypt(sourceKeypair.PublicKey, value)
	rangeProof, _ := zkproofs.NewRangeProof(128, value, randomness)
	rangeProofProto := NewRangeProofProto(rangeProof)

	equalityProof, _ := zkproofs.NewCiphertextCommitmentEqualityProof(sourceKeypair, encrypted, &randomness, &scalarValue)
	equalityProofProto := NewCiphertextCommitmentEqualityProofProto(equalityProof)

	tests := []struct {
		name    string
		proofs  WithdrawMsgProofs
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid proofs",
			proofs: WithdrawMsgProofs{
				RemainingBalanceRangeProof:    rangeProofProto,
				RemainingBalanceEqualityProof: equalityProofProto,
			},
			wantErr: false,
		},
		{
			name: "missing RangeProof",
			proofs: WithdrawMsgProofs{
				RemainingBalanceEqualityProof: equalityProofProto,
			},
			wantErr: true,
			errMsg:  "range proof is required",
		},
		{
			name: "missing CiphertextCommitmentEqualityProof",
			proofs: WithdrawMsgProofs{
				RemainingBalanceRangeProof: rangeProofProto,
			},
			wantErr: true,
			errMsg:  "remaining balance equality proof is required",
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

func TestCloseAccountMsgProofs_Validate(t *testing.T) {
	tests := []struct {
		name    string
		proofs  CloseAccountMsgProofs
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid proofs",
			proofs: CloseAccountMsgProofs{
				ZeroAvailableBalanceProof: &ZeroBalanceProof{},
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{},
				ZeroPendingBalanceHiProof: &ZeroBalanceProof{},
			},
			wantErr: false,
		},
		{
			name:    "missing ZeroAvailableBalanceProof",
			proofs:  CloseAccountMsgProofs{},
			wantErr: true,
			errMsg:  "close account proof is invalid",
		},
		{
			name: "missing ZeroPendingBalanceLoProof",
			proofs: CloseAccountMsgProofs{
				ZeroAvailableBalanceProof: &ZeroBalanceProof{},
			},
			wantErr: true,
			errMsg:  "close account proof is invalid",
		},
		{
			name: "missing ZeroPendingBalanceHiProof",
			proofs: CloseAccountMsgProofs{
				ZeroAvailableBalanceProof: &ZeroBalanceProof{},
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{},
			},
			wantErr: true,
			errMsg:  "close account proof is invalid",
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

func TestCloseAccountMsgProofs_FromProto(t *testing.T) {
	tests := []struct {
		name    string
		proofs  CloseAccountMsgProofs
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing ZeroAvailableBalanceProof",
			proofs:  CloseAccountMsgProofs{},
			wantErr: true,
			errMsg:  "close account proof is invalid",
		},
		{
			name: "missing ZeroPendingBalanceLoProof",
			proofs: CloseAccountMsgProofs{
				ZeroAvailableBalanceProof: &ZeroBalanceProof{},
			},
			wantErr: true,
			errMsg:  "close account proof is invalid",
		},
		{
			name: "missing ZeroPendingBalanceHiProof",
			proofs: CloseAccountMsgProofs{
				ZeroAvailableBalanceProof: &ZeroBalanceProof{},
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{},
			},
			wantErr: true,
			errMsg:  "close account proof is invalid",
		},
		{
			name: "valid proofs",
			proofs: CloseAccountMsgProofs{
				ZeroAvailableBalanceProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroPendingBalanceLoProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
				ZeroPendingBalanceHiProof: &ZeroBalanceProof{
					YP: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					YD: curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
					Z:  curves.ED25519().Scalar.Random(crand.Reader).Bytes(),
				},
			},
			wantErr: false,
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
