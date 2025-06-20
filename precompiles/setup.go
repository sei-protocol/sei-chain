package precompiles

import (
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ecommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/addr"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	"github.com/sei-protocol/sei-chain/precompiles/distribution"
	"github.com/sei-protocol/sei-chain/precompiles/gov"
	"github.com/sei-protocol/sei-chain/precompiles/ibc"
	"github.com/sei-protocol/sei-chain/precompiles/json"
	"github.com/sei-protocol/sei-chain/precompiles/oracle"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	"github.com/sei-protocol/sei-chain/precompiles/pointerview"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
)

var SetupMtx = &sync.Mutex{}
var Initialized = false

type PrecompileInfo struct {
	ABI     abi.ABI
	Address ecommon.Address
}

// PrecompileNamesToInfo is Populated by InitializePrecompiles
var PrecompileNamesToInfo = map[string]PrecompileInfo{}

type IPrecompile interface {
	vm.PrecompiledContract
	GetABI() abi.ABI
	GetName() string
	Address() ecommon.Address
}

func GetCustomPrecompiles(
	latestUpgrade string,
	keepers utils.Keepers,
) map[ecommon.Address]utils.VersionedPrecompiles {
	return map[ecommon.Address]utils.VersionedPrecompiles{
		ecommon.HexToAddress(bank.BankAddress):               bank.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(wasmd.WasmdAddress):             wasmd.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(json.JSONAddress):               json.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(addr.AddrAddress):               addr.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(staking.StakingAddress):         staking.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(gov.GovAddress):                 gov.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(distribution.DistrAddress):      distribution.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(oracle.OracleAddress):           oracle.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(ibc.IBCAddress):                 ibc.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(pointer.PointerAddress):         pointer.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(pointerview.PointerViewAddress): pointerview.GetVersioned(latestUpgrade, keepers),
	}
}

func InitializePrecompiles(
	dryRun bool,
	keepers utils.Keepers,
) error {
	SetupMtx.Lock()
	defer SetupMtx.Unlock()
	if Initialized {
		panic("precompiles already initialized")
	}
	bankp, err := bank.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	wasmdp, err := wasmd.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	jsonp, err := json.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	addrp, err := addr.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	stakingp, err := staking.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	govp, err := gov.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	distrp, err := distribution.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	oraclep, err := oracle.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	ibcp, err := ibc.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	pointerp, err := pointer.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	pointerviewp, err := pointerview.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	PrecompileNamesToInfo[bankp.GetName()] = PrecompileInfo{ABI: bankp.GetABI(), Address: bankp.Address()}
	PrecompileNamesToInfo[wasmdp.GetName()] = PrecompileInfo{ABI: wasmdp.GetABI(), Address: wasmdp.Address()}
	PrecompileNamesToInfo[jsonp.GetName()] = PrecompileInfo{ABI: jsonp.GetABI(), Address: jsonp.Address()}
	PrecompileNamesToInfo[addrp.GetName()] = PrecompileInfo{ABI: addrp.GetABI(), Address: addrp.Address()}
	PrecompileNamesToInfo[stakingp.GetName()] = PrecompileInfo{ABI: stakingp.GetABI(), Address: stakingp.Address()}
	PrecompileNamesToInfo[govp.GetName()] = PrecompileInfo{ABI: govp.GetABI(), Address: govp.Address()}
	PrecompileNamesToInfo[distrp.GetName()] = PrecompileInfo{ABI: distrp.GetABI(), Address: distrp.Address()}
	PrecompileNamesToInfo[oraclep.GetName()] = PrecompileInfo{ABI: oraclep.GetABI(), Address: oraclep.Address()}
	PrecompileNamesToInfo[ibcp.GetName()] = PrecompileInfo{ABI: ibcp.GetABI(), Address: ibcp.Address()}
	PrecompileNamesToInfo[pointerp.GetName()] = PrecompileInfo{ABI: pointerp.GetABI(), Address: pointerp.Address()}
	PrecompileNamesToInfo[pointerviewp.GetName()] = PrecompileInfo{ABI: pointerviewp.GetABI(), Address: pointerviewp.Address()}
	if !dryRun {
		addPrecompileToVM(bankp)
		addPrecompileToVM(wasmdp)
		addPrecompileToVM(jsonp)
		addPrecompileToVM(addrp)
		addPrecompileToVM(stakingp)
		addPrecompileToVM(govp)
		addPrecompileToVM(distrp)
		addPrecompileToVM(oraclep)
		addPrecompileToVM(ibcp)
		addPrecompileToVM(pointerp)
		addPrecompileToVM(pointerviewp)
		Initialized = true
	}
	return nil
}

func GetPrecompileInfo(name string) PrecompileInfo {
	if !Initialized {
		// Precompile Info does not require any keeper state
		_ = InitializePrecompiles(true, &utils.EmptyKeepers{})
	}
	i, ok := PrecompileNamesToInfo[name]
	if !ok {
		panic(name + "doesn't exist as a precompile")
	}
	return i
}

// This function modifies global variable in `vm` module. It should only be called once
// per precompile during initialization
func addPrecompileToVM(p IPrecompile) {
	vm.PrecompiledContractsHomestead[p.Address()] = p
	vm.PrecompiledContractsByzantium[p.Address()] = p
	vm.PrecompiledContractsIstanbul[p.Address()] = p
	vm.PrecompiledContractsBerlin[p.Address()] = p
	vm.PrecompiledContractsCancun[p.Address()] = p
	vm.PrecompiledContractsBLS[p.Address()] = p
	vm.PrecompiledAddressesHomestead = append(vm.PrecompiledAddressesHomestead, p.Address())
	vm.PrecompiledAddressesByzantium = append(vm.PrecompiledAddressesByzantium, p.Address())
	vm.PrecompiledAddressesIstanbul = append(vm.PrecompiledAddressesIstanbul, p.Address())
	vm.PrecompiledAddressesBerlin = append(vm.PrecompiledAddressesBerlin, p.Address())
	vm.PrecompiledAddressesCancun = append(vm.PrecompiledAddressesCancun, p.Address())
}

var PrecompileLastUpgrade = map[string]int64{
	bank.BankAddress: 1,
}
