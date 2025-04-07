package precompiles

import (
	"sync"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	ibctransferkeeper "github.com/cosmos/ibc-go/v3/modules/apps/transfer/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ecommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/addr"
	addrv520 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v520"
	addrv555 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v555"
	addrv562 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v562"
	addrv575 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v575"
	addrv600 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v600"
	addrv602 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v602"
	addrv603 "github.com/sei-protocol/sei-chain/precompiles/addr/legacy/v603"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	bankv520 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v520"
	bankv552 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v552"
	bankv555 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v555"
	bankv562 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v562"
	bankv580 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v580"
	bankv600 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v600"
	bankv602 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v602"
	bankv603 "github.com/sei-protocol/sei-chain/precompiles/bank/legacy/v603"
	"github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/confidentialtransfers"
	"github.com/sei-protocol/sei-chain/precompiles/distribution"
	distrv520 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v520"
	distrv552 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v552"
	distrv555 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v555"
	distrv562 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v562"
	distrv580 "github.com/sei-protocol/sei-chain/precompiles/distribution/legacy/v580"
	"github.com/sei-protocol/sei-chain/precompiles/gov"
	govv520 "github.com/sei-protocol/sei-chain/precompiles/gov/legacy/v520"
	govv555 "github.com/sei-protocol/sei-chain/precompiles/gov/legacy/v555"
	govv562 "github.com/sei-protocol/sei-chain/precompiles/gov/legacy/v562"
	govv580 "github.com/sei-protocol/sei-chain/precompiles/gov/legacy/v580"
	"github.com/sei-protocol/sei-chain/precompiles/ibc"
	ibcv501 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v501"
	ibcv510 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v510"
	ibcv520 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v520"
	ibcv530 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v530"
	ibcv555 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v555"
	ibcv562 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v562"
	ibcv580 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v580"
	ibcv602 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v602"
	ibcv603 "github.com/sei-protocol/sei-chain/precompiles/ibc/legacy/v603"
	"github.com/sei-protocol/sei-chain/precompiles/json"
	jsonv520 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v520"
	jsonv530 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v530"
	jsonv555 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v555"
	jsonv562 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v562"
	jsonv603 "github.com/sei-protocol/sei-chain/precompiles/json/legacy/v603"
	"github.com/sei-protocol/sei-chain/precompiles/oracle"
	oraclev520 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v520"
	oraclev555 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v555"
	oraclev562 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v562"
	oraclev600 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v600"
	oraclev602 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v602"
	oraclev603 "github.com/sei-protocol/sei-chain/precompiles/oracle/legacy/v603"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	pointerv520 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v520"
	pointerv522 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v522"
	pointerv530 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v530"
	pointerv555 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v555"
	pointerv562 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v562"
	pointerv575 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v575"
	pointerv580 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v580"
	pointerv600 "github.com/sei-protocol/sei-chain/precompiles/pointer/legacy/v600"
	"github.com/sei-protocol/sei-chain/precompiles/pointerview"
	pointerviewv520 "github.com/sei-protocol/sei-chain/precompiles/pointerview/legacy/v520"
	pointerviewv555 "github.com/sei-protocol/sei-chain/precompiles/pointerview/legacy/v555"
	pointerviewv562 "github.com/sei-protocol/sei-chain/precompiles/pointerview/legacy/v562"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	stakingv520 "github.com/sei-protocol/sei-chain/precompiles/staking/legacy/v520"
	stakingv555 "github.com/sei-protocol/sei-chain/precompiles/staking/legacy/v555"
	stakingv562 "github.com/sei-protocol/sei-chain/precompiles/staking/legacy/v562"
	stakingv580 "github.com/sei-protocol/sei-chain/precompiles/staking/legacy/v580"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	wasmdv501 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v501"
	wasmdv510 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v510"
	wasmdv520 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v520"
	wasmdv522 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v522"
	wasmdv530 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v530"
	wasmdv555 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v555"
	wasmdv562 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v562"
	wasmdv575 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v575"
	wasmdv580 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v580"
	wasmdv600 "github.com/sei-protocol/sei-chain/precompiles/wasmd/legacy/v600"
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

type VersionedPrecompiles map[string]vm.PrecompiledContract

func GetCustomPrecompiles(
	latestUpgrade string,
	evmKeeper common.EVMKeeper,
	bankKeeper common.BankKeeper,
	bankSender common.BankMsgServer,
	wasmdKeeper *wasmkeeper.PermissionedKeeper,
	wasmdViewKeeper wasmkeeper.Keeper,
	stakingKeeper common.StakingKeeper,
	stakingQuerier common.StakingQuerier,
	govKeeper common.GovKeeper,
	distrKeeper common.DistributionKeeper,
	oracleKeeper common.OracleKeeper,
	transferKeeper ibctransferkeeper.Keeper,
	clientKeeper common.ClientKeeper,
	connectionKeeper common.ConnectionKeeper,
	channelKeeper common.ChannelKeeper,
	accountKeeper common.AccountKeeper,
	ctViewKeeper common.ConfidentialTransfersViewKeeper,
	ctKeeper common.ConfidentialTransfersKeeper,

) map[ecommon.Address]VersionedPrecompiles {
	bankVersions := VersionedPrecompiles{
		latestUpgrade: check(bank.NewPrecompile(bankKeeper, bankSender, evmKeeper, accountKeeper)),
		"v5.2.0":      check(bankv520.NewPrecompile(bankKeeper, evmKeeper)),
		"v5.5.2":      check(bankv552.NewPrecompile(bankKeeper, evmKeeper)),
		"v5.5.5":      check(bankv555.NewPrecompile(bankKeeper, evmKeeper)),
		"v5.6.2":      check(bankv562.NewPrecompile(bankKeeper, evmKeeper, accountKeeper)),
		"v5.8.0":      check(bankv580.NewPrecompile(bankKeeper, evmKeeper, accountKeeper)),
		"v6.0.0":      check(bankv600.NewPrecompile(bankKeeper, evmKeeper, accountKeeper)),
		"v6.0.2":      check(bankv602.NewPrecompile(bankKeeper, bankSender, evmKeeper, accountKeeper)),
		"v6.0.3":      check(bankv603.NewPrecompile(bankKeeper, bankSender, evmKeeper, accountKeeper)),
	}
	wasmdVersions := VersionedPrecompiles{
		latestUpgrade: check(wasmd.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)),
		"v5.0.1":      check(wasmdv501.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)),
		"v5.1.0":      check(wasmdv510.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)),
		"v5.2.0":      check(wasmdv520.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)),
		"v5.2.2":      check(wasmdv522.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)),
		"v5.3.0":      check(wasmdv530.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)),
		"v5.5.5":      check(wasmdv555.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)),
		"v5.6.2":      check(wasmdv562.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)),
		"v5.7.5":      check(wasmdv575.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)),
		"v5.8.0":      check(wasmdv580.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)),
		"v6.0.0":      check(wasmdv600.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)),
	}
	jsonVersions := VersionedPrecompiles{
		latestUpgrade: check(json.NewPrecompile()),
		"v5.2.0":      check(jsonv520.NewPrecompile()),
		"v5.3.0":      check(jsonv530.NewPrecompile()),
		"v5.5.5":      check(jsonv555.NewPrecompile()),
		"v5.6.2":      check(jsonv562.NewPrecompile()),
		"v6.0.3":      check(jsonv603.NewPrecompile()),
	}
	addrVersions := VersionedPrecompiles{
		latestUpgrade: check(addr.NewPrecompile(evmKeeper, bankKeeper, accountKeeper)),
		"v5.2.0":      check(addrv520.NewPrecompile(evmKeeper)),
		"v5.5.5":      check(addrv555.NewPrecompile(evmKeeper)),
		"v5.6.2":      check(addrv562.NewPrecompile(evmKeeper)),
		"v5.7.5":      check(addrv575.NewPrecompile(evmKeeper, bankKeeper, accountKeeper)),
		"v6.0.0":      check(addrv600.NewPrecompile(evmKeeper, bankKeeper, accountKeeper)),
		"v6.0.2":      check(addrv602.NewPrecompile(evmKeeper, bankKeeper, accountKeeper)),
		"v6.0.3":      check(addrv603.NewPrecompile(evmKeeper, bankKeeper, accountKeeper)),
	}
	stakingVersions := VersionedPrecompiles{
		latestUpgrade: check(staking.NewPrecompile(stakingKeeper, stakingQuerier, evmKeeper, bankKeeper)),
		"v5.2.0":      check(stakingv520.NewPrecompile(stakingKeeper, evmKeeper, bankKeeper)),
		"v5.5.5":      check(stakingv555.NewPrecompile(stakingKeeper, evmKeeper, bankKeeper)),
		"v5.6.2":      check(stakingv562.NewPrecompile(stakingKeeper, evmKeeper, bankKeeper)),
		"v5.8.0":      check(stakingv580.NewPrecompile(stakingKeeper, stakingQuerier, evmKeeper, bankKeeper)),
	}
	govVersions := VersionedPrecompiles{
		latestUpgrade: check(gov.NewPrecompile(govKeeper, evmKeeper, bankKeeper)),
		"v5.2.0":      check(govv520.NewPrecompile(govKeeper, evmKeeper, bankKeeper)),
		"v5.5.5":      check(govv555.NewPrecompile(govKeeper, evmKeeper, bankKeeper)),
		"v5.6.2":      check(govv562.NewPrecompile(govKeeper, evmKeeper, bankKeeper)),
		"v5.8.0":      check(govv580.NewPrecompile(govKeeper, evmKeeper, bankKeeper)),
	}
	distrVersions := VersionedPrecompiles{
		latestUpgrade: check(distribution.NewPrecompile(distrKeeper, evmKeeper)),
		"v5.2.0":      check(distrv520.NewPrecompile(distrKeeper, evmKeeper)),
		"v5.5.2":      check(distrv552.NewPrecompile(distrKeeper, evmKeeper)),
		"v5.5.5":      check(distrv555.NewPrecompile(distrKeeper, evmKeeper)),
		"v5.6.2":      check(distrv562.NewPrecompile(distrKeeper, evmKeeper)),
		"v5.8.0":      check(distrv580.NewPrecompile(distrKeeper, evmKeeper)),
	}
	oracleVersions := VersionedPrecompiles{
		latestUpgrade: check(oracle.NewPrecompile(oracleKeeper, evmKeeper)),
		"v5.2.0":      check(oraclev520.NewPrecompile(oracleKeeper, evmKeeper)),
		"v5.5.5":      check(oraclev555.NewPrecompile(oracleKeeper, evmKeeper)),
		"v5.6.2":      check(oraclev562.NewPrecompile(oracleKeeper, evmKeeper)),
		"v6.0.0":      check(oraclev600.NewPrecompile(oracleKeeper, evmKeeper)),
		"v6.0.2":      check(oraclev602.NewPrecompile(oracleKeeper, evmKeeper)),
		"v6.0.3":      check(oraclev603.NewPrecompile(oracleKeeper, evmKeeper)),
	}
	ibcVersions := VersionedPrecompiles{
		latestUpgrade: check(ibc.NewPrecompile(transferKeeper, evmKeeper, clientKeeper, connectionKeeper, channelKeeper)),
		"v5.0.1":      check(ibcv501.NewPrecompile(transferKeeper, evmKeeper)),
		"v5.1.0":      check(ibcv510.NewPrecompile(transferKeeper, evmKeeper)),
		"v5.2.0":      check(ibcv520.NewPrecompile(transferKeeper, evmKeeper)),
		"v5.3.0":      check(ibcv530.NewPrecompile(transferKeeper, evmKeeper, clientKeeper, connectionKeeper, channelKeeper)),
		"v5.5.5":      check(ibcv555.NewPrecompile(transferKeeper, evmKeeper, clientKeeper, connectionKeeper, channelKeeper)),
		"v5.6.2":      check(ibcv562.NewPrecompile(transferKeeper, evmKeeper, clientKeeper, connectionKeeper, channelKeeper)),
		"v5.8.0":      check(ibcv580.NewPrecompile(transferKeeper, evmKeeper, clientKeeper, connectionKeeper, channelKeeper)),
		"v6.0.2":      check(ibcv602.NewPrecompile(transferKeeper, evmKeeper, clientKeeper, connectionKeeper, channelKeeper)),
		"v6.0.3":      check(ibcv603.NewPrecompile(transferKeeper, evmKeeper, clientKeeper, connectionKeeper, channelKeeper)),
	}
	pointerVersions := VersionedPrecompiles{
		latestUpgrade: check(pointer.NewPrecompile(evmKeeper, bankKeeper, wasmdViewKeeper)),
		"v5.2.0":      check(pointerv520.NewPrecompile(evmKeeper, bankKeeper, wasmdViewKeeper)),
		"v5.2.2":      check(pointerv522.NewPrecompile(evmKeeper, bankKeeper, wasmdViewKeeper)),
		"v5.3.0":      check(pointerv530.NewPrecompile(evmKeeper, bankKeeper, wasmdViewKeeper)),
		"v5.5.5":      check(pointerv555.NewPrecompile(evmKeeper, bankKeeper, wasmdViewKeeper)),
		"v5.6.2":      check(pointerv562.NewPrecompile(evmKeeper, bankKeeper, wasmdViewKeeper)),
		"v5.7.5":      check(pointerv575.NewPrecompile(evmKeeper, bankKeeper, wasmdViewKeeper)),
		"v5.8.0":      check(pointerv580.NewPrecompile(evmKeeper, bankKeeper, wasmdViewKeeper)),
		"v6.0.0":      check(pointerv600.NewPrecompile(evmKeeper, bankKeeper, wasmdViewKeeper)),
	}
	pointerviewVersions := VersionedPrecompiles{
		latestUpgrade: check(pointerview.NewPrecompile(evmKeeper)),
		"v5.2.0":      check(pointerviewv520.NewPrecompile(evmKeeper)),
		"v5.5.5":      check(pointerviewv555.NewPrecompile(evmKeeper)),
		"v5.6.2":      check(pointerviewv562.NewPrecompile(evmKeeper)),
	}
	ctprVersions := VersionedPrecompiles{
		latestUpgrade: check(confidentialtransfers.NewPrecompile(ctViewKeeper, ctKeeper, evmKeeper)),
	}

	return map[ecommon.Address]VersionedPrecompiles{
		ecommon.HexToAddress(bank.BankAddress):                bankVersions,
		ecommon.HexToAddress(wasmd.WasmdAddress):              wasmdVersions,
		ecommon.HexToAddress(json.JSONAddress):                jsonVersions,
		ecommon.HexToAddress(addr.AddrAddress):                addrVersions,
		ecommon.HexToAddress(staking.StakingAddress):          stakingVersions,
		ecommon.HexToAddress(gov.GovAddress):                  govVersions,
		ecommon.HexToAddress(distribution.DistrAddress):       distrVersions,
		ecommon.HexToAddress(oracle.OracleAddress):            oracleVersions,
		ecommon.HexToAddress(ibc.IBCAddress):                  ibcVersions,
		ecommon.HexToAddress(pointer.PointerAddress):          pointerVersions,
		ecommon.HexToAddress(pointerview.PointerViewAddress):  pointerviewVersions,
		ecommon.HexToAddress(confidentialtransfers.CtAddress): ctprVersions,
	}
}

