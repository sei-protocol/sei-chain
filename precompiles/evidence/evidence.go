package evidence

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
	evidencetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/types"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
)

const (
	EvidenceMethod    = "evidence"
	AllEvidenceMethod = "allEvidence"
)

const (
	EvidenceAddress = "0x000000000000000000000000000000000000100F"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper       utils.EVMKeeper
	evidenceQuerier utils.EvidenceQuerier
	cdc             codec.Codec

	EvidenceID    []byte
	AllEvidenceID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:       keepers.EVMK(),
		evidenceQuerier: keepers.EvidenceQ(),
		cdc:             keepers.Codec(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case EvidenceMethod:
			p.EvidenceID = m.ID
		case AllEvidenceMethod:
			p.AllEvidenceID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, common.HexToAddress(EvidenceAddress), "evidence"), nil
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
	case EvidenceMethod:
		return p.evidence(ctx, method, args, value)
	case AllEvidenceMethod:
		return p.allEvidence(ctx, method, args, value)
	}
	return
}

func (p PrecompileExecutor) evidence(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	req := &evidencetypes.QueryEvidenceRequest{
		EvidenceHash: tmbytes.HexBytes(args[0].([]byte)),
	}

	resp, err := p.evidenceQuerier.Evidence(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	evidenceJSON, err := p.cdc.MarshalAsJSON(resp.Evidence)
	if err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(evidenceJSON)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

type AllEvidenceResponse struct {
	EvidenceList [][]byte
	NextKey      []byte
}

func (p PrecompileExecutor) allEvidence(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	req := &evidencetypes.QueryAllEvidenceRequest{
		Pagination: &query.PageRequest{
			Key: args[0].([]byte),
		},
	}

	resp, err := p.evidenceQuerier.AllEvidence(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	res := AllEvidenceResponse{
		EvidenceList: make([][]byte, len(resp.Evidence)),
	}
	for i, evidenceAny := range resp.Evidence {
		evidenceJSON, err := p.cdc.MarshalAsJSON(evidenceAny)
		if err != nil {
			return nil, 0, err
		}
		res.EvidenceList[i] = evidenceJSON
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

func (p PrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return p.evmKeeper
}

func (PrecompileExecutor) IsTransaction(string) bool {
	return false
}
