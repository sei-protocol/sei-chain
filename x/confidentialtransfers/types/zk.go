package types

func (c *TransferProofs) Validate() error {
	return nil
}

//func (c *TransferProofs) FromProofs(proofs *Proofs) *TransferProofs {
//	//proofs.RemainingBalanceRangeProof.
//	//
//	//return &TransferProofs{
//	//	RemainingBalanceCommitmentValidityProof: *proofs.RemainingBalanceCommitmentValidityProof.FromCiphertextCommitmentValidityProof(),
//	//	SenderTransferAmountLoValidityProof:     *proofs.SenderTransferAmountLoValidityProof.FromCiphertextEqualityProof(),
//	//	SenderTransferAmountHiValidityProof:     *proofs.SenderTransferAmountHiValidityProof.FromCiphertextEqualityProof(),
//	//	RecipientTransferAmountLoValidityProof:  *proofs.RecipientTransferAmountLoValidityProof.FromCiphertextEqualityProof(),
//	//	RecipientTransferAmountHiValidityProof:  *proofs.RecipientTransferAmountHiValidityProof.FromCiphertextEqualityProof(),
//	//	RemainingBalanceRangeProof:              *proofs.RemainingBalanceRangeProof.FromRangeProof(),
//	//	RemainingBalanceEqualityProof:           *proofs.RemainingBalanceEqualityProof.FromCiphertextEqualityProof(),
//	//	TransferAmountLoEqualityProof:           *proofs.TransferAmountLoEqualityProof.FromCiphertextEqualityProof(),
//	//	TransferAmountHiEqualityProof:           *proofs.TransferAmountHiEqualityProof.FromCiphertextEqualityProof(),
//	//}
//}
