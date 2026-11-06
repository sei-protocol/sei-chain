// Package slashing is the EVM-only slashing precompile. Its ABI is Slashing.sol.
//
// It serves unjail, which returns the caller's jailed validator to staking's
// power index. It keeps no signing info, so it neither jails nor slashes.
package slashing

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
)

//go:embed abi.json
var abiJSON []byte

// ABI is the slashing precompile's ABI.
var ABI = func() abi.ABI {
	parsed, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return parsed
}()

// Address is the slashing precompile's address.
var Address = common.HexToAddress("0x0000000000000000000000000000000000001014")

const UnjailMethod = "unjail"

// UnjailEvent is the event an unjail logs.
const UnjailEvent = "Unjail"

const authzUnsupported = "unjail authorizations are not supported"

// retiredMethods are the V2 methods, by signature, slashing keeps in its ABI
// only to revert with a reason. They are checked before dispatch.
var retiredMethods = map[string]string{
	"grantUnjailAuthorization(address,int64)": authzUnsupported,
	"unjailWithAuthorization(address)":        authzUnsupported,
	"revokeUnjailAuthorization(address)":      authzUnsupported,
}

// Flat per-call gas, as V2's pcommon.DefaultGasCost prices it from the
// Cosmos KV gas config, charged on the arguments after the method selector.
const (
	unknownMethodGas   = 3_000
	transactionGas     = 2_000
	transactionByteGas = 30
)

var (
	errDelegateCall  = errors.New("cannot delegatecall slashing")
	errStaticCall    = fmt.Errorf("cannot call slashing precompile from staticcall: %w", vm.ErrWriteProtection)
	errNonPayable    = errors.New("sending funds to a non-payable function")
	errUnknownMethod = errors.New("unknown slashing method")
)

// Precompile is the slashing precompile.
type Precompile struct{}

var _ precompiles.Contract = Precompile{}

func (Precompile) RequiredGas(input []byte) uint64 {
	if _, err := methodOf(input); err != nil {
		return unknownMethodGas
	}
	return transactionGas + transactionByteGas*uint64(len(input)-4) //nolint:gosec // methodOf requires a 4-byte selector
}

func (Precompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	// Under delegatecall the caller is the calling contract's caller, so a
	// contract could unjail that caller's validator without it calling slashing.
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
	if ctx.ReadOnly {
		return nil, errStaticCall
	}
	if ctx.ApparentValue != nil && ctx.ApparentValue.Sign() != 0 {
		return nil, errNonPayable
	}

	switch method.Name {
	case UnjailMethod:
		return unjailCall(ctx, method)
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

func unjailCall(ctx *precompiles.Context, method *abi.Method) ([]byte, error) {
	if err := staking.Unjail(ctx.State, ctx.Caller); err != nil {
		return nil, err
	}
	ctx.Logs.AddLog(&ethtypes.Log{
		Address: ctx.Address,
		Topics:  []common.Hash{ABI.Events[UnjailEvent].ID, common.BytesToHash(ctx.Caller[:])},
	})
	return method.Outputs.Pack(true)
}
