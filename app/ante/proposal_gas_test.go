package ante

import (
	"math"
	"testing"

	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	"github.com/stretchr/testify/require"
)

func TestAssociateTxProposalGasWanted(t *testing.T) {
	authParams := authtypes.DefaultParams()
	gasParams := paramtypes.DefaultCosmosGasParams()

	require.Equal(t, uint64(51_880), AssociateTxProposalGasWanted(588, authParams, *gasParams))

	gasParams.CosmosGasMultiplierNumerator = 3
	gasParams.CosmosGasMultiplierDenominator = 2
	require.Equal(t, uint64(77_820), AssociateTxProposalGasWanted(588, authParams, *gasParams))
}

func TestAssociateTxProposalGasWantedUsesConfiguredAuthCosts(t *testing.T) {
	authParams := authtypes.DefaultParams()
	authParams.TxSizeCostPerByte = 7
	authParams.SigVerifyCostSecp256k1 = 2_000
	gasParams := paramtypes.DefaultCosmosGasParams()

	require.Equal(t, uint64(47_700), AssociateTxProposalGasWanted(100, authParams, *gasParams))
}

func TestAssociateTxProposalGasWantedSaturates(t *testing.T) {
	authParams := authtypes.DefaultParams()
	authParams.TxSizeCostPerByte = math.MaxUint64
	gasParams := paramtypes.DefaultCosmosGasParams()

	require.Equal(t, uint64(math.MaxUint64), AssociateTxProposalGasWanted(2, authParams, *gasParams))

	gasParams.CosmosGasMultiplierDenominator = 0
	require.Equal(t, uint64(math.MaxUint64), AssociateTxProposalGasWanted(1, authtypes.DefaultParams(), *gasParams))
}
