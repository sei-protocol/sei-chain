package precompiles

import (
	"sync"

	ecommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/addr"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	"github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/json"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
)

var SetupMtx = &sync.Mutex{}
var Initialized = false

func InitializePrecompiles(
	evmKeeper common.EVMKeeper,
	bankKeeper common.BankKeeper,
	wasmdKeeper common.WasmdKeeper,
	wasmdViewKeeper common.WasmdViewKeeper,
) error {
	SetupMtx.Lock()
	defer SetupMtx.Unlock()
	if Initialized {
		panic("precompiles already initialized")
	}
	bankp, err := bank.NewPrecompile(bankKeeper, evmKeeper)
	if err != nil {
		return err
	}
	addPrecompileToVM(bankp, bankp.Address())
	wasmdp, err := wasmd.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper)
	if err != nil {
		return err
	}
	addPrecompileToVM(wasmdp, wasmdp.Address())
	jsonp, err := json.NewPrecompile()
	if err != nil {
		return err
	}
	addPrecompileToVM(jsonp, jsonp.Address())
	addrp, err := addr.NewPrecompile(evmKeeper)
	if err != nil {
		return err
	}
	addPrecompileToVM(addrp, addrp.Address())
	Initialized = true
	return nil
}

// This function modifies global variable in `vm` module. It should only be called once
// per precompile during initialization
func addPrecompileToVM(p vm.PrecompiledContract, addr ecommon.Address) {
	vm.PrecompiledContractsHomestead[addr] = p
	vm.PrecompiledContractsByzantium[addr] = p
	vm.PrecompiledContractsIstanbul[addr] = p
	vm.PrecompiledContractsBerlin[addr] = p
	vm.PrecompiledContractsCancun[addr] = p
	vm.PrecompiledContractsBLS[addr] = p
	vm.PrecompiledAddressesHomestead = append(vm.PrecompiledAddressesHomestead, addr)
	vm.PrecompiledAddressesByzantium = append(vm.PrecompiledAddressesByzantium, addr)
	vm.PrecompiledAddressesIstanbul = append(vm.PrecompiledAddressesIstanbul, addr)
	vm.PrecompiledAddressesBerlin = append(vm.PrecompiledAddressesBerlin, addr)
	vm.PrecompiledAddressesCancun = append(vm.PrecompiledAddressesCancun, addr)
}
