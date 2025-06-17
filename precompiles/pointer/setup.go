package pointer

import (
	"github.com/ethereum/go-ethereum/core/vm"
	pointerv552 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v552"
	pointerv555 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v555"
	pointerv562 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v562"
	pointerv575 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v575"
	pointerv580 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v580"
	pointerv600 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v600"
	pointerv605 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v605"
	pointerv606 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v606"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v5.5.2":      check(pointerv552.NewPrecompile(keepers)),
		"v5.5.5":      check(pointerv555.NewPrecompile(keepers)),
		"v5.6.2":      check(pointerv562.NewPrecompile(keepers)),
		"v5.7.5":      check(pointerv575.NewPrecompile(keepers)),
		"v5.8.0":      check(pointerv580.NewPrecompile(keepers)),
		"v6.0.0":      check(pointerv600.NewPrecompile(keepers)),
		"v6.0.5":      check(pointerv605.NewPrecompile(keepers)),
		"v6.0.6":      check(pointerv606.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
