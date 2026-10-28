// Package params is the EVM-only params precompile. Its state is
// docs/giga-storage/ParamsStorage.sol, held as contract storage at Address,
// and its params are the ones declared in that schema and in this package.
//
// Its ABI is precompiles/params' with param and paramKeys added.
package params

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
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
	ParamsMethod    = "params"
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
	case ParamsMethod:
		return paramsQuery(ctx.State, method, args[0].(string), args[1].(string))
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

// paramsQuery returns V2 key's value as amino JSON, or "" for an undeclared key.
func paramsQuery(state precompiles.State, method *abi.Method, subspaceName, key string) ([]byte, error) {
	ss, ok := lookupSubspace(subspaceName)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errUnknownSubspace, subspaceName)
	}
	value, err := aminoJSON(newStore(state), ss, key)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(value)
}

func aminoJSON(s solstorage.Store, ss *subspace, key string) (string, error) {
	ps, isStruct := ss.v2Params(key)
	if len(ps) == 0 {
		return "", nil
	}
	if !isStruct {
		bz, err := json.Marshal(ps[0].kind.toJSON(ss.get(s, ps[0])))
		return string(bz), err
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for _, p := range ps {
		v := ss.get(s, p)
		if v.Sign() == 0 && p.kind.omitZero {
			continue
		}
		_, field := p.v2Key()
		name, err := json.Marshal(field)
		if err != nil {
			return "", err
		}
		value, err := json.Marshal(p.kind.toJSON(v))
		if err != nil {
			return "", err
		}
		if buf.Len() > 1 {
			buf.WriteByte(',')
		}
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.String(), nil
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
