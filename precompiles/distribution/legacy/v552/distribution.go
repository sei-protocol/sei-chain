package v552

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common/legacy/v555"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

const (
	SetWithdrawAddressMethod        = "setWithdrawAddress"
	WithdrawDelegationRewardsMethod = "withdrawDelegationRewards"
)

const (
	DistrAddress = "0x0000000000000000000000000000000000001007"
)

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

func GetABI() abi.ABI {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		panic(err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		panic(err)
	}
	return newAbi
}

type Precompile struct {
	pcommon.Precompile
	distrKeeper utils.DistributionKeeper
	evmKeeper   utils.EVMKeeper
	address     common.Address

	SetWithdrawAddrID           []byte
	WithdrawDelegationRewardsID []byte
}

func NewPrecompile(keepers utils.Keepers) (*Precompile, error) {
	newAbi := GetABI()

	p := &Precompile{
		Precompile:  pcommon.Precompile{ABI: newAbi},
		distrKeeper: keepers.DistributionK(),
		evmKeeper:   keepers.EVMK(),
		address:     common.HexToAddress(DistrAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case SetWithdrawAddressMethod:
			p.SetWithdrawAddrID = m.ID
		case WithdrawDelegationRewardsMethod:
			p.WithdrawDelegationRewardsID = m.ID
		}
	}

	return p, nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p Precompile) RequiredGas(input []byte) uint64 {
	methodID, err := pcommon.ExtractMethodID(input)
	if err != nil {
		return pcommon.UnknownMethodCallGas
	}

	if bytes.Equal(methodID, p.SetWithdrawAddrID) {
		return 30000
	} else if bytes.Equal(methodID, p.WithdrawDelegationRewardsID) {
		return 50000
	}

	// This should never happen since this is going to fail during Run
	return pcommon.UnknownMethodCallGas
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) GetName() string {
	return "distribution"
}

func (p Precompile) Run(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, value *big.Int, readOnly bool, _ bool, hooks *tracing.Hooks) (bz []byte, err error) {
	defer func() {
		if err != nil {
			state.GetDBImpl(evm.StateDB).SetPrecompileError(err)
		}
	}()
	if readOnly {
		return nil, errors.New("cannot call distr precompile from staticcall")
	}
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}
	if caller.Cmp(callingContract) != 0 {
		return nil, errors.New("cannot delegatecall distr")
	}

	switch method.Name {
	case SetWithdrawAddressMethod:
		return p.setWithdrawAddress(ctx, method, caller, args, value)
	case WithdrawDelegationRewardsMethod:
		return p.withdrawDelegationRewards(ctx, method, caller, args, value)
	}
	return
}

func (p Precompile) setWithdrawAddress(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	delegator, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, fmt.Errorf("delegator %s is not associated", caller.Hex())
	}
	withdrawAddr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, err
	}
	err = p.distrKeeper.SetWithdrawAddr(ctx, delegator, withdrawAddr)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) withdrawDelegationRewards(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	delegator, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, fmt.Errorf("delegator %s is not associated", caller.Hex())
	}
	validator, err := sdk.ValAddressFromBech32(args[0].(string))
	if err != nil {
		return nil, err
	}
	_, err = p.distrKeeper.WithdrawDelegationRewards(ctx, delegator, validator)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, errors.New("invalid addr")
	}
	seiAddr, associated := p.evmKeeper.GetSeiAddress(ctx, addr)
	if !associated {
		return nil, errors.New("cannot use an unassociated address as withdraw address")
	}
	return seiAddr, nil
}
