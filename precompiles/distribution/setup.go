package distribution

import (
	"github.com/ethereum/go-ethereum/core/vm"
	distributionv552 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v552"
	distributionv555 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v555"
	distributionv562 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v562"
	distributionv580 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v580"
	distributionv605 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v605"
	distributionv606 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v606"
	distributionv610 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v610"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v5.5.2":      check(distributionv552.NewPrecompile(keepers)),
		"v5.5.5":      check(distributionv555.NewPrecompile(keepers)),
		"v5.6.2":      check(distributionv562.NewPrecompile(keepers)),
		"v5.8.0":      check(distributionv580.NewPrecompile(keepers)),
		"v6.0.5":      check(distributionv605.NewPrecompile(keepers)),
		"v6.0.6":      check(distributionv606.NewPrecompile(keepers)),
		"v6.1.0":      check(distributionv610.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
