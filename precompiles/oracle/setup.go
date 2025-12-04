package oracle

import (
	"github.com/ethereum/go-ethereum/core/vm"
	oraclev552 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v552"
	oraclev555 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v555"
	oraclev562 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v562"
	oraclev600 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v600"
	oraclev601 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v601"
	oraclev603 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v603"
	oraclev605 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v605"
	oraclev606 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v606"
	oraclev610 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v610"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v5.5.2":      check(oraclev552.NewPrecompile(keepers)),
		"v5.5.5":      check(oraclev555.NewPrecompile(keepers)),
		"v5.6.2":      check(oraclev562.NewPrecompile(keepers)),
		"v6.0.0":      check(oraclev600.NewPrecompile(keepers)),
		"v6.0.1":      check(oraclev601.NewPrecompile(keepers)),
		"v6.0.3":      check(oraclev603.NewPrecompile(keepers)),
		"v6.0.5":      check(oraclev605.NewPrecompile(keepers)),
		"v6.0.6":      check(oraclev606.NewPrecompile(keepers)),
		"v6.1.0":      check(oraclev610.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
