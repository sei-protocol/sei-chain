package types

import sdk "github.com/cosmos/cosmos-sdk/types"

type AddressHandler interface {
	GetSeiAddressFromString(address string) (sdk.AccAddress, error)
}

type SeiAddressHandler struct{}

func (h SeiAddressHandler) GetSeiAddressFromString(address string) (sdk.AccAddress, error) {
	parsedAddress, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		return nil, err
	}
	return parsedAddress, nil
}