func InitializePrecompiles(
	dryRun bool,
	evmKeeper common.EVMKeeper,
	bankKeeper common.BankKeeper,
	bankSender common.BankMsgServer,
	wasmdKeeper common.WasmdKeeper,
	wasmdViewKeeper common.WasmdViewKeeper,
	stakingKeeper common.StakingKeeper,
	stakingQuerier common.StakingQuerier,
	govKeeper common.GovKeeper,
	distrKeeper common.DistributionKeeper,
	oracleKeeper common.OracleKeeper,
	transferKeeper common.TransferKeeper,
	clientKeeper common.ClientKeeper,
	connectionKeeper common.ConnectionKeeper,
	channelKeeper common.ChannelKeeper,
	accountKeeper common.AccountKeeper,
	ctViewKeeper common.ConfidentialTransfersViewKeeper,
	ctKeeper common.ConfidentialTransfersKeeper,
) error {
	SetupMtx.Lock()
	defer SetupMtx.Unlock()
	if Initialized {
		panic("precompiles already initialized")
	}
	bankp, err := bank.NewPrecompile(bankKeeper, bankSender, evmKeeper, accountKeeper)
	if err != nil {
		return err
	}
	wasmdp, err := wasmd.NewPrecompile(evmKeeper, wasmdKeeper, wasmdViewKeeper, bankKeeper)
	if err != nil {
		return err
	}
	jsonp, err := json.NewPrecompile()
	if err != nil {
		return err
	}
	addrp, err := addr.NewPrecompile(evmKeeper, bankKeeper, accountKeeper)
	if err != nil {
		return err
	}
	stakingp, err := staking.NewPrecompile(stakingKeeper, stakingQuerier, evmKeeper, bankKeeper)
	if err != nil {
		return err
	}
	govp, err := gov.NewPrecompile(govKeeper, evmKeeper, bankKeeper)
	if err != nil {
		return err
	}
	distrp, err := distribution.NewPrecompile(distrKeeper, evmKeeper)
	if err != nil {
		return err
	}
	oraclep, err := oracle.NewPrecompile(oracleKeeper, evmKeeper)
	if err != nil {
		return err
	}
	ibcp, err := ibc.NewPrecompile(transferKeeper, evmKeeper, clientKeeper, connectionKeeper, channelKeeper)
	if err != nil {
		return err
	}
	pointerp, err := pointer.NewPrecompile(evmKeeper, bankKeeper, wasmdViewKeeper)
	if err != nil {
		return err
	}
	pointerviewp, err := pointerview.NewPrecompile(evmKeeper)
	if err != nil {
		return err
	}
	ctpr, err := confidentialtransfers.NewPrecompile(ctViewKeeper, ctKeeper, evmKeeper)
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
	PrecompileNamesToInfo[ctpr.GetName()] = PrecompileInfo{ABI: ctpr.GetABI(), Address: ctpr.Address()}
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
		addPrecompileToVM(ctpr)
		Initialized = true
	}
	return nil
}

func GetPrecompileInfo(name string) PrecompileInfo {
	if !Initialized {
		// Precompile Info does not require any keeper state
		_ = InitializePrecompiles(true, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}
	i, ok := PrecompileNamesToInfo[name]
	if !ok {
		panic(name + "doesn't exist as a precompile")
	}
	return i
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
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
