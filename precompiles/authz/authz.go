package authz

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
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	authztypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
)

const (
	GrantsMethod        = "grants"
	GranterGrantsMethod = "granterGrants"
	GranteeGrantsMethod = "granteeGrants"
)

const (
	AuthzAddress = "0x000000000000000000000000000000000000100E"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper    utils.EVMKeeper
	authzQuerier utils.AuthzQuerier
	cdc          codec.Codec

	GrantsID        []byte
	GranterGrantsID []byte
	GranteeGrantsID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:    keepers.EVMK(),
		authzQuerier: keepers.AuthzQ(),
		cdc:          keepers.Codec(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case GrantsMethod:
			p.GrantsID = m.ID
		case GranterGrantsMethod:
			p.GranterGrantsID = m.ID
		case GranteeGrantsMethod:
			p.GranteeGrantsID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, common.HexToAddress(AuthzAddress), "authz"), nil
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
	case GrantsMethod:
		return p.grants(ctx, method, args, value)
	case GranterGrantsMethod:
		return p.granterGrants(ctx, method, args, value)
	case GranteeGrantsMethod:
		return p.granteeGrants(ctx, method, args, value)
	}
	return
}

type Grant struct {
	Authorization []byte
	Expiration    int64
}

type GrantsResponse struct {
	Grants  []Grant
	NextKey []byte
}

type GrantAuthorization struct {
	Granter       string
	Grantee       string
	Authorization []byte
	Expiration    int64
}

type GrantAuthorizationsResponse struct {
	Grants  []GrantAuthorization
	NextKey []byte
}

func (p PrecompileExecutor) grants(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 4); err != nil {
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

	msgTypeUrl := args[2].(string)
	req := &authztypes.QueryGrantsRequest{
		Granter:    granter.String(),
		Grantee:    grantee.String(),
		MsgTypeUrl: msgTypeUrl,
		Pagination: &query.PageRequest{
			Key: args[3].([]byte),
		},
	}

	resp, err := p.authzQuerier.Grants(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	res := GrantsResponse{
		Grants: make([]Grant, len(resp.Grants)),
	}
	for i, grant := range resp.Grants {
		authorizationJSON, err := p.cdc.MarshalAsJSON(grant.Authorization)
		if err != nil {
			return nil, 0, err
		}
		res.Grants[i] = Grant{
			Authorization: authorizationJSON,
			Expiration:    grant.Expiration.Unix(),
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

func (p PrecompileExecutor) granterGrants(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
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

	req := &authztypes.QueryGranterGrantsRequest{
		Granter: granter.String(),
		Pagination: &query.PageRequest{
			Key: args[1].([]byte),
		},
	}

	resp, err := p.authzQuerier.GranterGrants(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	grants, err := p.convertGrantAuthorizations(resp.Grants)
	if err != nil {
		return nil, 0, err
	}

	res := GrantAuthorizationsResponse{
		Grants: grants,
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

func (p PrecompileExecutor) granteeGrants(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
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

	req := &authztypes.QueryGranteeGrantsRequest{
		Grantee: grantee.String(),
		Pagination: &query.PageRequest{
			Key: args[1].([]byte),
		},
	}

	resp, err := p.authzQuerier.GranteeGrants(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	grants, err := p.convertGrantAuthorizations(resp.Grants)
	if err != nil {
		return nil, 0, err
	}

	res := GrantAuthorizationsResponse{
		Grants: grants,
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

func (p PrecompileExecutor) convertGrantAuthorizations(grants []*authztypes.GrantAuthorization) ([]GrantAuthorization, error) {
	res := make([]GrantAuthorization, len(grants))
	for i, grant := range grants {
		authorizationJSON, err := p.cdc.MarshalAsJSON(grant.Authorization)
		if err != nil {
			return nil, err
		}
		res[i] = GrantAuthorization{
			Granter:       grant.Granter,
			Grantee:       grant.Grantee,
			Authorization: authorizationJSON,
			Expiration:    grant.Expiration.Unix(),
		}
	}
	return res, nil
}

func (p PrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return p.evmKeeper
}

func (PrecompileExecutor) IsTransaction(string) bool {
	return false
}
