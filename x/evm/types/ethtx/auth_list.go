package ethtx

import (
	"math"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
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

	for i, auth := range al {
		chainId := new(uint256.Int)
		chainId.SetFromBig(auth.ChainID.BigInt())
		v := new(uint256.Int)
		v.SetBytes(auth.V)

		var v8 uint8
		if v.Uint64() > math.MaxUint8 {
			panic("v value too large to fit in uint8")
		}
		v8 = uint8(v.Uint64()) //nolint:gosec

		r := new(uint256.Int)
		r.SetBytes(auth.R)
		s := new(uint256.Int)
		s.SetBytes(auth.S)
		ethAuthList[i] = ethtypes.SetCodeAuthorization{
			ChainID: *chainId,
			Address: common.HexToAddress(auth.Address),
			Nonce:   auth.Nonce,
			V:       v8,
			R:       *r,
			S:       *s,
		}
	}

	return &ethAuthList
}
