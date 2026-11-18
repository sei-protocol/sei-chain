package precompiles

import (
	"slices"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	errorSelector = crypto.Keccak256([]byte("Error(string)"))[:4]
	errorArgs     = abi.Arguments{{Type: mustType("string")}}
)

// Revert returns the output and error that revert a call with Error(reason),
// as Solidity's revert(reason) does, refunding the call's unused gas.
func Revert(reason string) ([]byte, error) {
	data, err := errorArgs.Pack(reason)
	if err != nil {
		panic(err) // packing a string cannot fail
	}
	return slices.Concat(errorSelector, data), vm.ErrExecutionReverted
}

func mustType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return typ
}
