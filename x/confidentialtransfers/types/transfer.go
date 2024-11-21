package types

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"strconv"

	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

type Transfer struct {
	FromAddress                string              `json:"from_address"`
	ToAddress                  string              `json:"to_address"`
	Denom                      string              `json:"denom"`
	SenderTransferAmountLo     *elgamal.Ciphertext `json:"sender_transfer_amount_lo"`
	SenderTransferAmountHi     *elgamal.Ciphertext `json:"sender_transfer_amount_hi"`
	RecipientTransferAmountLo  *elgamal.Ciphertext `json:"recipient_transfer_amount_lo"`
	RecipientTransferAmountHi  *elgamal.Ciphertext `json:"recipient_transfer_amount_hi"`
	RemainingBalanceCommitment *elgamal.Ciphertext `json:"remaining_balance_commitment"`
	DecryptableBalance         string              `json:"decryptable_balance"`
	Proofs                     *TransferProofs     `json:"proofs"`
	Auditors                   []*TransferAuditor  `json:"auditors,omitempty"` //optional field
}

type TransferProofs struct {
	RemainingBalanceCommitmentValidityProof *zkproofs.CiphertextValidityProof           `json:"remaining_balance_commitment_validity_proof"`
	SenderTransferAmountLoValidityProof     *zkproofs.CiphertextValidityProof           `json:"sender_transfer_amount_lo_validity_proof"`
	SenderTransferAmountHiValidityProof     *zkproofs.CiphertextValidityProof           `json:"sender_transfer_amount_hi_validity_proof"`
	RecipientTransferAmountLoValidityProof  *zkproofs.CiphertextValidityProof           `json:"recipient_transfer_amount_lo_validity_proof"`
	RecipientTransferAmountHiValidityProof  *zkproofs.CiphertextValidityProof           `json:"recipient_transfer_amount_hi_validity_proof"`
	RemainingBalanceRangeProof              *zkproofs.RangeProof                        `json:"remaining_balance_range_proof"`
	RemainingBalanceEqualityProof           *zkproofs.CiphertextCommitmentEqualityProof `json:"remaining_balance_equality_proof"`
	TransferAmountLoEqualityProof           *zkproofs.CiphertextCiphertextEqualityProof `json:"transfer_amount_lo_equality_proof"`
	TransferAmountHiEqualityProof           *zkproofs.CiphertextCiphertextEqualityProof `json:"transfer_amount_hi_equality_proof"`
}

type TransferAuditor struct {
	Address                       string                                      `json:"address"`
	EncryptedTransferAmountLo     *elgamal.Ciphertext                         `json:"encrypted_transfer_amount_lo"`
	EncryptedTransferAmountHi     *elgamal.Ciphertext                         `json:"encrypted_transfer_amount_hi"`
	TransferAmountLoValidityProof *zkproofs.CiphertextValidityProof           `json:"transfer_amount_lo_validity_proof"`
	TransferAmountHiValidityProof *zkproofs.CiphertextValidityProof           `json:"transfer_amount_hi_validity_proof"`
	TransferAmountLoEqualityProof *zkproofs.CiphertextCiphertextEqualityProof `json:"transfer_amount_lo_equality_proof"`
	TransferAmountHiEqualityProof *zkproofs.CiphertextCiphertextEqualityProof `json:"transfer_amount_hi_equality_proof"`
}

type AuditorInput struct {
	Address string
	Pubkey  *curves.Point
}

