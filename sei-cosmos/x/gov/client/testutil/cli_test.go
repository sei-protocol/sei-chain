//go:build norace
// +build norace

package testutil

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/network"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestIntegrationTestSuite(t *testing.T) {
	cfg := network.DefaultConfig()
	cfg.NumValidators = 1
	suite.Run(t, NewIntegrationTestSuite(cfg))

	genesisState := types.DefaultGenesisState()
	genesisState.DepositParams = types.NewDepositParams(
		sdk.NewCoins(sdk.NewCoin(cfg.BondDenom, types.DefaultMinDepositTokens)),
		sdk.NewCoins(sdk.NewCoin(cfg.BondDenom, types.DefaultMinExpeditedDepositTokens)),
		time.Duration(15)*time.Second)
	genesisState.VotingParams = types.NewVotingParams(time.Duration(5)*time.Second, time.Duration(2)*time.Second)
	bz, err := cfg.Codec.MarshalAsJSON(genesisState)
	require.NoError(t, err)
	cfg.GenesisState["gov"] = bz
	suite.Run(t, NewDepositTestSuite(cfg))
}
