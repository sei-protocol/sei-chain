package client

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/keeper"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
)

func TestDeprecatedClientProposals(t *testing.T) {
	handler := NewClientProposalHandler(keeper.Keeper{})
	proposals := []govtypes.Content{
		&types.ClientUpdateProposal{},
		&types.UpgradeProposal{},
	}

	for _, proposal := range proposals {
		require.ErrorIs(t, handler(sdk.Context{}, proposal), types.ErrClientDeprecated)
	}
}