// NewTransfer creates a new Transfer object.
func NewTransfer(
	privateKey *ecdsa.PrivateKey,
	senderAddr,
	recipientAddr,
	denom,
	senderCurrentDecryptableBalance string,
	senderCurrentAvailableBalance *elgamal.Ciphertext,
	amount uint64,
	recipientPubkey *curves.Point,
	auditors []AuditorInput) (*Transfer, error) {

	if senderAddr == recipientAddr {
		return &Transfer{}, errors.New("sender and recipient addresses cannot be the same")
	}

	// Get the current balance of the account from the decryptableBalance
	aesKey, err := encryption.GetAESKey(*privateKey, denom)
	if err != nil {
		return &Transfer{}, err
	}

	currentBalance, err := encryption.DecryptAESGCM(senderCurrentDecryptableBalance, aesKey)
	if err != nil {
		return &Transfer{}, err
	}

	// Check that account has sufficient balance to make the transfer.
	if currentBalance < amount {
		return &Transfer{}, errors.New("insufficient balance")
	}

	// Encrypt the new balance using the user's AES Key.
	newBalance := currentBalance - amount
	decryptableNewBalance, err := encryption.EncryptAESGCM(newBalance, aesKey)
	if err != nil {
		return &Transfer{}, err
	}

	// Now we want to encrypt the commitment to the new balance. This is used to generate the range proof.
	teg := elgamal.NewTwistedElgamal()
	senderKeyPair, err := teg.KeyGen(*privateKey, denom)
	if err != nil {
		return &Transfer{}, err
	}

	newBalanceCommitment, newBalanceRandomness, err := teg.Encrypt(senderKeyPair.PublicKey, newBalance)
	if err != nil {
		return &Transfer{}, err
	}

	// Split the transfer amount into bottom 16 bits and top 32 bits.
	// Extract the bottom 16 bits (rightmost 16 bits)
	transferLoBits, transferHiBits, err := utils.SplitTransferBalance(amount)
	if err != nil {
		return &Transfer{}, err
	}

	// Encrypt the transfer amounts for the sender
	senderEncryptedTransferLoBits, senderLoBitsRandomness, err := teg.Encrypt(senderKeyPair.PublicKey, uint64(transferLoBits))
	if err != nil {
		return &Transfer{}, err
	}

	senderEncryptedTransferHiBits, senderHiBitsRandomness, err := teg.Encrypt(senderKeyPair.PublicKey, uint64(transferHiBits))
	if err != nil {
		return &Transfer{}, err
	}

	// Now that we have all the params we need, start generating the proofs wrt the Sender params.
	// First we generate validity proofs that all the ciphertexts are valid.
	newCommitmentValidityProof, err := zkproofs.NewCiphertextValidityProof(&newBalanceRandomness, senderKeyPair.PublicKey, newBalanceCommitment, newBalance)
	if err != nil {
		return &Transfer{}, err
	}

	senderLoBitsValidityProof, err := zkproofs.NewCiphertextValidityProof(&senderLoBitsRandomness, senderKeyPair.PublicKey, senderEncryptedTransferLoBits, uint64(transferLoBits))
	if err != nil {
		return &Transfer{}, err
	}

	senderHiBitsValidityProof, err := zkproofs.NewCiphertextValidityProof(&senderHiBitsRandomness, senderKeyPair.PublicKey, senderEncryptedTransferHiBits, uint64(transferHiBits))
	if err != nil {
		return &Transfer{}, err
	}

	// Secondly, we generate a Range Proof to prove that the PedersonCommitment to the new balance is greater than zero.
	newBalanceRangeProof, err := zkproofs.NewRangeProof(64, int(newBalance), newBalanceRandomness)
	if err != nil {
		return &Transfer{}, err
	}

	// Thirdly we generate proof that the PedersonCommitment we generated encrypts the same value as AvailableBalance - TransferAmount
	bigIntNewBalance := new(big.Int).SetUint64(newBalance)
	newBalanceScalar, err := curves.ED25519().Scalar.SetBigInt(bigIntNewBalance)
	if err != nil {
		return &Transfer{}, err
	}

	newBalanceCiphertext, err := teg.SubWithLoHi(senderCurrentAvailableBalance, senderEncryptedTransferLoBits, senderEncryptedTransferHiBits)
	if err != nil {
		return &Transfer{}, err
	}

	commitmentCiphertextEqualityProof, err := zkproofs.NewCiphertextCommitmentEqualityProof(senderKeyPair, newBalanceCiphertext, &newBalanceRandomness, &newBalanceScalar)
	if err != nil {
		return &Transfer{}, err
	}

	// Now, we create params and proofs specific to the recipient
	recipientParams, err := createTransferPartyParams(recipientAddr, transferLoBits, transferHiBits, senderKeyPair, senderEncryptedTransferLoBits, senderEncryptedTransferHiBits, recipientPubkey)
	if err != nil {
		return &Transfer{}, err
	}

	proofs := TransferProofs{
		RemainingBalanceCommitmentValidityProof: newCommitmentValidityProof,
		SenderTransferAmountLoValidityProof:     senderLoBitsValidityProof,
		SenderTransferAmountHiValidityProof:     senderHiBitsValidityProof,
		RecipientTransferAmountLoValidityProof:  recipientParams.TransferAmountLoValidityProof,
		RecipientTransferAmountHiValidityProof:  recipientParams.TransferAmountHiValidityProof,
		RemainingBalanceRangeProof:              newBalanceRangeProof,
		RemainingBalanceEqualityProof:           commitmentCiphertextEqualityProof,
		TransferAmountLoEqualityProof:           recipientParams.TransferAmountLoEqualityProof,
		TransferAmountHiEqualityProof:           recipientParams.TransferAmountHiEqualityProof,
	}

	// Lastly we generate the Auditor parameters, if required.
	auditorsData := []*TransferAuditor{}
	for _, auditor := range auditors {
		auditorData, err := createTransferPartyParams(auditor.Address, transferLoBits, transferHiBits, senderKeyPair, senderEncryptedTransferLoBits, senderEncryptedTransferHiBits, auditor.Pubkey)
		if err != nil {
			return &Transfer{}, err
		}
		auditorsData = append(auditorsData, auditorData)
	}

	return &Transfer{
		FromAddress:                senderAddr,
		ToAddress:                  recipientAddr,
		Denom:                      denom,
		SenderTransferAmountLo:     senderEncryptedTransferLoBits,
		SenderTransferAmountHi:     senderEncryptedTransferHiBits,
		RecipientTransferAmountLo:  recipientParams.EncryptedTransferAmountLo,
		RecipientTransferAmountHi:  recipientParams.EncryptedTransferAmountHi,
		RemainingBalanceCommitment: newBalanceCommitment,
		DecryptableBalance:         decryptableNewBalance,
		Proofs:                     &proofs,
		Auditors:                   auditorsData,
	}, nil
}

