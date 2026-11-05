package evmonlyapp

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

var (
	errGovNotStarted    = errors.New("gov starts after the first block")
	errParamsNotStarted = errors.New("params start after the first block")
)

// NewGovRegistry returns a registry running gov, weighing votes by electorate,
// and the params precompile. The first block writes params and
// startingProposalID; until then both refuse calls.
func NewGovRegistry(params gov.Params, startingProposalID uint64, electorate gov.GenesisElectorate) precompiles.Registry {
	return precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{
		gov.Address: govPrecompile{
			Precompile:         gov.Precompile{Electorate: electorate},
			params:             params,
			startingProposalID: startingProposalID,
		},
		chainparams.Address: paramsPrecompile{},
	}, gov.Address)
}

// govPrecompile is gov with the genesis state it writes at its first EndBlock.
type govPrecompile struct {
	gov.Precompile
	params             gov.Params
	startingProposalID uint64
}

func (g govPrecompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	if !gov.HasParams(ctx.State) {
		return nil, errGovNotStarted
	}
	return g.Precompile.Run(ctx, input)
}

func (g govPrecompile) EndBlock(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	if !gov.HasParams(state) {
		if err := gov.SetParams(state, g.params); err != nil {
			return nil, err
		}
		if g.startingProposalID != 0 {
			if err := gov.SetStartingProposalID(state, g.startingProposalID); err != nil {
				return nil, err
			}
		}
	}
	return g.Precompile.EndBlock(block, state)
}

// paramsPrecompile is the params precompile, refusing calls until the first
// block writes gov's params.
type paramsPrecompile struct {
	chainparams.Precompile
}

func (p paramsPrecompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	if !gov.HasParams(ctx.State) {
		return nil, errParamsNotStarted
	}
	return p.Precompile.Run(ctx, input)
}
