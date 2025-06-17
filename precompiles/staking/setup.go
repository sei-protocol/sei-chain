package staking

import (
	"github.com/ethereum/go-ethereum/core/vm"
	stakingv552 "github.com/sei-protocol/sei-chain/precompiles/staking/legacy/v552"
	stakingv555 "github.com/sei-protocol/sei-chain/precompiles/staking/legacy/v555"
	stakingv562 "github.com/sei-protocol/sei-chain/precompiles/staking/legacy/v562"
	stakingv580 "github.com/sei-protocol/sei-chain/precompiles/staking/legacy/v580"
	stakingv605 "github.com/sei-protocol/sei-chain/precompiles/staking/legacy/v605"
	stakingv606 "github.com/sei-protocol/sei-chain/precompiles/staking/legacy/v606"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v5.5.2":      check(stakingv552.NewPrecompile(keepers)),
		"v5.5.5":      check(stakingv555.NewPrecompile(keepers)),
		"v5.6.2":      check(stakingv562.NewPrecompile(keepers)),
		"v5.8.0":      check(stakingv580.NewPrecompile(keepers)),
		"v6.0.5":      check(stakingv605.NewPrecompile(keepers)),
		"v6.0.6":      check(stakingv606.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
