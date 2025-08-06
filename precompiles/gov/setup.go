package gov

import (
	"github.com/ethereum/go-ethereum/core/vm"
	govv552 "github.com/sei-protocol/sei-chain/precompiles/gov/legacy/v552"
	govv555 "github.com/sei-protocol/sei-chain/precompiles/gov/legacy/v555"
	govv562 "github.com/sei-protocol/sei-chain/precompiles/gov/legacy/v562"
	govv580 "github.com/sei-protocol/sei-chain/precompiles/gov/legacy/v580"
	govv605 "github.com/sei-protocol/sei-chain/precompiles/gov/legacy/v605"
	govv606 "github.com/sei-protocol/sei-chain/precompiles/gov/legacy/v606"
	govv610 "github.com/sei-protocol/sei-chain/precompiles/gov/legacy/v610"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v5.5.2":      check(govv552.NewPrecompile(keepers)),
		"v5.5.5":      check(govv555.NewPrecompile(keepers)),
		"v5.6.2":      check(govv562.NewPrecompile(keepers)),
		"v5.8.0":      check(govv580.NewPrecompile(keepers)),
		"v6.0.5":      check(govv605.NewPrecompile(keepers)),
		"v6.0.6":      check(govv606.NewPrecompile(keepers)),
		"v6.1.0":      check(govv610.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
