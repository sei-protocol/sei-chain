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
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	GrantAllowanceMethod      = "grantAllowance"
	RevokeAllowanceMethod     = "revokeAllowance"
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
	evmKeeper         utils.EVMKeeper
	feegrantMsgServer utils.FeegrantMsgServer
	feegrantQuerier   utils.FeegrantQuerier
	cdc               codec.Codec

	GrantAllowanceID      []byte
	RevokeAllowanceID     []byte
	AllowanceID           []byte
	AllowancesID          []byte
	AllowancesByGranterID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:         keepers.EVMK(),
		feegrantMsgServer: keepers.FeegrantMS(),
		feegrantQuerier:   keepers.FeegrantQ(),
		cdc:               keepers.Codec(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case GrantAllowanceMethod:
			p.GrantAllowanceID = m.ID
		case RevokeAllowanceMethod:
			p.RevokeAllowanceID = m.ID
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

	// Transaction methods act on behalf of the caller, so they must not be
	// reachable through delegatecall (which would let a contract act on
	// behalf of its own caller) or staticcall.
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, 0, errors.New("cannot delegatecall feegrant")
	}
	if readOnly {
		return nil, 0, errors.New("cannot call feegrant precompile from staticcall")
	}
	switch method.Name {
	case GrantAllowanceMethod:
		return p.grantAllowance(ctx, method, caller, args, value)
	case RevokeAllowanceMethod:
		return p.revokeAllowance(ctx, method, caller, args, value)
	}
	return
}

// grantAllowance stores a fee allowance from the caller (granter) to the
// grantee. The allowance is a protobuf-JSON encoded FeeAllowanceI (the same
// encoding the query methods return), e.g.
// {"@type":"/cosmos.feegrant.v1beta1.BasicAllowance","spend_limit":[...],"expiration":null}.
func (p PrecompileExecutor) grantAllowance(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	granter, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, 0, evmtypes.NewAssociationMissingErr(caller.Hex())
	}

	grantee, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	var allowance feegranttypes.FeeAllowanceI
	if err := p.cdc.UnmarshalInterfaceJSON(args[1].([]byte), &allowance); err != nil {
		return nil, 0, fmt.Errorf("failed to parse allowance JSON: %w", err)
	}

	msg, err := feegranttypes.NewMsgGrantAllowance(allowance, granter, grantee)
	if err != nil {
		return nil, 0, err
	}

	if err := msg.ValidateBasic(); err != nil {
		return nil, 0, err
	}

	if _, err := p.feegrantMsgServer.GrantAllowance(sdk.WrapSDKContext(ctx), msg); err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(true)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

// revokeAllowance removes the caller's fee allowance to the grantee.
func (p PrecompileExecutor) revokeAllowance(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	granter, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, 0, evmtypes.NewAssociationMissingErr(caller.Hex())
	}

	grantee, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	msg := feegranttypes.NewMsgRevokeAllowance(granter, grantee)
	if err := msg.ValidateBasic(); err != nil {
		return nil, 0, err
	}

	if _, err := p.feegrantMsgServer.RevokeAllowance(sdk.WrapSDKContext(ctx), &msg); err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(true)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
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

// IsTransaction returns true for methods that mutate state; all other feegrant
// methods are views.
func (PrecompileExecutor) IsTransaction(method string) bool {
	switch method {
	case GrantAllowanceMethod, RevokeAllowanceMethod:
		return true
	default:
		return false
	}
}
