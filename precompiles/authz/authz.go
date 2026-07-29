package authz

import (
	"embed"
	"errors"
	"fmt"
	"math/big"
	"time"

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
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	GrantMethod         = "grant"
	ExecMethod          = "exec"
	RevokeMethod        = "revoke"
	GrantsMethod        = "grants"
	GranterGrantsMethod = "granterGrants"
	GranteeGrantsMethod = "granteeGrants"
)

// maxNestedMsgs caps MsgExec nesting when scanning exec messages, mirroring
// app/antedecorators.AuthzNestedMessageDecorator (which precompile calls
// bypass since they don't go through the cosmos ante handlers).
const maxNestedMsgs = 5

const (
	AuthzAddress = "0x000000000000000000000000000000000000100E"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper      utils.EVMKeeper
	authzMsgServer utils.AuthzMsgServer
	authzQuerier   utils.AuthzQuerier
	cdc            codec.Codec

	GrantID         []byte
	ExecID          []byte
	RevokeID        []byte
	GrantsID        []byte
	GranterGrantsID []byte
	GranteeGrantsID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:      keepers.EVMK(),
		authzMsgServer: keepers.AuthzMS(),
		authzQuerier:   keepers.AuthzQ(),
		cdc:            keepers.Codec(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case GrantMethod:
			p.GrantID = m.ID
		case ExecMethod:
			p.ExecID = m.ID
		case RevokeMethod:
			p.RevokeID = m.ID
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

	// Transaction methods act on behalf of the caller, so they must not be
	// reachable through delegatecall (which would let a contract act on
	// behalf of its own caller) or staticcall.
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, 0, errors.New("cannot delegatecall authz")
	}
	if readOnly {
		return nil, 0, errors.New("cannot call authz precompile from staticcall")
	}
	switch method.Name {
	case GrantMethod:
		return p.grant(ctx, method, caller, args, value)
	case ExecMethod:
		return p.exec(ctx, method, caller, args, value)
	case RevokeMethod:
		return p.revoke(ctx, method, caller, args, value)
	}
	return
}

// grant stores an authorization from the caller (granter) to the grantee. The
// authorization is a protobuf-JSON encoded Authorization (the same encoding
// the query methods return), e.g.
// {"@type":"/cosmos.bank.v1beta1.SendAuthorization","spend_limit":[...]}.
func (p PrecompileExecutor) grant(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 3); err != nil {
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

	var authorization authztypes.Authorization
	if err := p.cdc.UnmarshalInterfaceJSON(args[1].([]byte), &authorization); err != nil {
		return nil, 0, fmt.Errorf("failed to parse authorization JSON: %w", err)
	}

	expiration := time.Unix(args[2].(int64), 0).UTC()
	msg, err := authztypes.NewMsgGrant(granter, grantee, authorization, expiration)
	if err != nil {
		return nil, 0, err
	}

	if err := msg.ValidateBasic(); err != nil {
		return nil, 0, err
	}

	if _, err := p.authzMsgServer.Grant(sdk.WrapSDKContext(ctx), msg); err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(true)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

// exec executes messages (each protobuf-JSON encoded) with the caller as the
// authz grantee. Messages whose signer is not the caller require a matching
// grant. EVM messages are rejected, mirroring the cosmos ante handler's
// restriction on nested EVM messages in MsgExec.
func (p PrecompileExecutor) exec(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	grantee, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, 0, evmtypes.NewAssociationMissingErr(caller.Hex())
	}

	msgBzs := args[0].([][]byte)
	msgs := make([]sdk.Msg, len(msgBzs))
	for i, msgBz := range msgBzs {
		var m sdk.Msg
		if err := p.cdc.UnmarshalInterfaceJSON(msgBz, &m); err != nil {
			return nil, 0, fmt.Errorf("failed to parse message JSON: %w", err)
		}
		msgs[i] = m
	}

	if err := checkNoNestedEVMMessages(msgs, 0); err != nil {
		return nil, 0, err
	}

	msg := authztypes.NewMsgExec(grantee, msgs)
	if err := msg.ValidateBasic(); err != nil {
		return nil, 0, err
	}

	resp, err := p.authzMsgServer.Exec(sdk.WrapSDKContext(ctx), &msg)
	if err != nil {
		return nil, 0, err
	}

	results := resp.Results
	if results == nil {
		results = [][]byte{}
	}
	bz, err := method.Outputs.Pack(results)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

// revoke deletes the caller's grant to the grantee for the given msg type url.
func (p PrecompileExecutor) revoke(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, uint64, error) {
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

	msg := authztypes.NewMsgRevoke(granter, grantee, args[1].(string))
	if err := msg.ValidateBasic(); err != nil {
		return nil, 0, err
	}

	if _, err := p.authzMsgServer.Revoke(sdk.WrapSDKContext(ctx), &msg); err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(true)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

// checkNoNestedEVMMessages rejects EVM messages anywhere in the message tree,
// recursing through nested MsgExecs, mirroring
// app/antedecorators.AuthzNestedMessageDecorator.
func checkNoNestedEVMMessages(msgs []sdk.Msg, nestedLvl int) error {
	if nestedLvl >= maxNestedMsgs {
		return errors.New("permission denied, more nested msgs than permitted")
	}
	for _, m := range msgs {
		switch m := m.(type) {
		case *evmtypes.MsgEVMTransaction:
			return errors.New("permission denied, authz exec contains evm message")
		case *authztypes.MsgExec:
			nested, err := m.GetMessages()
			if err != nil {
				return err
			}
			if err := checkNoNestedEVMMessages(nested, nestedLvl+1); err != nil {
				return err
			}
		}
	}
	return nil
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

// IsTransaction returns true for methods that mutate state; all other authz
// methods are views.
func (PrecompileExecutor) IsTransaction(method string) bool {
	switch method {
	case GrantMethod, ExecMethod, RevokeMethod:
		return true
	default:
		return false
	}
}
