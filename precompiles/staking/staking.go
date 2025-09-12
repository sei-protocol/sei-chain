package staking

import (
	"bytes"
	"embed"
	"encoding/hex"
	"errors"
	"math/big"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	DelegateMethod        = "delegate"
	RedelegateMethod      = "redelegate"
	UndelegateMethod      = "undelegate"
	DelegationMethod      = "delegation"
	CreateValidatorMethod = "createValidator"
	EditValidatorMethod   = "editValidator"
)

const (
	StakingAddress = "0x0000000000000000000000000000000000001005"
)

//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	stakingKeeper  utils.StakingKeeper
	stakingQuerier utils.StakingQuerier
	evmKeeper      utils.EVMKeeper
	bankKeeper     utils.BankKeeper
	address        common.Address

	DelegateID        []byte
	RedelegateID      []byte
	UndelegateID      []byte
	DelegationID      []byte
	CreateValidatorID []byte
	EditValidatorID   []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.Precompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		stakingKeeper:  keepers.StakingK(),
		stakingQuerier: keepers.StakingQ(),
		evmKeeper:      keepers.EVMK(),
		bankKeeper:     keepers.BankK(),
		address:        common.HexToAddress(StakingAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case DelegateMethod:
			p.DelegateID = m.ID
		case RedelegateMethod:
			p.RedelegateID = m.ID
		case UndelegateMethod:
			p.UndelegateID = m.ID
		case DelegationMethod:
			p.DelegationID = m.ID
		case CreateValidatorMethod:
			p.CreateValidatorID = m.ID
		case EditValidatorMethod:
			p.EditValidatorID = m.ID
		}
	}

	return pcommon.NewPrecompile(newAbi, p, p.address, "staking"), nil
}

func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	switch {
	case bytes.Equal(method.ID, p.DelegateID):
		return 50000
	case bytes.Equal(method.ID, p.RedelegateID):
		return 70000
	case bytes.Equal(method.ID, p.UndelegateID):
		return 50000
	case bytes.Equal(method.ID, p.CreateValidatorID):
		return 100000
	case bytes.Equal(method.ID, p.EditValidatorID):
		return 100000
	case bytes.Equal(method.ID, p.DelegationID):
		return pcommon.DefaultGasCost(input, false)
	default:
		return pcommon.UnknownMethodCallGas
	}
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) ([]byte, error) {
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, errors.New("cannot delegatecall staking")
	}
	if readOnly && method.Name != DelegationMethod {
		return nil, errors.New("cannot call staking precompile from staticcall")
	}
	switch method.Name {
	case DelegateMethod:
		return p.delegate(ctx, method, caller, args, value, hooks, evm)
	case RedelegateMethod:
		return p.redelegate(ctx, method, caller, args, value, evm)
	case UndelegateMethod:
		return p.undelegate(ctx, method, caller, args, value, evm)
	case CreateValidatorMethod:
		return p.createValidator(ctx, method, caller, args, value, hooks, evm)
	case EditValidatorMethod:
		return p.editValidator(ctx, method, caller, args, value, hooks, evm)
	case DelegationMethod:
		return p.delegation(ctx, method, args, value)
	}
	return nil, errors.New("unknown method")
}
