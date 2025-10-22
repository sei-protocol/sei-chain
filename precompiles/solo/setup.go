package solo

import (
	"github.com/ethereum/go-ethereum/core/vm"
	solov614 "github.com/sei-protocol/sei-chain/precompiles/solo/legacy/v614"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v6.1.4":      check(solov614.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
