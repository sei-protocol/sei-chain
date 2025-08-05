package json

import (
	"github.com/ethereum/go-ethereum/core/vm"
	jsonv552 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v552"
	jsonv555 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v555"
	jsonv562 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v562"
	jsonv603 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v603"
	jsonv605 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v605"
	jsonv606 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v606"
	jsonv610 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v610"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v5.5.2":      check(jsonv552.NewPrecompile(keepers)),
		"v5.5.5":      check(jsonv555.NewPrecompile(keepers)),
		"v5.6.2":      check(jsonv562.NewPrecompile(keepers)),
		"v6.0.3":      check(jsonv603.NewPrecompile(keepers)),
		"v6.0.5":      check(jsonv605.NewPrecompile(keepers)),
		"v6.0.6":      check(jsonv606.NewPrecompile(keepers)),
		"v6.1.0":      check(jsonv610.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