func createTransferPartyParams(
	partyAddress string,
	transferLoBits uint16,
	transferHiBits uint32,
	senderKeyPair *elgamal.KeyPair,
	senderEncryptedTransferLoBits,
	senderEncryptedTransferHiBits *elgamal.Ciphertext,
	partyPubkey *curves.Point) (*TransferAuditor, error) {
	teg := elgamal.NewTwistedElgamal()

	// Encrypt the transfer amounts using the party's public key.
	encryptedTransferLoBits, loBitsRandomness, err := teg.Encrypt(*partyPubkey, uint64(transferLoBits))
	if err != nil {
		return &TransferAuditor{}, err
	}

	encryptedTransferHiBits, hiBitsRandomness, err := teg.Encrypt(*partyPubkey, uint64(transferHiBits))
	if err != nil {
		return &TransferAuditor{}, err
	}

	// Create validity proofs that the ciphertexts are valid (encrypted with the correct pubkey).
	loBitsValidityProof, err := zkproofs.NewCiphertextValidityProof(&loBitsRandomness, *partyPubkey, encryptedTransferLoBits, uint64(transferLoBits))
	if err != nil {
		return &TransferAuditor{}, err
	}

	hiBitsValidityProof, err := zkproofs.NewCiphertextValidityProof(&hiBitsRandomness, *partyPubkey, encryptedTransferHiBits, uint64(transferHiBits))
	if err != nil {
		return &TransferAuditor{}, err
	}

	// Lastly, we need to generate proof that the ciphertexts of the transfer amounts encrypt the same value as those for the sender.
	bigIntLoBits := new(big.Int).SetUint64(uint64(transferLoBits))
	loBitsScalar, err := curves.ED25519().Scalar.SetBigInt(bigIntLoBits)
	if err != nil {
		return &TransferAuditor{}, err
	}

	bigIntHiBits := new(big.Int).SetUint64(uint64(transferHiBits))
	hiBitsScalar, err := curves.ED25519().Scalar.SetBigInt(bigIntHiBits)
	if err != nil {
		return &TransferAuditor{}, err
	}

	ciphertextLoEqualityProof, err := zkproofs.NewCiphertextCiphertextEqualityProof(senderKeyPair, partyPubkey, senderEncryptedTransferLoBits, &loBitsRandomness, &loBitsScalar)
	if err != nil {
		return &TransferAuditor{}, err
	}

	ciphertextHiEqualityProof, err := zkproofs.NewCiphertextCiphertextEqualityProof(senderKeyPair, partyPubkey, senderEncryptedTransferHiBits, &hiBitsRandomness, &hiBitsScalar)
	if err != nil {
		return &TransferAuditor{}, err
	}

	return &TransferAuditor{
		Address:                       partyAddress,
		EncryptedTransferAmountLo:     encryptedTransferLoBits,
		EncryptedTransferAmountHi:     encryptedTransferHiBits,
		TransferAmountLoValidityProof: loBitsValidityProof,
		TransferAmountHiValidityProof: hiBitsValidityProof,
		TransferAmountLoEqualityProof: ciphertextLoEqualityProof,
		TransferAmountHiEqualityProof: ciphertextHiEqualityProof,
	}, nil
}

