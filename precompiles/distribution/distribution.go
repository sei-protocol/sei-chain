package distribution

import (
	"bytes"
	"embed"
	"errors"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
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
	distrKeeper pcommon.DistributionKeeper
	evmKeeper   pcommon.EVMKeeper
	address     common.Address

	SetWithdrawAddrID           []byte
	WithdrawDelegationRewardsID []byte
}

func NewPrecompile(distrKeeper pcommon.DistributionKeeper, evmKeeper pcommon.EVMKeeper) (*Precompile, error) {
	newAbi := GetABI()

	p := &Precompile{
		Precompile:  pcommon.Precompile{ABI: newAbi},
		distrKeeper: distrKeeper,
		evmKeeper:   evmKeeper,
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
		return 0
	}

	if bytes.Equal(methodID, p.SetWithdrawAddrID) {
		return 30000
	} else if bytes.Equal(methodID, p.WithdrawDelegationRewardsID) {
		return 50000
	}
	panic("unknown method")
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) Run(evm *vm.EVM, caller common.Address, input []byte, value *big.Int) (bz []byte, err error) {
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
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
	pcommon.AssertNonPayable(value)
	pcommon.AssertArgsLength(args, 1)
	delegator := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
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
	pcommon.AssertNonPayable(value)
	pcommon.AssertArgsLength(args, 1)
	delegator := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
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
