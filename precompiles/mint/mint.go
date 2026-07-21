package mint

import (
	"embed"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
)

const (
	ParamsMethod = "params"
	MinterMethod = "minter"
)

const (
	MintAddress = "0x0000000000000000000000000000000000001012"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper   utils.EVMKeeper
	mintQuerier utils.MintQuerier

	ParamsID []byte
	MinterID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:   keepers.EVMK(),
		mintQuerier: keepers.MintQ(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case ParamsMethod:
			p.ParamsID = m.ID
		case MinterMethod:
			p.MinterID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, common.HexToAddress(MintAddress), "mint"), nil
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
	if !p.IsTransaction(method.Name) {
		// Queries must not mutate state even when the underlying querier has
		// side effects (e.g. auth's NextAccountNumber increments the global
		// account number counter, gov's Tally deletes votes), so run every
		// view on a branched context and discard the writes.
		ctx, _ = ctx.CacheContext()
	}
	switch method.Name {
	case ParamsMethod:
		return p.params(ctx, method, args, value)
	case MinterMethod:
		return p.minter(ctx, method, args, value)
	}
	return
}

type ScheduledTokenRelease struct {
	StartDate          string
	EndDate            string
	TokenReleaseAmount uint64
}

type MintParams struct {
	MintDenom            string
	TokenReleaseSchedule []ScheduledTokenRelease
}

type Minter struct {
	StartDate           string
	EndDate             string
	Denom               string
	TotalMintAmount     uint64
	RemainingMintAmount uint64
	LastMintAmount      uint64
	LastMintDate        string
	LastMintHeight      uint64
}

func (p PrecompileExecutor) params(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	request := &minttypes.QueryParamsRequest{}
	response, err := p.mintQuerier.Params(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	tokenReleaseSchedule := make([]ScheduledTokenRelease, 0, len(response.Params.TokenReleaseSchedule))
	for _, release := range response.Params.TokenReleaseSchedule {
		tokenReleaseSchedule = append(tokenReleaseSchedule, ScheduledTokenRelease{
			StartDate:          release.StartDate,
			EndDate:            release.EndDate,
			TokenReleaseAmount: release.TokenReleaseAmount,
		})
	}

	params := MintParams{
		MintDenom:            response.Params.MintDenom,
		TokenReleaseSchedule: tokenReleaseSchedule,
	}

	bz, err := method.Outputs.Pack(params)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) minter(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	request := &minttypes.QueryMinterRequest{}
	response, err := p.mintQuerier.Minter(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	minter := Minter{
		StartDate:           response.StartDate,
		EndDate:             response.EndDate,
		Denom:               response.Denom,
		TotalMintAmount:     response.TotalMintAmount,
		RemainingMintAmount: response.RemainingMintAmount,
		LastMintAmount:      response.LastMintAmount,
		LastMintDate:        response.LastMintDate,
		LastMintHeight:      response.LastMintHeight,
	}

	bz, err := method.Outputs.Pack(minter)
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
