package hashable

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func GenHash[T Hashable](rng utils.Rng) Hash[T] {
	return Hash[T](utils.GenBytes(rng, len(Hash[T]{})))
}
