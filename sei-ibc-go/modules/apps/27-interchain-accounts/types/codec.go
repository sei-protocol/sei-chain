package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

var (
	// ModuleCdc references the global interchain accounts module codec. Note, the codec
	// should ONLY be used in certain instances of tests and for JSON encoding.
	//
	// The actual codec used for serialization should be provided to interchain accounts and
	// defined at the application level.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

// RegisterLegacyAminoCodec registers the account interfaces and concrete types on the
// provided LegacyAmino codec. These types are used for Amino JSON serialization
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterInterface((*InterchainAccountI)(nil), nil)
	cdc.RegisterConcrete(&InterchainAccount{}, "27-interchain-accounts/InterchainAccount", nil)
}

// RegisterInterfaces registers the concrete InterchainAccount implementation against the associated
// x/auth AccountI and GenesisAccount interfaces
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*authtypes.AccountI)(nil), &InterchainAccount{})
	registry.RegisterImplementations((*authtypes.GenesisAccount)(nil), &InterchainAccount{})
}

// SerializeCosmosTx serializes a slice of sdk.Msg's using the CosmosTx type. The sdk.Msg's are
// packed into Any's and inserted into the Messages field of a CosmosTx. The proto marshaled CosmosTx
// bytes are returned.
func SerializeCosmosTx(cdc codec.BinaryCodec, msgs []sdk.Msg) (bz []byte, err error) {
	msgAnys := make([]*codectypes.Any, len(msgs))

	for i, msg := range msgs {
		msgAnys[i], err = codectypes.NewAnyWithValue(msg)
		if err != nil {
			return nil, err
		}
	}

	cosmosTx := &CosmosTx{
		Messages: msgAnys,
	}

	bz, err = cdc.Marshal(cosmosTx)
	if err != nil {
		return nil, err
	}

	return bz, nil
}

// DeserializeCosmosTx unmarshals and unpacks a slice of transaction bytes
// into a slice of sdk.Msg's.
func DeserializeCosmosTx(cdc codec.BinaryCodec, data []byte) ([]sdk.Msg, error) {
	var cosmosTx CosmosTx
	if err := cdc.Unmarshal(data, &cosmosTx); err != nil {
		return nil, err
	}

	msgs := make([]sdk.Msg, len(cosmosTx.Messages))

	for i, any := range cosmosTx.Messages {
		var msg sdk.Msg

		err := cdc.UnpackAny(any, &msg)
		if err != nil {
			return nil, err
		}

		msgs[i] = msg
	}

	return msgs, nil
}
