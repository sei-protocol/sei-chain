package ethtx

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

type AuthList []SetCodeAuthorization

func NewAuthList(ethAuthList *[]ethtypes.SetCodeAuthorization) AuthList {
	if ethAuthList == nil {
		return nil
	}

	al := AuthList{}
	for _, auth := range *ethAuthList {
		chainId := sdk.NewIntFromBigInt(auth.ChainID.ToBig())
		al = append(al, SetCodeAuthorization{
			ChainID: &chainId,
			Address: auth.Address.String(),
			Nonce:   auth.Nonce,
			V:       []byte{auth.V},
			R:       (auth.R).Bytes(),
			S:       (auth.S).Bytes(),
		})
	}

	return al
}

func (al AuthList) ToEthAuthList() *[]ethtypes.SetCodeAuthorization {
	ethAuthList := make([]ethtypes.SetCodeAuthorization, len(al))

	for _, auth := range al {
		chainId := new(uint256.Int)
		chainId.SetFromBig(auth.ChainID.BigInt())
		v := new(uint256.Int)
		v.SetBytes(auth.V)
		r := new(uint256.Int)
		r.SetBytes(auth.R)
		s := new(uint256.Int)
		s.SetBytes(auth.S)
		ethAuthList = append(ethAuthList, ethtypes.SetCodeAuthorization{
			ChainID: *chainId,
			Address: common.HexToAddress(auth.Address),
			Nonce:   auth.Nonce,
			V:       uint8(v.Uint64()),
			R:       *r,
			S:       *s,
		})
	}

	return &ethAuthList
}
