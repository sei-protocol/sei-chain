package gov

import (
	"bytes"
	"embed"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

const (
	VoteMethod    = "vote"
	DepositMethod = "deposit"
)

const (
	GovAddress = "0x0000000000000000000000000000000000001006"
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
	govKeeper pcommon.GovKeeper
	evmKeeper pcommon.EVMKeeper
	address   common.Address

	VoteID    []byte
	DepositID []byte
}

func NewPrecompile(govKeeper pcommon.GovKeeper, evmKeeper pcommon.EVMKeeper) (*Precompile, error) {
	newAbi := GetABI()

	p := &Precompile{
		Precompile: pcommon.Precompile{ABI: newAbi},
		govKeeper:  govKeeper,
		evmKeeper:  evmKeeper,
		address:    common.HexToAddress(GovAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case VoteMethod:
			p.VoteID = m.ID
		case DepositMethod:
			p.DepositID = m.ID
		}
	}

	return p, nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p Precompile) RequiredGas(input []byte) uint64 {
	methodID := input[:4]

	if bytes.Equal(methodID, p.VoteID) {
		return 30000
	} else if bytes.Equal(methodID, p.DepositID) {
		return 30000
	}
	panic("unknown method")
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) Run(evm *vm.EVM, caller common.Address, input []byte) (bz []byte, err error) {
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case VoteMethod:
		return p.vote(ctx, method, caller, args)
	case DepositMethod:
		return p.deposit(ctx, method, caller, args)
	}
	return
}

func (p Precompile) vote(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 2)
	voter := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
	proposalID := args[0].(uint64)
	voteOption := args[1].(int32)
	err := p.govKeeper.AddVote(ctx, proposalID, voter, govtypes.NewNonSplitVoteOption(govtypes.VoteOption(voteOption)))
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) deposit(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 2)
	depositor := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
	proposalID := args[0].(uint64)
	amount := args[1].(*big.Int)
	res, err := p.govKeeper.AddDeposit(ctx, proposalID, depositor, sdk.NewCoins(sdk.NewCoin(p.evmKeeper.GetBaseDenom(ctx), sdk.NewIntFromBigInt(amount))))
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(res)
}
