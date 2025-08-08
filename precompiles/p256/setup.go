package p256

import (
	"github.com/ethereum/go-ethereum/core/vm"
	p256v606 "github.com/sei-protocol/sei-chain/precompiles/p256/legacy/v606"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v6.0.6":      check(p256v606.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
