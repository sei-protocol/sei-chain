// Package params is the EVM-only params precompile. Its state is
// docs/giga-storage/ParamsStorage.sol, held as contract storage at Address,
// and its params are the ones declared in that schema and in this package.
//
// Its ABI is Params.sol.
package params

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"

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

const (
	ParamMethod     = "param"
	ParamKeysMethod = "paramKeys"
)

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

// ParamKey is a declared param: its subspace, flat key, and the Solidity type
// of its value.
type ParamKey struct {
	Subspace  string
	Key       string
	ValueType string
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
	args, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return nil, err
	}
	if ctx.ApparentValue != nil && ctx.ApparentValue.Sign() != 0 {
		return nil, errNonPayable
	}
	switch method.Name {
	case ParamMethod:
		return paramQuery(ctx.State, method, args[0].(string), args[1].(string))
	case ParamKeysMethod:
		return method.Outputs.Pack(paramKeys())
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

// paramQuery returns a param's value as abi.encode of its Solidity type.
func paramQuery(state precompiles.State, method *abi.Method, subspaceName, key string) ([]byte, error) {
	ss, ok := lookupSubspace(subspaceName)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errUnknownSubspace, subspaceName)
	}
	p, ok := ss.param(key)
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s", errUnknownKey, subspaceName, key)
	}
	return method.Outputs.Pack(common.BigToHash(ss.get(newStore(state), p)).Bytes())
}

func paramKeys() []ParamKey {
	var out []ParamKey
	for _, ss := range subspaces {
		for _, p := range ss.params {
			out = append(out, ParamKey{Subspace: ss.name, Key: p.key, ValueType: p.kind.solType})
		}
	}
	return out
}
