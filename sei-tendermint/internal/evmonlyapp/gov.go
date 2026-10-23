package evmonlyapp

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
)

var errGovNotStarted = errors.New("gov starts after the first block")

// NewGovRegistry returns a registry running gov, weighing votes by electorate.
// The first block writes params and startingProposalID; until then gov refuses
// calls.
func NewGovRegistry(params gov.Params, startingProposalID uint64, electorate gov.GenesisElectorate) precompiles.Registry {
	return precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{
		gov.Address: govPrecompile{
			Precompile:         gov.Precompile{Electorate: electorate},
			params:             params,
			startingProposalID: startingProposalID,
		},
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

func (g govPrecompile) EndBlock(block precompiles.BlockContext, state precompiles.State) error {
	if !gov.HasParams(state) {
		gov.SetParams(state, g.params)
		if g.startingProposalID != 0 {
			if err := gov.SetStartingProposalID(state, g.startingProposalID); err != nil {
				return err
			}
		}
	}
	return g.Precompile.EndBlock(block, state)
}
