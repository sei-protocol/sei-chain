package evmonlyapp

import (
	"crypto/ecdsa"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func signedEVMOnlyGovTx(t *testing.T, key *ecdsa.PrivateKey, nonce uint64, method string, args ...any) []byte {
	t.Helper()
	data, err := gov.ABI.Pack(method, args...)
	require.NoError(t, err)
	to := gov.Address
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(evmOnlyMinGasPrice),
		Gas:      2_000_000,
		To:       &to,
		Data:     data,
	}), ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(evmOnlyTestChainID)), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw
}

func evmOnlyProposalStatus(t *testing.T, app abci.Application, id uint64) int32 {
	t.Helper()
	data, err := gov.ABI.Pack("proposal", id)
	require.NoError(t, err)
	to := gov.Address
	result, err := app.(*evmOnlyApplication).EvmCall(t.Context(), &ethcore.Message{
		To:               &to,
		GasLimit:         10_000_000,
		GasPrice:         new(big.Int),
		GasFeeCap:        new(big.Int),
		GasTipCap:        new(big.Int),
		Value:            new(big.Int),
		Data:             data,
		SkipNonceChecks:  true,
		SkipFromEOACheck: true,
	})
	require.NoError(t, err)
	require.NoError(t, result.Err)
	values, err := gov.ABI.Methods["proposal"].Outputs.Unpack(result.ReturnData)
	require.NoError(t, err)
	encoded, err := json.Marshal(values[0])
	require.NoError(t, err)
	var proposal struct{ Status int32 }
	require.NoError(t, json.Unmarshal(encoded, &proposal))
	return proposal.Status
}

type evmOnlyStorageReader struct{ view gigatypes.StateView }

func (r evmOnlyStorageReader) GetState(addr common.Address, key common.Hash) common.Hash {
	return r.view.GetStorage(addr, key)
}

// TestEVMOnlyApplicationRunsGovernance drives an upgrade proposal through
// FinalizeBlock and requires the passed plan to survive a restart.
func TestEVMOnlyApplicationRunsGovernance(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	params := gov.DefaultParams()
	params.VotingPeriod = 2
	contract, err := gov.New(gov.Genesis{
		Voters: []gov.Voter{{Address: crypto.PubkeyToAddress(key.PublicKey), Weight: 1}},
		Params: params,
	})
	require.NoError(t, err)
	registry := utils.Some[precompiles.Registry](contract.Registry())

	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()), gigaconfig.DefaultConfig.Execution, registry)
	require.NoError(t, err)
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)

	proposal := `{"title":"t","description":"d","type":"SoftwareUpgrade","plan":{"name":"0123456789abcdef0123456789abcdef01234567","height":50}}`
	response, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(1,
		signedEVMOnlyGovTx(t, key, 0, "submitProposal", proposal),
		signedEVMOnlyGovTx(t, key, 1, "vote", uint64(1), gov.OptionYes),
	))
	require.NoError(t, err)
	for _, result := range response.TxResults {
		require.Equal(t, uint32(0), result.Code)
	}
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
	require.Equal(t, gov.StatusVotingPeriod, evmOnlyProposalStatus(t, app, 1))

	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2))
	require.Equal(t, gov.StatusVotingPeriod, evmOnlyProposalStatus(t, app, 1))
	// evmOnlyTestBlock spaces blocks one second apart, so block 3 ends the period.
	passedHash := finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(3))
	require.Equal(t, gov.StatusPassed, evmOnlyProposalStatus(t, app, 1))

	closeEVMOnlyTestApp(t, app, storage)
	storage = openEVMOnlyTestStorageAt(t, home)
	app, err = NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()), gigaconfig.DefaultConfig.Execution, registry)
	require.NoError(t, err)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })
	require.Equal(t, passedHash, app.Info().LastBlockAppHash)
	require.Equal(t, gov.StatusPassed, evmOnlyProposalStatus(t, app, 1))
	view := app.(*evmOnlyApplication).openSettledView()
	defer view.Close()
	plan, ok := gov.ReadPlan(evmOnlyStorageReader{view})
	require.True(t, ok)
	require.Equal(t, gov.Plan{Name: "0123456789abcdef0123456789abcdef01234567", Height: 50, Proposal: 1}, plan)
}
