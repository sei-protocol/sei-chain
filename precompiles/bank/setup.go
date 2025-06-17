package bank

import (
	"github.com/ethereum/go-ethereum/core/vm"
	bankv552 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v552"
	bankv555 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v555"
	bankv562 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v562"
	bankv580 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v580"
	bankv600 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v600"
	bankv601 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v601"
	bankv603 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v603"
	bankv605 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v605"
	bankv606 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v606"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v5.5.2":      check(bankv552.NewPrecompile(keepers)),
		"v5.5.5":      check(bankv555.NewPrecompile(keepers)),
		"v5.6.2":      check(bankv562.NewPrecompile(keepers)),
		"v5.8.0":      check(bankv580.NewPrecompile(keepers)),
		"v6.0.0":      check(bankv600.NewPrecompile(keepers)),
		"v6.0.1":      check(bankv601.NewPrecompile(keepers)),
		"v6.0.3":      check(bankv603.NewPrecompile(keepers)),
		"v6.0.5":      check(bankv605.NewPrecompile(keepers)),
		"v6.0.6":      check(bankv606.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
