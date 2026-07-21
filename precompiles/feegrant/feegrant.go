package feegrant

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
	feegranttypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant"
)

const (
	AllowanceMethod           = "allowance"
	AllowancesMethod          = "allowances"
	AllowancesByGranterMethod = "allowancesByGranter"
)

const (
	FeegrantAddress = "0x0000000000000000000000000000000000001010"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper       utils.EVMKeeper
	feegrantQuerier utils.FeegrantQuerier
	cdc             codec.Codec

	AllowanceID           []byte
	AllowancesID          []byte
	AllowancesByGranterID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:       keepers.EVMK(),
		feegrantQuerier: keepers.FeegrantQ(),
		cdc:             keepers.Codec(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case AllowanceMethod:
			p.AllowanceID = m.ID
		case AllowancesMethod:
			p.AllowancesID = m.ID
		case AllowancesByGranterMethod:
			p.AllowancesByGranterID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, common.HexToAddress(FeegrantAddress), "feegrant"), nil
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
	case AllowanceMethod:
		return p.allowance(ctx, method, args, value)
	case AllowancesMethod:
		return p.allowances(ctx, method, args, value)
	case AllowancesByGranterMethod:
		return p.allowancesByGranter(ctx, method, args, value)
	}
	return
}

type Grant struct {
	Granter   string
	Grantee   string
	Allowance []byte
}

type AllowancesResponse struct {
	Allowances []Grant
	NextKey    []byte
}

func (p PrecompileExecutor) allowance(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	granter, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	grantee, err := pcommon.GetSeiAddressFromArg(ctx, args[1], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	req := &feegranttypes.QueryAllowanceRequest{
		Granter: granter.String(),
		Grantee: grantee.String(),
	}

	resp, err := p.feegrantQuerier.Allowance(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	grant, err := p.toGrant(resp.Allowance)
	if err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(grant)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) allowances(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	grantee, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	req := &feegranttypes.QueryAllowancesRequest{
		Grantee: grantee.String(),
		Pagination: &query.PageRequest{
			Key: args[1].([]byte),
		},
	}

	resp, err := p.feegrantQuerier.Allowances(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	res, err := p.toAllowancesResponse(resp.Allowances, resp.Pagination)
	if err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(res)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) allowancesByGranter(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	granter, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	req := &feegranttypes.QueryAllowancesByGranterRequest{
		Granter: granter.String(),
		Pagination: &query.PageRequest{
			Key: args[1].([]byte),
		},
	}

	resp, err := p.feegrantQuerier.AllowancesByGranter(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	res, err := p.toAllowancesResponse(resp.Allowances, resp.Pagination)
	if err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(res)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) toGrant(grant *feegranttypes.Grant) (Grant, error) {
	if grant == nil {
		return Grant{}, errors.New("no allowance found")
	}
	allowanceJSON, err := p.cdc.MarshalAsJSON(grant.Allowance)
	if err != nil {
		return Grant{}, err
	}
	return Grant{
		Granter:   grant.Granter,
		Grantee:   grant.Grantee,
		Allowance: allowanceJSON,
	}, nil
}

func (p PrecompileExecutor) toAllowancesResponse(grants []*feegranttypes.Grant, pagination *query.PageResponse) (AllowancesResponse, error) {
	res := AllowancesResponse{
		Allowances: make([]Grant, len(grants)),
	}
	for i, grant := range grants {
		g, err := p.toGrant(grant)
		if err != nil {
			return AllowancesResponse{}, err
		}
		res.Allowances[i] = g
	}
	if pagination != nil {
		res.NextKey = pagination.NextKey
	}
	return res, nil
}

func (p PrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return p.evmKeeper
}

func (PrecompileExecutor) IsTransaction(string) bool {
	return false
}
