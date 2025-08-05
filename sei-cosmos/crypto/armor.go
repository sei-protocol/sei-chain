package crypto

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/tendermint/crypto/bcrypt"
	"github.com/tendermint/tendermint/crypto"
	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/openpgp/armor"

	cosmoscrypto "github.com/cosmos/cosmos-sdk/crypto/utils"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	blockTypePrivKey = "TENDERMINT PRIVATE KEY"
	blockTypeKeyInfo = "TENDERMINT KEY INFO"
	blockTypePubKey  = "TENDERMINT PUBLIC KEY"

	defaultAlgo = "secp256k1"

	headerVersion = "version"
	headerType    = "type"
)

func EncodeArmor(blockType string, headers map[string]string, data []byte) string {
	buf := new(bytes.Buffer)
	w, err := armor.Encode(buf, blockType, headers)
	if err != nil {
		panic(fmt.Errorf("could not encode ascii armor: %s", err))
	}
	_, err = w.Write(data)
	if err != nil {
		panic(fmt.Errorf("could not encode ascii armor: %s", err))
	}
	err = w.Close()
	if err != nil {
		panic(fmt.Errorf("could not encode ascii armor: %s", err))
	}
	return buf.String()
}

func DecodeArmor(armorStr string) (blockType string, headers map[string]string, data []byte, err error) {
	buf := bytes.NewBufferString(armorStr)
	block, err := armor.Decode(buf)
	if err != nil {
		return "", nil, nil, err
	}
	data, err = ioutil.ReadAll(block.Body)
	if err != nil {
		return "", nil, nil, err
	}
	return block.Type, block.Header, data, nil
}

const nonceLen = 24
const secretLen = 32

// secret must be 32 bytes long. Use something like Sha256(Bcrypt(passphrase))
// The ciphertext is (secretbox.Overhead + 24) bytes longer than the plaintext.
func EncryptSymmetric(plaintext []byte, secret []byte) (ciphertext []byte) {
	if len(secret) != secretLen {
		panic(fmt.Sprintf("Secret must be 32 bytes long, got len %v", len(secret)))
	}
	nonce := crypto.CRandBytes(nonceLen)
	nonceArr := [nonceLen]byte{}
	copy(nonceArr[:], nonce)
	secretArr := [secretLen]byte{}
	copy(secretArr[:], secret)
	ciphertext = make([]byte, nonceLen+secretbox.Overhead+len(plaintext))
	copy(ciphertext, nonce)
	secretbox.Seal(ciphertext[nonceLen:nonceLen], plaintext, &nonceArr, &secretArr)
	return ciphertext
}

// secret must be 32 bytes long. Use something like Sha256(Bcrypt(passphrase))
// The ciphertext is (secretbox.Overhead + 24) bytes longer than the plaintext.
func DecryptSymmetric(ciphertext []byte, secret []byte) (plaintext []byte, err error) {
	if len(secret) != secretLen {
		panic(fmt.Sprintf("Secret must be 32 bytes long, got len %v", len(secret)))
	}
	if len(ciphertext) <= secretbox.Overhead+nonceLen {
		return nil, errors.New("ciphertext is too short")
	}
	nonce := ciphertext[:nonceLen]
	nonceArr := [nonceLen]byte{}
	copy(nonceArr[:], nonce)
	secretArr := [secretLen]byte{}
	copy(secretArr[:], secret)
	plaintext = make([]byte, len(ciphertext)-nonceLen-secretbox.Overhead)
	_, ok := secretbox.Open(plaintext[:0], ciphertext[nonceLen:], &nonceArr, &secretArr)
	if !ok {
		return nil, errors.New("ciphertext decryption failed")
	}
	return plaintext, nil
}

// BcryptSecurityParameter is security parameter var, and it can be changed within the lcd test.
// Making the bcrypt security parameter a var shouldn't be a security issue:
// One can't verify an invalid key by maliciously changing the bcrypt
// parameter during a runtime vulnerability. The main security
// threat this then exposes would be something that changes this during
// runtime before the user creates their key. This vulnerability must
// succeed to update this to that same value before every subsequent call
// to the keys command in future startups / or the attacker must get access
// to the filesystem. However, with a similar threat model (changing
// variables in runtime), one can cause the user to sign a different tx
// than what they see, which is a significantly cheaper attack then breaking
// a bcrypt hash. (Recall that the nonce still exists to break rainbow tables)
// For further notes on security parameter choice, see README.md
var BcryptSecurityParameter = 12

//-----------------------------------------------------------------
// encrypt/decrypt with armor

// Encrypt and armor the private key.
func EncryptArmorPrivKey(privKeyBytes []byte, passphrase string, algo string) string {
	saltBytes, encBytes := encryptPrivKey(privKeyBytes, passphrase)
	header := map[string]string{
		"kdf":  "bcrypt",
		"salt": fmt.Sprintf("%X", saltBytes),
	}

	if algo != "" {
		header[headerType] = algo
	}

	armorStr := EncodeArmor(blockTypePrivKey, header, encBytes)

	return armorStr
}

// encrypt the given privKey with the passphrase using a randomly
// generated salt and the xsalsa20 cipher. returns the salt and the
// encrypted priv key.
func encryptPrivKey(privKeyBytes []byte, passphrase string) (saltBytes []byte, encBytes []byte) {
	saltBytes = crypto.CRandBytes(16)
	key, err := bcrypt.GenerateFromPassword(saltBytes, []byte(passphrase), BcryptSecurityParameter)

	if err != nil {
		panic(sdkerrors.Wrap(err, "error generating bcrypt key from passphrase"))
	}

	key = cosmoscrypto.Sha256(key) // get 32 bytes

	return saltBytes, EncryptSymmetric(privKeyBytes, key)
}

// UnarmorDecryptPrivKey returns the privkey byte slice, a string of the algo type, and an error
func UnarmorDecryptPrivKey(armorStr string, passphrase string) (privKey []byte, algo string, err error) {
	blockType, header, encBytes, err := DecodeArmor(armorStr)
	if err != nil {
		return privKey, "", err
	}

	if blockType != blockTypePrivKey {
		return privKey, "", fmt.Errorf("unrecognized armor type: %v", blockType)
	}

	if header["kdf"] != "bcrypt" {
		return privKey, "", fmt.Errorf("unrecognized KDF type: %v", header["kdf"])
	}

	if header["salt"] == "" {
		return privKey, "", fmt.Errorf("missing salt bytes")
	}

	saltBytes, err := hex.DecodeString(header["salt"])
	if err != nil {
		return privKey, "", fmt.Errorf("error decoding salt: %v", err.Error())
	}

	privKey, err = decryptPrivKey(saltBytes, encBytes, passphrase)

	if header[headerType] == "" {
		header[headerType] = defaultAlgo
	}

	return privKey, header[headerType], err
}

func decryptPrivKey(saltBytes []byte, encBytes []byte, passphrase string) (privKey []byte, err error) {
	key, err := bcrypt.GenerateFromPassword(saltBytes, []byte(passphrase), BcryptSecurityParameter)
	if err != nil {
		return privKey, sdkerrors.Wrap(err, "error generating bcrypt key from passphrase")
	}

	key = cosmoscrypto.Sha256(key) // Get 32 bytes

	privKeyBytes, err := DecryptSymmetric(encBytes, key)
	if err != nil && err.Error() == "Ciphertext decryption failed" {
		return privKey, sdkerrors.ErrWrongPassword
	} else if err != nil {
		return privKey, err
	}

	// return legacy.PrivKeyFromBytes(privKeyBytes)
	return privKeyBytes, nil
}
