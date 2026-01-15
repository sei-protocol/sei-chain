package ed25519

// Another way to leak a secret. Needed for LegacyAminoEncoding.
func (k SecretKey) MarshalAmino() ([]byte, error) {
	return k.SecretBytes(), nil
}

func (k *SecretKey) UnmarshalAmino(secretBytes []byte) error {
	x, err := SecretKeyFromSecretBytes(secretBytes)
	if err != nil {
		return err
	}
	*k = x
	return nil
}

func (k PublicKey) MarshalAmino() ([]byte, error) {
	return k.Bytes(), nil
}

func (k *PublicKey) UnmarshalAmino(bytes []byte) error {
	x, err := PublicKeyFromBytes(bytes)
	if err != nil {
		return err
	}
	*k = x
	return nil
}