// Verifies the proofs sent in the transfer request. This does not verify proofs for auditors.
func VerifyTransferProofs(params *Transfer, senderPubkey *curves.Point, recipientPubkey *curves.Point, newBalanceCiphertext *elgamal.Ciphertext) error {
	// Verify the validity proofs that the ciphertexts sent are valid (encrypted with the correct pubkey).
	ok := zkproofs.VerifyCiphertextValidity(params.Proofs.RemainingBalanceCommitmentValidityProof, *senderPubkey, params.RemainingBalanceCommitment)
	if !ok {
		return errors.New("failed to verify remaining balance commitment")
	}

	ok = zkproofs.VerifyCiphertextValidity(params.Proofs.SenderTransferAmountLoValidityProof, *senderPubkey, params.SenderTransferAmountLo)
	if !ok {
		return errors.New("failed to verify sender transfer amount lo")
	}

	ok = zkproofs.VerifyCiphertextValidity(params.Proofs.SenderTransferAmountHiValidityProof, *senderPubkey, params.SenderTransferAmountHi)
	if !ok {
		return errors.New("failed to verify sender transfer amount hi")
	}

	ok = zkproofs.VerifyCiphertextValidity(params.Proofs.RecipientTransferAmountLoValidityProof, *recipientPubkey, params.RecipientTransferAmountLo)
	if !ok {
		return errors.New("failed to verify recipient transfer amount lo")
	}

	ok = zkproofs.VerifyCiphertextValidity(params.Proofs.RecipientTransferAmountHiValidityProof, *recipientPubkey, params.RecipientTransferAmountHi)
	if !ok {
		return errors.New("failed to verify recipient transfer amount hi")
	}

	// Verify that the account's remaining balance is greater than zero after this transfer.
	// This validates the RemainingBalanceCommitment sent by the user, so an additional check is needed to make sure this matches what is calculated by the server.
	ok, err := zkproofs.VerifyRangeProof(params.Proofs.RemainingBalanceRangeProof, params.RemainingBalanceCommitment, 64)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("range proof verification failed")
	}

	// As part of the range proof above, we verify that the RemainingBalanceCommitment sent by the user is equal to the remaining balance calculated by the server.
	ok = zkproofs.VerifyCiphertextCommitmentEquality(params.Proofs.RemainingBalanceEqualityProof, senderPubkey, newBalanceCiphertext, &params.RemainingBalanceCommitment.C)
	if !ok {
		return errors.New("ciphertext commitment equality verification failed")
	}

	// Lastly verify that the transferAmount ciphertexts encode the same value
	ok = zkproofs.VerifyCiphertextCiphertextEquality(params.Proofs.TransferAmountLoEqualityProof, senderPubkey, recipientPubkey, params.SenderTransferAmountLo, params.RecipientTransferAmountLo)
	if !ok {
		return errors.New("ciphertext ciphertext equality verification on transfer amount lo failed")
	}

	ok = zkproofs.VerifyCiphertextCiphertextEquality(params.Proofs.TransferAmountHiEqualityProof, senderPubkey, recipientPubkey, params.SenderTransferAmountHi, params.RecipientTransferAmountHi)
	if !ok {
		return errors.New("ciphertext ciphertext equality verification on transfer amount hi failed")
	}

	return nil
}

