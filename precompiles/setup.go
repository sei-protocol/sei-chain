package precompiles

import (
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	"github.com/sei-protocol/sei-chain/precompiles/common"
)

func InitializePrecompiles(
	evmKeeper common.EVMKeeper,
	bankKeeper common.BankKeeper,
) error {
	bankp, err := bank.NewPrecompile(bankKeeper, evmKeeper)
	if err != nil {
		return err
	}
	vm.PrecompiledContractsHomestead[bankp.Address()] = bankp
	vm.PrecompiledContractsByzantium[bankp.Address()] = bankp
	vm.PrecompiledContractsIstanbul[bankp.Address()] = bankp
	vm.PrecompiledContractsBerlin[bankp.Address()] = bankp
	vm.PrecompiledContractsCancun[bankp.Address()] = bankp
	vm.PrecompiledContractsBLS[bankp.Address()] = bankp
	vm.PrecompiledAddressesHomestead = append(vm.PrecompiledAddressesHomestead, bankp.Address())
	vm.PrecompiledAddressesByzantium = append(vm.PrecompiledAddressesByzantium, bankp.Address())
	vm.PrecompiledAddressesIstanbul = append(vm.PrecompiledAddressesIstanbul, bankp.Address())
	vm.PrecompiledAddressesBerlin = append(vm.PrecompiledAddressesBerlin, bankp.Address())
	vm.PrecompiledAddressesCancun = append(vm.PrecompiledAddressesCancun, bankp.Address())
	return nil
}
