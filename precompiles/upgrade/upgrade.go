package upgrade

import (
	"embed"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
)

const (
	CurrentPlanMethod            = "currentPlan"
	AppliedPlanMethod            = "appliedPlan"
	UpgradedConsensusStateMethod = "upgradedConsensusState"
	ModuleVersionsMethod         = "moduleVersions"
)

const (
	UpgradeAddress = "0x0000000000000000000000000000000000001015"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper      utils.EVMKeeper
	upgradeQuerier utils.UpgradeQuerier

	CurrentPlanID            []byte
	AppliedPlanID            []byte
	UpgradedConsensusStateID []byte
	ModuleVersionsID         []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:      keepers.EVMK(),
		upgradeQuerier: keepers.UpgradeQ(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case CurrentPlanMethod:
			p.CurrentPlanID = m.ID
		case AppliedPlanMethod:
			p.AppliedPlanID = m.ID
		case UpgradedConsensusStateMethod:
			p.UpgradedConsensusStateID = m.ID
		case ModuleVersionsMethod:
			p.ModuleVersionsID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, common.HexToAddress(UpgradeAddress), "upgrade"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return pcommon.DefaultGasCost(input, p.IsTransaction(method.Name))
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, hooks *tracing.Hooks) (bz []byte, remainingGas uint64, err error) {
	// Needed to catch gas meter panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("execution reverted: %v", r)
		}
	}()
	// Queries must not mutate state even when the underlying querier has side
	// effects (e.g. auth's NextAccountNumber increments the global account
	// number counter, gov's Tally deletes votes), so run every view on a
	// branched context and discard the writes.
	ctx, _ = ctx.CacheContext()
	switch method.Name {
	case CurrentPlanMethod:
		return p.currentPlan(ctx, method, args, value)
	case AppliedPlanMethod:
		return p.appliedPlan(ctx, method, args, value)
	case UpgradedConsensusStateMethod:
		return p.upgradedConsensusState(ctx, method, args, value)
	case ModuleVersionsMethod:
		return p.moduleVersions(ctx, method, args, value)
	}
	return
}

type Plan struct {
	Name   string
	Height int64
	Info   string
}

type ModuleVersion struct {
	Name    string
	Version uint64
}

func (p PrecompileExecutor) currentPlan(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	request := &upgradetypes.QueryCurrentPlanRequest{}
	response, err := p.upgradeQuerier.CurrentPlan(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	// no plan scheduled returns a zero-valued plan
	plan := Plan{}
	if response.Plan != nil {
		plan = Plan{
			Name:   response.Plan.Name,
			Height: response.Plan.Height,
			Info:   response.Plan.Info,
		}
	}

	bz, err := method.Outputs.Pack(plan)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) appliedPlan(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	request := &upgradetypes.QueryAppliedPlanRequest{
		Name: args[0].(string),
	}
	response, err := p.upgradeQuerier.AppliedPlan(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(response.Height)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) upgradedConsensusState(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	// The request type is marked deprecated upstream, but it is still the
	// wire type of the Query/UpgradedConsensusState rpc this method mirrors.
	request := &upgradetypes.QueryUpgradedConsensusStateRequest{ //nolint:staticcheck
		LastHeight: args[0].(int64),
	}
	response, err := p.upgradeQuerier.UpgradedConsensusState(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(response.UpgradedConsensusState)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) moduleVersions(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	request := &upgradetypes.QueryModuleVersionsRequest{
		ModuleName: args[0].(string),
	}
	response, err := p.upgradeQuerier.ModuleVersions(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	versions := make([]ModuleVersion, 0, len(response.ModuleVersions))
	for _, mv := range response.ModuleVersions {
		if mv == nil {
			continue
		}
		versions = append(versions, ModuleVersion{
			Name:    mv.Name,
			Version: mv.Version,
		})
	}

	bz, err := method.Outputs.Pack(versions)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return p.evmKeeper
}

func (PrecompileExecutor) IsTransaction(string) bool {
	return false
}