// Verifies the proofs sent for an individual auditor.
func VerifyAuditorProof(
	senderTransferAmountLo,
	senderTransferAmountHi *elgamal.Ciphertext,
	auditorParams *TransferAuditor,
	senderPubkey *curves.Point,
	auditorPubkey *curves.Point) error {
	// Verify that the transfer amounts are valid (encrypted with the correct pubkey).
	ok := zkproofs.VerifyCiphertextValidity(auditorParams.TransferAmountLoValidityProof, *auditorPubkey, auditorParams.EncryptedTransferAmountLo)
	if !ok {
		return errors.New("failed to verify auditor transfer amoun lo")
	}

	ok = zkproofs.VerifyCiphertextValidity(auditorParams.TransferAmountHiValidityProof, *auditorPubkey, auditorParams.EncryptedTransferAmountHi)
	if !ok {
		return errors.New("failed to verify auditor transfer amount hi")
	}

	// Then, verify that the transferAmount ciphertexts encode the same value
	ok = zkproofs.VerifyCiphertextCiphertextEquality(auditorParams.TransferAmountLoEqualityProof, senderPubkey, auditorPubkey, senderTransferAmountLo, auditorParams.EncryptedTransferAmountLo)
	if !ok {
		return errors.New("ciphertext ciphertext equality verification on auditor transfer amount lo failed")
	}

	ok = zkproofs.VerifyCiphertextCiphertextEquality(auditorParams.TransferAmountHiEqualityProof, senderPubkey, auditorPubkey, senderTransferAmountHi, auditorParams.EncryptedTransferAmountHi)
	if !ok {
		return errors.New("ciphertext ciphertext equality verification on auditor transfer amount hi failed")
	}

	return nil
}

// Decrypts the transfer transaction and returns the decrypted data.
// The method only works if the decryptor is the sender, recipient or an auditor on the transaction.
func (r *Transfer) Decrypt(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey, decryptAvailableBalance bool, decryptorAddress string) (*TransferDecrypted, error) {
	switch decryptorAddress {
	case r.FromAddress:
		if decryptAvailableBalance {
			return r.decryptWithAvailableBalanceAsSender(decryptor, privKey)
		} else {
			return r.decryptAsSender(decryptor, privKey)
		}
	case r.ToAddress:
		return r.decryptAsRecipient(decryptor, privKey)
	default:
		return r.decryptAsAuditor(decryptor, privKey, decryptorAddress)
	}
}

// Decrypts the Transfer object as a sender, while also attempting to perform the expensive operation of decrypting the NewBalanceCommitment.
// NOTE: Decryption of the NewBalanceCommitment can potentially take hours or be impossible even with the correct private key and should only be done when necessary.
func (r *Transfer) decryptWithAvailableBalanceAsSender(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey) (*TransferDecrypted, error) {
	decrypted, err := r.decryptAsSender(decryptor, privKey)
	if err != nil {
		return nil, err
	}

	keyPair, err := decryptor.KeyGen(privKey, r.Denom)
	if err != nil {
		return nil, err
	}

	remainingBalance, err := decryptor.Decrypt(keyPair.PrivateKey, r.RemainingBalanceCommitment, elgamal.MaxBits48)
	if err != nil {
		return nil, err
	}

	decrypted.RemainingBalanceCommitment = strconv.FormatUint(remainingBalance, 10)
	return decrypted, nil
}

