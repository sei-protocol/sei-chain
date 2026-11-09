// Package params is the EVM-only params precompile. Its ABI is Params.sol and
// its state is docs/giga-storage/ParamsStorage.sol, held as contract storage at
// Address. Its params are the ones declared in that schema and in this package,
// and change only through Apply, which gov calls for a passed proposal.
package params

import (
	"bytes"
	_ "embed"
	"errors"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

//go:embed abi.json
var abiJSON []byte

// ABI is the params precompile's ABI.
var ABI = func() abi.ABI {
	parsed, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return parsed
}()

const ParamMethod = "param"

// Flat per-call gas, as V2's pcommon.DefaultGasCost prices a query.
const (
	unknownMethodGas = 3_000
	queryGas         = 1_000
	queryByteGas     = 3
)

var (
	errDelegateCall  = errors.New("cannot delegatecall params")
	errNonPayable    = errors.New("sending funds to a non-payable function")
	errUnknownMethod = errors.New("unknown params method")
)

// retiredMethods are the V2 methods, by signature, params keeps in its ABI only
// to revert with a reason naming the call to use instead. They are checked before
// dispatch.
var retiredMethods = map[string]string{
	"params(string,string)": "params(string,string) is not supported; use param(string,string), which takes a flat key and returns the ABI-encoded value",
}

// Precompile is the params precompile.
type Precompile struct{}

var _ precompiles.Contract = Precompile{}

func (Precompile) RequiredGas(input []byte) uint64 {
	if _, err := methodOf(input); err != nil {
		return unknownMethodGas
	}
	return queryGas + queryByteGas*uint64(len(input)-4) //nolint:gosec // methodOf requires a 4-byte selector
}

func (p Precompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
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
	case ParamMethod:
		ss, p, err := lookupParam(args[0].(string), args[1].(string))
		if err != nil {
			return precompiles.Revert(err.Error())
		}
		return method.Outputs.Pack(common.BigToHash(ss.get(newStore(ctx.State), p)).Bytes())
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
