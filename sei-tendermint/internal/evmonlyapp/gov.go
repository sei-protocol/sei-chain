package evmonlyapp

import (
	"errors"
	"slices"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
)

var (
	errGovNotStarted    = errors.New("gov starts after the first block")
	errParamsNotStarted = errors.New("params start after the first block")
)

// NewGovRegistry returns a registry running gov, weighing votes by electorate,
// the params precompile and the upgrade precompile. The first block writes
// params, startingProposalID and appliedUpgrades, the upgrade names the chain
// starts past; until then gov and params refuse calls.
func NewGovRegistry(params gov.Params, startingProposalID uint64, electorate gov.GenesisElectorate, appliedUpgrades []string) precompiles.Registry {
	return precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{
		gov.Address: govPrecompile{
			Precompile:         gov.Precompile{Electorate: electorate},
			params:             params,
			startingProposalID: startingProposalID,
			appliedUpgrades:    slices.Clone(appliedUpgrades),
		},
		chainparams.Address: paramsPrecompile{},
		upgrade.Address:     upgrade.Precompile{Handlers: upgradeHandlers()},
	}, gov.Address).WithBeginBlockers(upgrade.Address)
}

// govPrecompile is gov with the genesis state it writes at its first EndBlock.
type govPrecompile struct {
	gov.Precompile
	params             gov.Params
	startingProposalID uint64
	appliedUpgrades    []string
}

func (g govPrecompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	if !gov.HasParams(ctx.State) {
		return nil, errGovNotStarted
	}
	return g.Precompile.Run(ctx, input)
}

func (g govPrecompile) EndBlock(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	if !gov.HasParams(state) {
		for _, write := range g.genesisWriters() {
			if err := write(state, block.Number); err != nil {
				return nil, err
			}
		}
	}
	return g.Precompile.EndBlock(block, state)
}

// genesisWriter writes one precompile's genesis state, recorded at height.
type genesisWriter func(state precompiles.State, height uint64) error

// genesisWriters returns the genesis writers gov's first EndBlock runs, in
// order.
//
// TODO(evmonly-initchain-genesis): run these from InitChain, at the chain's
// initial height, instead of gov's first EndBlock.
func (g govPrecompile) genesisWriters() []genesisWriter {
	return []genesisWriter{g.writeGovGenesis, g.writeUpgradeGenesis}
}

// writeGovGenesis writes gov's params and, when set, its starting proposal ID.
func (g govPrecompile) writeGovGenesis(state precompiles.State, _ uint64) error {
	if err := gov.SetParams(state, g.params); err != nil {
		return err
	}
	if g.startingProposalID == 0 {
		return nil
	}
	return gov.SetStartingProposalID(state, g.startingProposalID)
}

// writeUpgradeGenesis records the upgrades the chain starts past as applied
// at height.
func (g govPrecompile) writeUpgradeGenesis(state precompiles.State, height uint64) error {
	return upgrade.WriteGenesis(state, g.appliedUpgrades, height)
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
