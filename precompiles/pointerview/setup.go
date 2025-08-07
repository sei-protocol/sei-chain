package pointerview

import (
	"github.com/ethereum/go-ethereum/core/vm"
	pointerviewv552 "github.com/sei-protocol/sei-chain/precompiles/pointerview/legacy/v552"
	pointerviewv555 "github.com/sei-protocol/sei-chain/precompiles/pointerview/legacy/v555"
	pointerviewv562 "github.com/sei-protocol/sei-chain/precompiles/pointerview/legacy/v562"
	pointerviewv605 "github.com/sei-protocol/sei-chain/precompiles/pointerview/legacy/v605"
	pointerviewv606 "github.com/sei-protocol/sei-chain/precompiles/pointerview/legacy/v606"
	pointerviewv610 "github.com/sei-protocol/sei-chain/precompiles/pointerview/legacy/v610"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
		"v5.5.2":      check(pointerviewv552.NewPrecompile(keepers)),
		"v5.5.5":      check(pointerviewv555.NewPrecompile(keepers)),
		"v5.6.2":      check(pointerviewv562.NewPrecompile(keepers)),
		"v6.0.5":      check(pointerviewv605.NewPrecompile(keepers)),
		"v6.0.6":      check(pointerviewv606.NewPrecompile(keepers)),
		"v6.1.0":      check(pointerviewv610.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
