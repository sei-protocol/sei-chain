package ed25519

import (
	"encoding/json"
)

func (k PrivKey) MarshalJSON() ([]byte,error) {
	return json.Marshal(k.raw)
}

func (k *PrivKey) UnmarshalJSON(j []byte) error {
	return json.Unmarshal(j,&k.raw)
}

func (k PubKey) MarshalJSON() ([]byte,error) {
	return json.Marshal(k[:])
}

func (k *PubKey) UnmarshalJSON(j []byte) error {
	var raw []byte
	if err:=json.Unmarshal(j,&raw); err!=nil { return err }
	x,err := PubKeyFromBytes(raw)
	if err!=nil { return err }
	*k = x
	return nil
}
