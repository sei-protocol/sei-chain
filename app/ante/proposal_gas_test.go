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

	dimensions := AssociateTxGasDimensions{TxSize: 588, SignerCount: 1, SignatureCount: 1}
	require.Equal(t, uint64(51_880), AssociateTxProposalGasWanted(dimensions, authParams, *gasParams))

	gasParams.CosmosGasMultiplierNumerator = 3
	gasParams.CosmosGasMultiplierDenominator = 2
	require.Equal(t, uint64(77_820), AssociateTxProposalGasWanted(dimensions, authParams, *gasParams))
}

func TestAssociateTxProposalGasWantedAccountsForAllStateAccess(t *testing.T) {
	authParams := authtypes.DefaultParams()
	gasParams := paramtypes.DefaultCosmosGasParams()

	twoSigners := AssociateTxGasDimensions{TxSize: 588, SignerCount: 2, SignatureCount: 2}
	require.Equal(t, uint64(97_880), AssociateTxProposalGasWanted(twoSigners, authParams, *gasParams))
	oneSignerWithFeeGrant := AssociateTxGasDimensions{TxSize: 588, SignerCount: 1, SignatureCount: 1, UsesFeeGrant: true}
	require.Equal(t, uint64(96_880), AssociateTxProposalGasWanted(oneSignerWithFeeGrant, authParams, *gasParams))
}

func TestAssociateTxProposalGasWantedUsesConfiguredAuthCosts(t *testing.T) {
	authParams := authtypes.DefaultParams()
	authParams.TxSizeCostPerByte = 7
	authParams.SigVerifyCostSecp256k1 = 2_000
	gasParams := paramtypes.DefaultCosmosGasParams()

	dimensions := AssociateTxGasDimensions{TxSize: 100, SignerCount: 1, SignatureCount: 1}
	require.Equal(t, uint64(47_700), AssociateTxProposalGasWanted(dimensions, authParams, *gasParams))

	authParams.SigVerifyCostED25519 = 3_000
	require.Equal(t, uint64(48_700), AssociateTxProposalGasWanted(dimensions, authParams, *gasParams))
}

func TestAssociateTxProposalGasWantedSaturates(t *testing.T) {
	authParams := authtypes.DefaultParams()
	authParams.TxSizeCostPerByte = math.MaxUint64
	gasParams := paramtypes.DefaultCosmosGasParams()

	require.Equal(t, uint64(math.MaxUint64), AssociateTxProposalGasWanted(AssociateTxGasDimensions{
		TxSize: 2, SignerCount: 1, SignatureCount: 1,
	}, authParams, *gasParams))

	gasParams.CosmosGasMultiplierDenominator = 0
	require.Equal(t, uint64(math.MaxUint64), AssociateTxProposalGasWanted(AssociateTxGasDimensions{
		TxSize: 1, SignerCount: 1, SignatureCount: 1,
	}, authtypes.DefaultParams(), *gasParams))

	gasParams = paramtypes.DefaultCosmosGasParams()
	require.Equal(t, uint64(math.MaxUint64), AssociateTxProposalGasWanted(AssociateTxGasDimensions{
		TxSize: 1, SignerCount: math.MaxUint64, SignatureCount: 1, UsesFeeGrant: true,
	}, authtypes.DefaultParams(), *gasParams))
}
