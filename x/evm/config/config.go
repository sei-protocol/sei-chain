package config

import "math/big"

const DefaultChainID = int64(713715)

// ChainIDMapping is a mapping of cosmos chain IDs to their respective chain IDs.
var ChainIDMapping = map[string]int64{
	// pacific-1 chain ID == 0x531
	"pacific-1": int64(1329),
	// atlantic-2 chain ID == 0x530
	"atlantic-2": int64(1328),
	"arctic-1":   int64(713715),
}

func GetEVMChainID(cosmosChainID string) *big.Int {
	if evmChainID, ok := ChainIDMapping[cosmosChainID]; ok {
		return big.NewInt(evmChainID)
	}
	return big.NewInt(DefaultChainID)
}
