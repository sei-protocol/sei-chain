package precompiles

import (
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ecommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/addr"
	"github.com/sei-protocol/sei-chain/precompiles/auth"
	"github.com/sei-protocol/sei-chain/precompiles/authz"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	"github.com/sei-protocol/sei-chain/precompiles/distribution"
	"github.com/sei-protocol/sei-chain/precompiles/evidence"
	"github.com/sei-protocol/sei-chain/precompiles/gov"
	"github.com/sei-protocol/sei-chain/precompiles/ibc"
	"github.com/sei-protocol/sei-chain/precompiles/json"
	"github.com/sei-protocol/sei-chain/precompiles/mint"
	"github.com/sei-protocol/sei-chain/precompiles/oracle"
	"github.com/sei-protocol/sei-chain/precompiles/p256"
	"github.com/sei-protocol/sei-chain/precompiles/params"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	"github.com/sei-protocol/sei-chain/precompiles/pointerview"
	"github.com/sei-protocol/sei-chain/precompiles/slashing"
	"github.com/sei-protocol/sei-chain/precompiles/solo"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	"github.com/sei-protocol/sei-chain/precompiles/upgrade"
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

// GetCustomPrecompiles returns every Sei custom precompile keyed by its address. It panics
// if any precompile fails to construct.
func GetCustomPrecompiles(keepers utils.Keepers) map[ecommon.Address]vm.PrecompiledContract {
	return map[ecommon.Address]vm.PrecompiledContract{
		ecommon.HexToAddress(bank.BankAddress):               check(bank.NewPrecompile(keepers)),
		ecommon.HexToAddress(wasmd.WasmdAddress):             check(wasmd.NewPrecompile(keepers)),
		ecommon.HexToAddress(json.JSONAddress):               check(json.NewPrecompile(keepers)),
		ecommon.HexToAddress(addr.AddrAddress):               check(addr.NewPrecompile(keepers)),
		ecommon.HexToAddress(staking.StakingAddress):         check(staking.NewPrecompile(keepers)),
		ecommon.HexToAddress(gov.GovAddress):                 check(gov.NewPrecompile(keepers)),
		ecommon.HexToAddress(distribution.DistrAddress):      check(distribution.NewPrecompile(keepers)),
		ecommon.HexToAddress(oracle.OracleAddress):           check(oracle.NewPrecompile(keepers)),
		ecommon.HexToAddress(ibc.IBCAddress):                 check(ibc.NewPrecompile(keepers)),
		ecommon.HexToAddress(pointer.PointerAddress):         check(pointer.NewPrecompile(keepers)),
		ecommon.HexToAddress(pointerview.PointerViewAddress): check(pointerview.NewPrecompile(keepers)),
		ecommon.HexToAddress(p256.P256VerifyAddress):         check(p256.NewPrecompile(keepers)),
		ecommon.HexToAddress(solo.SoloAddress):               check(solo.NewPrecompile(keepers)),
		ecommon.HexToAddress(auth.AuthAddress):               check(auth.NewPrecompile(keepers)),
		ecommon.HexToAddress(authz.AuthzAddress):             check(authz.NewPrecompile(keepers)),
		ecommon.HexToAddress(evidence.EvidenceAddress):       check(evidence.NewPrecompile(keepers)),
		ecommon.HexToAddress(mint.MintAddress):               check(mint.NewPrecompile(keepers)),
		ecommon.HexToAddress(params.ParamsAddress):           check(params.NewPrecompile(keepers)),
		ecommon.HexToAddress(slashing.SlashingAddress):       check(slashing.NewPrecompile(keepers)),
		ecommon.HexToAddress(upgrade.UpgradeAddress):         check(upgrade.NewPrecompile(keepers)),
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
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

	p256p, err := p256.NewPrecompile(keepers)
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
	PrecompileNamesToInfo[p256p.GetName()] = PrecompileInfo{ABI: p256p.GetABI(), Address: p256p.Address()}

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
		addPrecompileToVM(p256p)
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
