package core

import (
	"context"
	"maps"
	"slices"

	tmmath "github.com/sei-protocol/sei-chain/sei-tendermint/libs/math"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// Validators gets the validator set at the given block height.
//
// If no height is provided, it will fetch the latest validator set. Note the
// validators are sorted by their voting power - this is the canonical order
// for the validators in the set as used in computing their Merkle root.
//
// More: https://docs.tendermint.com/master/rpc/#/Info/validators
//
// Under Autobahn the CometBFT StateStore is not populated, but the committee
// is fixed at genesis (no validator-updates path under Autobahn), so any
// retained height returns the genesis committee. Pagination + error shape
// mirror the CometBFT path so external tools see consistent responses.
func (env *Environment) Validators(ctx context.Context, req *coretypes.RequestValidators) (*coretypes.ResultValidators, error) {
	if env.gigaRouter().IsPresent() {
		height, err := env.autobahnCheckAndGetHeight(ctx, (*int64)(req.Height))
		if err != nil {
			return nil, err
		}
		gvals := env.GenDoc.Validators
		vs := make([]*types.Validator, 0, len(gvals))
		for _, gv := range gvals {
			vs = append(vs, &types.Validator{
				Address:     gv.Address,
				PubKey:      gv.PubKey,
				VotingPower: gv.Power,
				// ProposerPriority left at 0; Autobahn uses round-robin
				// per-block leader selection (Committee.Leader), not
				// Tendermint's proposer-priority accumulator.
			})
		}
		totalCount := len(vs)
		perPage := env.validatePerPage(req.PerPage.IntPtr())
		page, err := validatePage(req.Page.IntPtr(), perPage, totalCount)
		if err != nil {
			return nil, err
		}
		skipCount := validateSkipCount(page, perPage)
		v := vs[skipCount : skipCount+tmmath.MinInt(perPage, totalCount-skipCount)]
		return &coretypes.ResultValidators{
			BlockHeight: height,
			Validators:  v,
			Count:       len(v),
			Total:       totalCount,
		}, nil
	}

	// The latest validator that we know is the NextValidator of the last block.
	height, err := env.getHeight(env.latestUncommittedHeight(), (*int64)(req.Height))
	if err != nil {
		return nil, err
	}

	validators, err := env.StateStore.LoadValidators(height)
	if err != nil {
		return nil, err
	}

	totalCount := len(validators.Validators)
	perPage := env.validatePerPage(req.PerPage.IntPtr())
	page, err := validatePage(req.Page.IntPtr(), perPage, totalCount)
	if err != nil {
		return nil, err
	}

	skipCount := validateSkipCount(page, perPage)

	v := validators.Validators[skipCount : skipCount+tmmath.MinInt(perPage, totalCount-skipCount)]

	return &coretypes.ResultValidators{
		BlockHeight: height,
		Validators:  v,
		Count:       len(v),
		Total:       totalCount,
	}, nil
}

// DumpConsensusState dumps consensus state.
// UNSTABLE
// More: https://docs.tendermint.com/master/rpc/#/Info/dump_consensus_state
func (env *Environment) DumpConsensusState(ctx context.Context) (*coretypes.ResultDumpConsensusState, error) {
	// Get Peer consensus states.
	reactor, err := env.requireConsensusReactor()
	if err != nil {
		return nil, err
	}
	consensusState, err := env.requireConsensusState()
	if err != nil {
		return nil, err
	}

	peerStates := map[types.NodeID]coretypes.PeerStateInfo{}
	for _, info := range env.Router.ConnInfos() {
		if _, ok := peerStates[info.ID]; ok {
			continue
		}
		peerState, ok := reactor.GetPeerState(info.ID)
		if !ok {
			continue
		}
		peerStateJSON, err := peerState.ToJSON()
		if err != nil {
			return nil, err
		}

		nodeAddress := ""
		if addr, ok := info.DialedAddr.Get(); ok {
			nodeAddress = addr.String()
		} else if addr, ok := info.SelfDeclaredAddr.Get(); ok {
			nodeAddress = addr.String()
		}

		peerStates[info.ID] = coretypes.PeerStateInfo{
			// Peer basic info.
			NodeAddress: nodeAddress,
			// Peer consensus state.
			PeerState: peerStateJSON,
		}
	}

	// Get self round state.
	roundState, err := consensusState.GetRoundStateJSON()
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultDumpConsensusState{
		RoundState: roundState,
		Peers:      slices.Collect(maps.Values(peerStates)),
	}, nil
}

// ConsensusState returns a concise summary of the consensus state.
// UNSTABLE
// More: https://docs.tendermint.com/master/rpc/#/Info/consensus_state
func (env *Environment) GetConsensusState(ctx context.Context) (*coretypes.ResultConsensusState, error) {
	consensusState, err := env.requireConsensusState()
	if err != nil {
		return nil, err
	}
	// Get self round state.
	bz, err := consensusState.GetRoundStateSimpleJSON()
	return &coretypes.ResultConsensusState{RoundState: bz}, err
}

// ConsensusParams gets the consensus parameters at the given block height.
// If no height is provided, it will fetch the latest consensus params.
// More: https://docs.tendermint.com/master/rpc/#/Info/consensus_params
func (env *Environment) ConsensusParams(ctx context.Context, req *coretypes.RequestConsensusParams) (*coretypes.ResultConsensusParams, error) {
	// The latest consensus params that we know is the consensus params after
	// the last block.
	height, err := env.getHeight(env.latestUncommittedHeight(), (*int64)(req.Height))
	if err != nil {
		return nil, err
	}

	consensusParams, err := env.StateStore.LoadConsensusParams(height)
	if err != nil {
		return nil, err
	}

	consensusParams.Synchrony = consensusParams.Synchrony.SynchronyParamsOrDefaults()
	consensusParams.Timeout = consensusParams.Timeout.Or(types.DefaultTimeoutParams())

	return &coretypes.ResultConsensusParams{
		BlockHeight:     height,
		ConsensusParams: consensusParams}, nil
}
