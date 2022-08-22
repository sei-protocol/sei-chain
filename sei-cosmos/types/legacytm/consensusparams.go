package legacytm

import (
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func LegacyToABCIConsensusParams(legacyParams *ConsensusParams) *tmproto.ConsensusParams {
	return &tmproto.ConsensusParams{
		Block: &tmproto.BlockParams{
			MaxBytes: legacyParams.Block.MaxBytes,
			MaxGas:   legacyParams.Block.MaxGas,
		},
		Evidence:  legacyParams.Evidence,
		Validator: legacyParams.Validator,
		Version:   legacyParams.Version,
	}
}

func ABCIToLegacyConsensusParams(params *tmproto.ConsensusParams) *ConsensusParams {
	return &ConsensusParams{
		Block: &BlockParams{
			MaxBytes: params.Block.MaxBytes,
			MaxGas:   params.Block.MaxGas,
		},
		Evidence:  params.Evidence,
		Validator: params.Validator,
		Version:   params.Version,
	}
}
