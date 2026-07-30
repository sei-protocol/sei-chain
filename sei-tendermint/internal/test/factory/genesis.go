package factory

import (
	"time"

	cfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func GenesisDoc(
	config *cfg.Config,
	time time.Time,
	validators []*types.Validator,
	consensusParams *types.ConsensusParams,
) *types.GenesisDoc {
	existing, err := types.GenesisDocFromFile(config.GenesisFile())
	if err != nil {
		panic(err)
	}

	genesisValidators := make([]types.GenesisValidator, len(validators))

	for i := range validators {
		genesisValidators[i] = types.GenesisValidator{
			Power:  validators[i].VotingPower,
			PubKey: validators[i].PubKey,
		}
	}

	return &types.GenesisDoc{
		GenesisTime:     time,
		InitialHeight:   1,
		ChainID:         existing.ChainID,
		Validators:      genesisValidators,
		ConsensusParams: consensusParams,
	}
}
