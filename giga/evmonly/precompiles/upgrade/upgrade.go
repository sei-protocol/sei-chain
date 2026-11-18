// Package upgrade is the EVM-only upgrade precompile. Its ABI is Upgrade.sol
// and its state is docs/giga-storage/UpgradeStorage.sol, held as contract
// storage at Address. Gov schedules and cancels plans through Schedule and
// Cancel; BeginBlock acts on a plan when it falls due.
package upgrade

import (
	"bytes"
	_ "embed"
	"errors"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

//go:embed abi.json
var abiJSON []byte

// ABI is the upgrade precompile's ABI.
var ABI = func() abi.ABI {
	parsed, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return parsed
}()

const (
	CurrentPlanMethod = "currentPlan"
	AppliedPlanMethod = "appliedPlan"
)

// Flat per-call gas, as V2's pcommon.DefaultGasCost prices a query.
const (
	unknownMethodGas = 3_000
	queryGas         = 1_000
	queryByteGas     = 3
)

var (
	errDelegateCall  = errors.New("cannot delegatecall upgrade")
	errNonPayable    = errors.New("sending funds to a non-payable function")
	errUnknownMethod = errors.New("unknown upgrade method")
)

// retiredMethods are the V2 methods, by signature, upgrade keeps in its ABI
// only to revert with a reason. They are checked before dispatch.
var retiredMethods = map[string]string{
	"moduleVersions(string)": "moduleVersions(string) is not supported; this chain has no Cosmos modules",
}

// Precompile is the upgrade precompile. Handlers holds an entry for every
// upgrade name this binary can apply, nil for one that changes no state.
// SkipHeights are the blocks at which this node clears a due plan instead of
// applying it or halting.
type Precompile struct {
	Handlers    map[string]Handler
	SkipHeights []uint64
}

var (
	_ precompiles.Contract     = Precompile{}
	_ precompiles.BeginBlocker = Precompile{}
)

func (Precompile) RequiredGas(input []byte) uint64 {
	if _, err := methodOf(input); err != nil {
		return unknownMethodGas
	}
	return queryGas + queryByteGas*uint64(len(input)-4) //nolint:gosec // methodOf requires a 4-byte selector
}

func (Precompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	if ctx.DelegateCall {
		return nil, errDelegateCall
	}
	method, err := methodOf(input)
	if err != nil {
		return nil, err
	}
	if reason, ok := retiredMethods[method.Sig]; ok {
		return precompiles.Revert(reason)
	}
	args, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return nil, err
	}
	if !method.IsPayable() && ctx.ApparentValue != nil && ctx.ApparentValue.Sign() != 0 {
		return nil, errNonPayable
	}
	switch method.Name {
	case CurrentPlanMethod:
		plan, _, err := CurrentPlan(ctx.State)
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(plan)
	case AppliedPlanMethod:
		name := args[0].(string)
		if len(name) > MaxNameLength {
			return precompiles.Revert(errNameTooLong.Error())
		}
		return method.Outputs.Pack(Applied(ctx.State, name))
	}
	return nil, errUnknownMethod
}

func methodOf(input []byte) (*abi.Method, error) {
	if len(input) < 4 {
		return nil, errUnknownMethod
	}
	method, err := ABI.MethodById(input[:4])
	if err != nil {
		return nil, errUnknownMethod
	}
	return method, nil
}
