package bank

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/tendermint/tendermint/libs/log"
)

const (
	SendMethod     = "send"
	BalanceMethod  = "balance"
	NameMethod     = "name"
	SymbolMethod   = "symbol"
	DecimalsMethod = "decimals"
	SupplyMethod   = "supply"
)

const (
	BankAddress = "0x0000000000000000000000000000000000001001"
)

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type Precompile struct {
	pcommon.Precompile
	bankKeeper pcommon.BankKeeper
	evmKeeper  pcommon.EVMKeeper
	address    common.Address

	SendID     []byte
	BalanceID  []byte
	NameID     []byte
	SymbolID   []byte
	DecimalsID []byte
	SupplyID   []byte
}

func NewPrecompile(bankKeeper pcommon.BankKeeper, evmKeeper pcommon.EVMKeeper) (*Precompile, error) {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		return nil, fmt.Errorf("error loading the staking ABI %s", err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		return nil, err
	}

	p := &Precompile{
		Precompile: pcommon.Precompile{ABI: newAbi},
		bankKeeper: bankKeeper,
		evmKeeper:  evmKeeper,
		address:    common.HexToAddress(BankAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case SendMethod:
			p.SendID = m.ID
		case "balance":
			p.BalanceID = m.ID
		case "name":
			p.NameID = m.ID
		case "symbol":
			p.SymbolID = m.ID
		case "decimals":
			p.DecimalsID = m.ID
		case "supply":
			p.SupplyID = m.ID
		}
	}

	return p, nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p Precompile) RequiredGas(input []byte) uint64 {
	methodID := input[:4]

	method, err := p.ABI.MethodById(methodID)
	if err != nil {
		// This should never happen since this method is going to fail during Run
		return 0
	}

	return p.Precompile.RequiredGas(input, p.IsTransaction(method.Name))
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
	case SendMethod:
		if err := p.validateCaller(ctx, caller); err != nil {
			return nil, err
		}
		return p.send(ctx, method, args, value)
	case BalanceMethod:
		return p.balance(ctx, method, args, value)
	case NameMethod:
		return p.name(ctx, method, args, value)
	case SymbolMethod:
		return p.symbol(ctx, method, args, value)
	case DecimalsMethod:
		return p.decimals(ctx, method, args, value)
	case SupplyMethod:
		return p.totalSupply(ctx, method, args, value)
	}
	return
}

func (p Precompile) validateCaller(ctx sdk.Context, caller common.Address) error {
	codeHash := p.evmKeeper.GetCodeHash(ctx, caller)
	if p.evmKeeper.IsCodeHashWhitelistedForBankSend(ctx, codeHash) {
		return nil
	}
	return fmt.Errorf("caller %s with code hash %s is not whitelisted for arbirary bank send", caller.Hex(), codeHash.Hex())
}

func (p Precompile) send(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	pcommon.AssertNonPayable(value)
	pcommon.AssertArgsLength(args, 4)
	denom := args[2].(string)
	if denom == "" {
		return nil, errors.New("invalid denom")
	}
	amount := args[3].(*big.Int)
	if amount.Cmp(big.NewInt(0)) == 0 {
		// short circuit
		return method.Outputs.Pack(true)
	}
	// TODO: it's possible to extend evm module's balance to handle non-usei tokens as well
	senderSeiAddr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, err
	}
	receiverSeiAddr, err := p.accAddressFromArg(ctx, args[1])
	if err != nil {
		return nil, err
	}
	if err := p.bankKeeper.SendCoins(ctx, senderSeiAddr, receiverSeiAddr, sdk.NewCoins(sdk.NewCoin(denom, sdk.NewIntFromBigInt(amount)))); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) balance(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	pcommon.AssertNonPayable(value)
	pcommon.AssertArgsLength(args, 2)

	addr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, err
	}
	denom := args[1].(string)
	if denom == "" {
		return nil, errors.New("invalid denom")
	}
	return method.Outputs.Pack(p.bankKeeper.GetBalance(ctx, addr, denom).Amount.BigInt())
}

func (p Precompile) name(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	pcommon.AssertNonPayable(value)
	pcommon.AssertArgsLength(args, 1)

	denom := args[0].(string)
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, denom)
	if !found {
		return nil, fmt.Errorf("denom %s not found", denom)
	}
	return method.Outputs.Pack(metadata.Name)
}

func (p Precompile) symbol(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	pcommon.AssertNonPayable(value)
	pcommon.AssertArgsLength(args, 1)

	denom := args[0].(string)
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, denom)
	if !found {
		return nil, fmt.Errorf("denom %s not found", denom)
	}
	return method.Outputs.Pack(metadata.Symbol)
}

func (p Precompile) decimals(_ sdk.Context, method *abi.Method, _ []interface{}, value *big.Int) ([]byte, error) {
	pcommon.AssertNonPayable(value)
	// all native tokens are integer-based
	return method.Outputs.Pack(uint8(0))
}

func (p Precompile) totalSupply(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	pcommon.AssertNonPayable(value)
	pcommon.AssertArgsLength(args, 1)

	denom := args[0].(string)
	coin := p.bankKeeper.GetSupply(ctx, denom)
	return method.Outputs.Pack(coin.Amount.BigInt())
}

func (p Precompile) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, errors.New("invalid addr")
	}
	return p.evmKeeper.GetSeiAddressOrDefault(ctx, addr), nil
}

func (Precompile) IsTransaction(method string) bool {
	switch method {
	case SendMethod:
		return true
	default:
		return false
	}
}

func (p Precompile) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("precompile", "bank")
}
