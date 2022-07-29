package dex

import (
	"sync"

	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
)

func deepCopyNestedMap(
	source *sync.Map,
	copier func(contractAddr typesutils.ContractAddress, pair typesutils.PairString, val any),
) {
	source.Range(func(key, val any) bool {
		contractAddr := key.(typesutils.ContractAddress)
		_map := val.(*sync.Map)
		_map.Range(func(pkey, pval any) bool {
			pair := pkey.(typesutils.PairString)
			copier(contractAddr, pair, pval)
			return true
		})
		return true
	})
}

func deepCopyMap(
	source *sync.Map,
	copier func(contractAddr typesutils.ContractAddress, val any),
) {
	source.Range(func(key, val any) bool {
		contractAddr := key.(typesutils.ContractAddress)
		copier(contractAddr, val)
		return true
	})
}