// Decrypts the Transfer object as a sender,
func (r *Transfer) decryptAsSender(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey) (*TransferDecrypted, error) {
	keyPair, err := decryptor.KeyGen(privKey, r.Denom)
	if err != nil {
		return nil, err
	}

	transferAmountLo, err := decryptor.DecryptLargeNumber(keyPair.PrivateKey, r.SenderTransferAmountLo, elgamal.MaxBits16)
	if err != nil {
		return &TransferDecrypted{}, err
	}

	transferAmountHi, err := decryptor.DecryptLargeNumber(keyPair.PrivateKey, r.SenderTransferAmountHi, elgamal.MaxBits32)
	if err != nil {
		return &TransferDecrypted{}, err
	}

	aesKey, err := encryption.GetAESKey(privKey, r.Denom)
	if err != nil {
		return nil, err
	}

	decryptableBalance, err := encryption.DecryptAESGCM(r.DecryptableBalance, aesKey)
	if err != nil {
		return nil, err
	}
	decryptableBalanceString := strconv.FormatUint(decryptableBalance, 10)

	return NewTransferDecrypted(r, uint32(transferAmountLo), uint32(transferAmountHi), decryptableBalanceString), nil
}

// Decrypts the Transfer object as the listed recipient in the transfer
func (r *Transfer) decryptAsRecipient(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey) (*TransferDecrypted, error) {
	keyPair, err := decryptor.KeyGen(privKey, r.Denom)
	if err != nil {
		return nil, err
	}

	transferAmountLo, err := decryptor.Decrypt(keyPair.PrivateKey, r.RecipientTransferAmountLo, elgamal.MaxBits16)
	if err != nil {
		return &TransferDecrypted{}, err
	}

	transferAmountHi, err := decryptor.DecryptLargeNumber(keyPair.PrivateKey, r.RecipientTransferAmountHi, elgamal.MaxBits32)
	if err != nil {
		return &TransferDecrypted{}, err
	}

	return NewTransferDecrypted(r, uint32(transferAmountLo), uint32(transferAmountHi), "Not Decrypted"), nil
}

// Decrypts the Transfer object as one of the auditors on the transaction.
func (r *Transfer) decryptAsAuditor(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey, decryptorAddress string) (*TransferDecrypted, error) {
	keyPair, err := decryptor.KeyGen(privKey, r.Denom)
	if err != nil {
		return nil, err
	}

	for _, auditor := range r.Auditors {
		if auditor.Address == decryptorAddress {
			transferAmountLo, err := decryptor.Decrypt(keyPair.PrivateKey, auditor.EncryptedTransferAmountLo, elgamal.MaxBits16)
			if err != nil {
				return &TransferDecrypted{}, err
			}

			transferAmountHi, err := decryptor.DecryptLargeNumber(keyPair.PrivateKey, auditor.EncryptedTransferAmountHi, elgamal.MaxBits32)
			if err != nil {
				return &TransferDecrypted{}, err
			}

			return NewTransferDecrypted(r, uint32(transferAmountLo), uint32(transferAmountHi), "Not Decrypted"), nil
		}
	}

	return nil, errors.New("address not found in transfer transaction")
}

func NewTransferDecrypted(transfer *Transfer, transferAmountLo uint32, transferAmountHi uint32, decryptableBalance string) *TransferDecrypted {
	auditorAddrs := make([]string, len(transfer.Auditors))
	for i, auditor := range transfer.Auditors {
		auditorAddrs[i] = auditor.Address
	}

	return &TransferDecrypted{
		FromAddress:                transfer.FromAddress,
		ToAddress:                  transfer.ToAddress,
		Denom:                      transfer.Denom,
		TransferAmountLo:           transferAmountLo,
		TransferAmountHi:           transferAmountHi,
		TotalTransferAmount:        utils.CombineTransferAmount(uint16(transferAmountLo), transferAmountHi),
		RemainingBalanceCommitment: "Not Decrypted",
		DecryptableBalance:         decryptableBalance,
		Proofs:                     NewTransferMsgProofs(transfer.Proofs),
		Auditors:                   auditorAddrs,
	}
}
