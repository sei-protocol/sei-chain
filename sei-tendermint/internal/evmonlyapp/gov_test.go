package evmonlyapp

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// anvilKey0 is anvil account 0 of the test mnemonic.
const anvilKey0 = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func newGovEVMOnlyTestApp(t *testing.T, params gov.Params, startingProposalID uint64) abci.Application {
	t.Helper()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	registry := NewGovRegistry(params, startingProposalID, gov.GenesisElectorate{common.HexToAddress("0xa1"): big.NewInt(1)})
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID,
		nil,
		storage,
		evmonly.NewFlatKVChangeSetEncoder(),
		gigaconfig.DefaultConfig.Execution,
		utils.Some(registry),
	)
	require.NoError(t, err)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	return app
}

// govCall runs a read-only gov call against the app's committed state.
func govCall(t *testing.T, app abci.Application, method string, args ...interface{}) ([]interface{}, error) {
	t.Helper()
	return precompileCall(t, app, gov.Address, gov.ABI, method, args...)
}

func precompileCall(t *testing.T, app abci.Application, to common.Address, contract abi.ABI, method string, args ...interface{}) ([]interface{}, error) {
	t.Helper()
	input, err := contract.Pack(method, args...)
	require.NoError(t, err)
	msg := callMessage(common.Address{}, &to)
	msg.Data = input
	msg.GasLimit = 1_000_000
	result, err := app.(*evmOnlyApplication).EvmCall(t.Context(), msg)
	require.NoError(t, err)
	if result.Err != nil {
		return nil, result.Err
	}
	return contract.Methods[method].Outputs.Unpack(result.ReturnData)
}

func signedGovTx(t *testing.T, nonce uint64, value *big.Int, method string, args ...interface{}) []byte {
	t.Helper()
	key, err := crypto.HexToECDSA(anvilKey0)
	require.NoError(t, err)
	input, err := gov.ABI.Pack(method, args...)
	require.NoError(t, err)
	to := gov.Address
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(evmOnlyMinGasPrice),
		Gas:      2_000_000,
		To:       &to,
		Value:    value,
		Data:     input,
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(evmOnlyTestChainID)), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw
}

func TestEVMOnlyApplicationStartsGovAtTheFirstBlock(t *testing.T) {
	params := gov.DefaultParams()
	params.VotingPeriod = uint64(40 * time.Second)
	params.ExpeditedVotingPeriod = uint64(20 * time.Second)
	app := newGovEVMOnlyTestApp(t, params, 500)

	_, err := govCall(t, app, gov.ParamsMethod)
	require.Error(t, err, "gov refuses calls before the first block writes its params")
	_, err = precompileCall(t, app, chainparams.Address, chainparams.ABI, chainparams.ParamMethod, "gov", "votingparams.voting_period")
	require.Error(t, err, "params refuses calls before the first block writes them")

	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1))
	_, err = precompileCall(t, app, chainparams.Address, chainparams.ABI, chainparams.ParamMethod, "gov", "votingparams.voting_period")
	require.NoError(t, err)
	out, err := govCall(t, app, gov.ParamsMethod)
	require.NoError(t, err)
	var got gov.GovParams
	abi.ConvertType(out[0], &got)
	require.Equal(t, uint64(40*time.Second), got.VotingPeriod)

	submit := signedGovTx(t, 0, nil, gov.SubmitTextProposalMethod, "t", "d", false)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2, submit))
	out, err = govCall(t, app, gov.ProposalMethod, uint64(500))
	require.NoError(t, err)
	var prop gov.ProposalData
	abi.ConvertType(out[0], &prop)
	require.Equal(t, uint64(500), prop.Id, "the first proposal takes the starting id")
}
