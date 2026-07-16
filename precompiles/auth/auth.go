package auth

import (
	"embed"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

const (
	AccountMethod           = "account"
	AccountsMethod          = "accounts"
	ParamsMethod            = "params"
	NextAccountNumberMethod = "nextAccountNumber"
)

const (
	AuthAddress = "0x000000000000000000000000000000000000100D"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper     utils.EVMKeeper
	accountKeeper utils.AccountKeeper
	authQuerier   utils.AuthQuerier
	cdc           codec.Codec

	AccountID           []byte
	AccountsID          []byte
	ParamsID            []byte
	NextAccountNumberID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:     keepers.EVMK(),
		accountKeeper: keepers.AccountK(),
		authQuerier:   keepers.AuthQ(),
		cdc:           keepers.Codec(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case AccountMethod:
			p.AccountID = m.ID
		case AccountsMethod:
			p.AccountsID = m.ID
		case ParamsMethod:
			p.ParamsID = m.ID
		case NextAccountNumberMethod:
			p.NextAccountNumberID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, common.HexToAddress(AuthAddress), "auth"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return pcommon.DefaultGasCost(input, p.IsTransaction(method.Name))
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, hooks *tracing.Hooks) (bz []byte, remainingGas uint64, err error) {
	// Needed to catch gas meter panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("execution reverted: %v", r)
		}
	}()
	switch method.Name {
	case AccountMethod:
		return p.account(ctx, method, args, value)
	case AccountsMethod:
		return p.accounts(ctx, method, args, value)
	case ParamsMethod:
		return p.params(ctx, method, args, value)
	case NextAccountNumberMethod:
		return p.nextAccountNumber(ctx, method, args, value)
	}
	return
}

type Account struct {
	AccountAddress string
	AccountNumber  uint64
	Sequence       uint64
}

type AccountsResponse struct {
	Accounts []Account
	NextKey  []byte
}

type AuthParams struct {
	MaxMemoCharacters      uint64
	TxSigLimit             uint64
	TxSizeCostPerByte      uint64
	SigVerifyCostEd25519   uint64
	SigVerifyCostSecp256k1 uint64
	DisableSeqnoCheck      bool
}

func (p PrecompileExecutor) account(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	seiAddr, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	acc := p.accountKeeper.GetAccount(ctx, seiAddr)
	if acc == nil {
		return nil, 0, errors.New("account not found")
	}

	account := Account{
		AccountAddress: acc.GetAddress().String(),
		AccountNumber:  acc.GetAccountNumber(),
		Sequence:       acc.GetSequence(),
	}

	bz, err := method.Outputs.Pack(account)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) accounts(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	req := &authtypes.QueryAccountsRequest{
		Pagination: &query.PageRequest{
			Key: args[0].([]byte),
		},
	}

	resp, err := p.authQuerier.Accounts(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	res := AccountsResponse{
		Accounts: make([]Account, len(resp.Accounts)),
	}
	for i, accountAny := range resp.Accounts {
		acc, ok := accountAny.GetCachedValue().(authtypes.AccountI)
		if !ok || acc == nil {
			var unpacked authtypes.AccountI
			if err := p.cdc.UnpackAny(accountAny, &unpacked); err != nil {
				return nil, 0, err
			}
			acc = unpacked
		}
		res.Accounts[i] = Account{
			AccountAddress: acc.GetAddress().String(),
			AccountNumber:  acc.GetAccountNumber(),
			Sequence:       acc.GetSequence(),
		}
	}
	if resp.Pagination != nil {
		res.NextKey = resp.Pagination.NextKey
	}

	bz, err := method.Outputs.Pack(res)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) params(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	resp, err := p.authQuerier.Params(sdk.WrapSDKContext(ctx), &authtypes.QueryParamsRequest{})
	if err != nil {
		return nil, 0, err
	}

	params := AuthParams{
		MaxMemoCharacters:      resp.Params.MaxMemoCharacters,
		TxSigLimit:             resp.Params.TxSigLimit,
		TxSizeCostPerByte:      resp.Params.TxSizeCostPerByte,
		SigVerifyCostEd25519:   resp.Params.SigVerifyCostED25519,
		SigVerifyCostSecp256k1: resp.Params.SigVerifyCostSecp256k1,
		DisableSeqnoCheck:      resp.Params.DisableSeqnoCheck,
	}

	bz, err := method.Outputs.Pack(params)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) nextAccountNumber(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	resp, err := p.authQuerier.NextAccountNumber(sdk.WrapSDKContext(ctx), &authtypes.QueryNextAccountNumberRequest{})
	if err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(resp.Count)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return p.evmKeeper
}

func (PrecompileExecutor) IsTransaction(string) bool {
	return false
}
