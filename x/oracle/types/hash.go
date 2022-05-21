package types

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v2"

	"github.com/tendermint/tendermint/crypto/tmhash"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ yaml.Marshaler = AggregateVoteHash{}

// AggregateVoteHash is hash value to hide vote exchange rates
// which is formatted as hex string in SHA256("{salt}:{exchange rate}{denom},...,{exchange rate}{denom}:{voter}")
type AggregateVoteHash []byte

// GetAggregateVoteHash computes hash value of ExchangeRateVote
// to avoid redundant DecCoins stringify operation, use string argument
func GetAggregateVoteHash(salt string, exchangeRatesStr string, voter sdk.ValAddress) AggregateVoteHash {
	hash := tmhash.NewTruncated()
	sourceStr := fmt.Sprintf("%s:%s:%s", salt, exchangeRatesStr, voter.String())
	_, err := hash.Write([]byte(sourceStr))
	if err != nil {
		panic(err)
	}
	bz := hash.Sum(nil)
	return bz
}

// AggregateVoteHashFromHexString convert hex string to AggregateVoteHash
func AggregateVoteHashFromHexString(s string) (AggregateVoteHash, error) {
	h, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}

	return h, nil
}

// String implements fmt.Stringer interface
func (h AggregateVoteHash) String() string {
	return hex.EncodeToString(h)
}

// Equal does bytes equal check
func (h AggregateVoteHash) Equal(h2 AggregateVoteHash) bool {
	return bytes.Equal(h, h2)
}

// Empty check the name hash has zero length
func (h AggregateVoteHash) Empty() bool {
	return len(h) == 0
}

// Bytes returns the raw address bytes.
func (h AggregateVoteHash) Bytes() []byte {
	return h
}

// Size returns the raw address bytes.
func (h AggregateVoteHash) Size() int {
	return len(h)
}

// Format implements the fmt.Formatter interface.
func (h AggregateVoteHash) Format(s fmt.State, verb rune) {
	switch verb {
	case 's':
		_, _ = s.Write([]byte(h.String()))
	case 'p':
		_, _ = s.Write([]byte(fmt.Sprintf("%p", h)))
	default:
		_, _ = s.Write([]byte(fmt.Sprintf("%X", []byte(h))))
	}
}

// Marshal returns the raw address bytes. It is needed for protobuf
// compatibility.
func (h AggregateVoteHash) Marshal() ([]byte, error) {
	return h, nil
}

// Unmarshal sets the address to the given data. It is needed for protobuf
// compatibility.
func (h *AggregateVoteHash) Unmarshal(data []byte) error {
	*h = data
	return nil
}

// MarshalJSON marshals to JSON using Bech32.
func (h AggregateVoteHash) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.String())
}

// MarshalYAML marshals to YAML using Bech32.
func (h AggregateVoteHash) MarshalYAML() (interface{}, error) {
	return h.String(), nil
}

// UnmarshalJSON unmarshals from JSON assuming Bech32 encoding.
func (h *AggregateVoteHash) UnmarshalJSON(data []byte) error {
	var s string
	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}

	h2, err := AggregateVoteHashFromHexString(s)
	if err != nil {
		return err
	}

	*h = h2
	return nil
}
