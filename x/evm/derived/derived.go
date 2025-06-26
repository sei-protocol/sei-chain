package derived

import (
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

type SignerVersion int

const (
	London SignerVersion = iota
	Cancun
	Prague
)

type Derived struct {
	SenderEVMAddr common.Address
	SenderSeiAddr sdk.AccAddress
	PubKey        *secp256k1.PubKey
	IsAssociate   bool
	Version       SignerVersion
}

// Derived should never come from deserialization or be transmitted after serialization,
// so all methods below would no-op.
func (d Derived) Marshal() ([]byte, error)             { return []byte{}, nil }
func (d *Derived) MarshalTo([]byte) (n int, err error) { return }
func (d *Derived) Unmarshal([]byte) error              { return nil }
func (d *Derived) Size() int                           { return 0 }

func (d Derived) MarshalJSON() ([]byte, error) { return []byte{}, nil }
func (d *Derived) UnmarshalJSON([]byte) error  { return nil }
