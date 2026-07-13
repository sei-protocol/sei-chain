package app

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/utils/helpers"
	"github.com/stretchr/testify/require"
)

// TestSetCodeTxRequiresAuthorityAssociation verifies the Giga-path guard for the EIP-7702
// root-cause fix: a SetCode transaction whose authorization authority is not yet associated
// with its true Sei address must be deferred to V2 (where the ante handler pre-associates
// it), while an already-associated authority or a non-SetCode transaction need not.
func TestSetCodeTxRequiresAuthorityAssociation(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := a.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: time.Now()})
	chainID := a.EvmKeeper.ChainID(ctx)

	victimKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	victimEVM := crypto.PubkeyToAddress(victimKey.PublicKey)
	_, victimTrueSei, _, err := helpers.GetAddressesFromPubkeyBytes(crypto.FromECDSAPub(&victimKey.PublicKey))
	require.NoError(t, err)

	auth, err := ethtypes.SignSetCode(victimKey, ethtypes.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: common.HexToAddress("0x000000000000000000000000000000000000c0de"),
		Nonce:   0,
	})
	require.NoError(t, err)

	sponsorKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	to := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	setCodeTx, err := ethtypes.SignNewTx(sponsorKey, ethtypes.NewPragueSigner(chainID), &ethtypes.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     0,
		GasTipCap: uint256.NewInt(1),
		GasFeeCap: uint256.NewInt(1),
		Gas:       100000,
		To:        to,
		Value:     uint256.NewInt(0),
		AuthList:  []ethtypes.SetCodeAuthorization{auth},
	})
	require.NoError(t, err)

	// Unassociated authority => giga must defer to V2 so the ante can pre-associate it.
	require.True(t, a.setCodeTxRequiresAuthorityAssociation(ctx, setCodeTx))

	// Once the authority is associated to its true Sei address, giga can execute directly
	// (SetCode will see the association and skip the direct-cast mapping).
	a.GigaEvmKeeper.SetAddressMapping(ctx, victimTrueSei, victimEVM)
	require.False(t, a.setCodeTxRequiresAuthorityAssociation(ctx, setCodeTx))

	// A SetCode tx carrying an unassociated authority signed for a FOREIGN chain does not
	// require deferral: the EVM skips the wrong-chain authorization, so no association (and
	// no direct-cast mapping) is ever created for it.
	foreignVictimKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	foreignAuth, err := ethtypes.SignSetCode(foreignVictimKey, ethtypes.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(new(big.Int).Add(chainID, big.NewInt(1))),
		Address: common.HexToAddress("0x000000000000000000000000000000000000c0de"),
		Nonce:   0,
	})
	require.NoError(t, err)
	foreignTx, err := ethtypes.SignNewTx(sponsorKey, ethtypes.NewPragueSigner(chainID), &ethtypes.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     1,
		GasTipCap: uint256.NewInt(1),
		GasFeeCap: uint256.NewInt(1),
		Gas:       100000,
		To:        to,
		Value:     uint256.NewInt(0),
		AuthList:  []ethtypes.SetCodeAuthorization{foreignAuth},
	})
	require.NoError(t, err)
	require.False(t, a.setCodeTxRequiresAuthorityAssociation(ctx, foreignTx))

	// A non-SetCode transaction carries no authorizations and never needs deferral.
	legacyTx, err := ethtypes.SignNewTx(sponsorKey, ethtypes.NewPragueSigner(chainID), &ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
	})
	require.NoError(t, err)
	require.False(t, a.setCodeTxRequiresAuthorityAssociation(ctx, legacyTx))
}
