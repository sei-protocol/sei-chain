package ed25519

import (
	"encoding/json"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/jsontypes"
)

const SecretKeyName = "tendermint/PrivKeyEd25519" //nolint:gosec
const PublicKeyName = "tendermint/PubKeyEd25519"
const KeyType = "ed25519"

func init() {
	jsontypes.MustRegister(PublicKey{})
	jsontypes.MustRegister(SecretKey{})
}

func (k SecretKey) TypeTag() string { return SecretKeyName }
func (k SecretKey) Type() string    { return KeyType }

func (k PublicKey) TypeTag() string { return PublicKeyName }
func (k PublicKey) Type() string    { return KeyType }

// WARNING: this is very BAD that one can leak a secret by embedding
// a private key in some struct and then calling json.Marshal on it.
// TODO(gprusak): get rid of it.
func (k SecretKey) MarshalJSON() ([]byte, error) {
	return json.Marshal((*k.key)[:])
}

func (k *SecretKey) UnmarshalJSON(j []byte) error {
	var raw []byte
	if err := json.Unmarshal(j, &raw); err != nil {
		return err
	}
	x, err := SecretKeyFromSecretBytes(raw)
	if err != nil {
		return err
	}
	*k = x
	return nil
}

func (k PublicKey) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.Bytes())
}

func (k *PublicKey) UnmarshalJSON(j []byte) error {
	var raw []byte
	if err := json.Unmarshal(j, &raw); err != nil {
		return err
	}
	x, err := PublicKeyFromBytes(raw)
	if err != nil {
		return err
	}
	*k = x
	return nil
}

func (s Signature) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Bytes())
}

func (s *Signature) UnmarshalJSON(j []byte) error {
	var raw []byte
	if err := json.Unmarshal(j, &raw); err != nil {
		return err
	}
	x, err := SignatureFromBytes(raw)
	if err != nil {
		return err
	}
	*s = x
	return nil
}
