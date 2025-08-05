package ibc

import (
	"github.com/ethereum/go-ethereum/core/vm"
	ibcv552 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v552"
	ibcv555 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v555"
	ibcv562 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v562"
	ibcv580 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v580"
	ibcv601 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v601"
	ibcv603 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v603"
	ibcv605 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v605"
	ibcv606 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v606"
	ibcv610 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v610"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v5.5.2":      check(ibcv552.NewPrecompile(keepers)),
		"v5.5.5":      check(ibcv555.NewPrecompile(keepers)),
		"v5.6.2":      check(ibcv562.NewPrecompile(keepers)),
		"v5.8.0":      check(ibcv580.NewPrecompile(keepers)),
		"v6.0.1":      check(ibcv601.NewPrecompile(keepers)),
		"v6.0.3":      check(ibcv603.NewPrecompile(keepers)),
		"v6.0.5":      check(ibcv605.NewPrecompile(keepers)),
		"v6.0.6":      check(ibcv606.NewPrecompile(keepers)),
		"v6.1.0":      check(ibcv610.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
