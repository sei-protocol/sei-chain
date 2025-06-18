package wasmd

import (
	"github.com/ethereum/go-ethereum/core/vm"
	wasmdv552 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v552"
	wasmdv555 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v555"
	wasmdv562 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v562"
	wasmdv575 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v575"
	wasmdv580 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v580"
	wasmdv600 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v600"
	wasmdv601 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v601"
	wasmdv603 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v603"
	wasmdv605 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v605"
	wasmdv606 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v606"
	wasmdv610 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v610"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v5.5.2":      check(wasmdv552.NewPrecompile(keepers)),
		"v5.5.5":      check(wasmdv555.NewPrecompile(keepers)),
		"v5.6.2":      check(wasmdv562.NewPrecompile(keepers)),
		"v5.7.5":      check(wasmdv575.NewPrecompile(keepers)),
		"v5.8.0":      check(wasmdv580.NewPrecompile(keepers)),
		"v6.0.0":      check(wasmdv600.NewPrecompile(keepers)),
		"v6.0.1":      check(wasmdv601.NewPrecompile(keepers)),
		"v6.0.3":      check(wasmdv603.NewPrecompile(keepers)),
		"v6.0.5":      check(wasmdv605.NewPrecompile(keepers)),
		"v6.0.6":      check(wasmdv606.NewPrecompile(keepers)),
		"v6.1.0":      check(wasmdv610.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
