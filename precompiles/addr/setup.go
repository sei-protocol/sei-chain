package addr

import (
	"github.com/ethereum/go-ethereum/core/vm"
	addrv552 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v552"
	addrv555 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v555"
	addrv562 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v562"
	addrv575 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v575"
	addrv600 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v600"
	addrv601 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v601"
	addrv603 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v603"
	addrv605 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v605"
	addrv606 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v606"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v5.5.2":      check(addrv552.NewPrecompile(keepers)),
		"v5.5.5":      check(addrv555.NewPrecompile(keepers)),
		"v5.6.2":      check(addrv562.NewPrecompile(keepers)),
		"v5.7.5":      check(addrv575.NewPrecompile(keepers)),
		"v6.0.0":      check(addrv600.NewPrecompile(keepers)),
		"v6.0.1":      check(addrv601.NewPrecompile(keepers)),
		"v6.0.3":      check(addrv603.NewPrecompile(keepers)),
		"v6.0.5":      check(addrv605.NewPrecompile(keepers)),
		"v6.0.6":      check(addrv606.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
