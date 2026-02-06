package legacytm

import (
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func ABCIToLegacyConsensusParams(params *tmproto.ConsensusParams) *abci.ConsensusParams {
	block := abci.BlockParams{}
	if params.Block != nil {
		block.MaxBytes = params.Block.MaxBytes
		block.MaxGas = params.Block.MaxGas
		block.MinTxsInBlock = params.Block.MinTxsInBlock
		block.MaxGasWanted = params.Block.MaxGasWanted
	}
	return &abci.ConsensusParams{
		Block:     &block,
		Evidence:  params.Evidence,
		Validator: params.Validator,
		Version:   params.Version,
	}
}
