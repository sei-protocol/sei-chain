package ibc

import (
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

// GetVersioned returns the retired IBC precompile for every supported upgrade version.
func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	tombstone := check(NewPrecompile(keepers))
	return utils.VersionedPrecompiles{
		latestUpgrade: tombstone,
		"v5.5.2":      tombstone,
		"v5.5.5":      tombstone,
		"v5.6.2":      tombstone,
		"v5.8.0":      tombstone,
		"v6.0.1":      tombstone,
		"v6.0.3":      tombstone,
		"v6.0.5":      tombstone,
		"v6.0.6":      tombstone,
		"v6.1.0":      tombstone,
		"v6.1.4":      tombstone,
		"v6.2.0":      tombstone,
		"v6.3.0":      tombstone,
		"v6.4.0":      tombstone,
		"v6.5":        tombstone,
		"v6.6":        tombstone,
		"v6.7":        tombstone,
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
