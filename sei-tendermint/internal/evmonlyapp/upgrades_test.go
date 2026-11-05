package evmonlyapp

import (
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/app/upgradenames"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// testAppliedUpgrades are the upgrade names test chains start past.
var testAppliedUpgrades = upgradenames.Release()

func TestEVMOnlyApplicationRecordsAppliedUpgradesAtTheFirstBlock(t *testing.T) {
	app := newGovEVMOnlyTestApp(t, gov.DefaultParams(), 0)
	out, err := precompileCall(t, app, upgrade.Address, upgrade.ABI, upgrade.AppliedPlanMethod, upgradenames.Latest())
	require.NoError(t, err)
	require.Equal(t, uint64(0), out[0].(uint64))

	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1))
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2))
	for _, name := range testAppliedUpgrades {
		out, err := precompileCall(t, app, upgrade.Address, upgrade.ABI, upgrade.AppliedPlanMethod, name)
		require.NoError(t, err)
		require.Equal(t, uint64(1), out[0].(uint64), name)
	}
	out, err = precompileCall(t, app, upgrade.Address, upgrade.ABI, upgrade.CurrentPlanMethod)
	require.NoError(t, err)
	require.Zero(t, out[0].(struct {
		Name   string `json:"name"`
		Height uint64 `json:"height"`
		Info   string `json:"info"`
	}).Height)
}

func TestUpgradeHandlersCoverEveryReleaseName(t *testing.T) {
	handlers := upgradeHandlers()
	want := len(upgradenames.Release())
	if testUpgradeHandler {
		want++
	}
	require.Equal(t, want, len(handlers))
	for _, name := range upgradenames.Release() {
		_, ok := handlers[name]
		require.True(t, ok, name)
	}
	for name := range releaseMigrations {
		require.True(t, slices.Contains(upgradenames.Release(), name), "migration %q is for a release name", name)
	}
}

func TestReleaseNamesFollowTheUpgradeNameRule(t *testing.T) {
	// Genesis records it as applied, so gov never schedules it.
	const exempt = "1.0.5beta upgrade"
	for _, name := range upgradenames.Release() {
		if name != exempt {
			require.NoError(t, upgrade.ValidateName(name), name)
		}
	}
	require.True(t, slices.Contains(upgradenames.Release(), exempt))
	require.Error(t, upgrade.ValidateName(exempt))
}

// upgradeTestNode is an app over storage at home whose gov electorate is
// anvil account 0. A halt writes upgrade-info.json under home.
type upgradeTestNode struct {
	app     proxy.EVMApplication
	storage *bootstrap.GigaStorageManager
}

func (n upgradeTestNode) close(t *testing.T) {
	t.Helper()
	closeEVMOnlyTestApp(t, n.app, n.storage)
}

// upgradeTestApp starts an upgradeTestNode with handlers for the release
// names, and for testUpgradeName when withTestUpgrade is set, whatever the
// build's tags.
func upgradeTestApp(t *testing.T, home string, withTestUpgrade bool) upgradeTestNode {
	t.Helper()
	handlers := upgradeHandlers()
	delete(handlers, testUpgradeName)
	if withTestUpgrade {
		handlers[testUpgradeName] = applyTestUpgrade
	}
	params := gov.DefaultParams()
	params.VotingPeriod = uint64(4 * time.Second)
	params.ExpeditedVotingPeriod = uint64(2 * time.Second)
	registry := newGovRegistry(params, 0, gov.GenesisElectorate{anvilAddress(t): big.NewInt(1)}, GovUpgrades{Applied: testAppliedUpgrades}, handlers)
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, gigaconfig.DefaultConfig.Execution, utils.Some(registry), utils.Some(home))
	require.NoError(t, err)
	return upgradeTestNode{app: app, storage: storage}
}

// maxDepositPeriod returns gov's max deposit period as the params precompile
// serves it.
func maxDepositPeriod(t *testing.T, app proxy.EVMApplication) uint64 {
	t.Helper()
	out, err := precompileCall(t, app, chainparams.Address, chainparams.ABI, chainparams.ParamMethod, "gov", "depositparams.max_deposit_period")
	require.NoError(t, err)
	return new(big.Int).SetBytes(out[0].([]byte)).Uint64()
}

func anvilAddress(t *testing.T) common.Address {
	t.Helper()
	key, err := crypto.HexToECDSA(anvilKey0)
	require.NoError(t, err)
	return crypto.PubkeyToAddress(key.PublicKey)
}

// passUpgrade passes an expedited upgrade proposal for plan, starting at
// block next, and returns the next block to finalize.
func passUpgrade(t *testing.T, app abci.Application, next int64, plan upgrade.Plan) int64 {
	t.Helper()
	deposit := gov.DefaultParams().MinExpeditedDeposit
	submit := signedGovTx(t, 0, deposit, gov.SubmitSoftwareUpgradeMethod, "t", "d", true,
		struct {
			Name   string
			Height uint64
			Info   string
		}{plan.Name, plan.Height, plan.Info})
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(next, submit))
	vote := signedGovTx(t, 1, nil, gov.VoteMethod, uint64(1), gov.OptionYes)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(next+1, vote))
	for n := next + 2; n < next+5; n++ {
		finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(n))
	}
	out, err := precompileCall(t, app, upgrade.Address, upgrade.ABI, upgrade.CurrentPlanMethod)
	require.NoError(t, err)
	var got struct {
		Name   string
		Height uint64
		Info   string
	}
	abi.ConvertType(out[0], &got)
	require.Equal(t, plan.Name, got.Name, "the proposal passed and scheduled its plan")
	return next + 5
}

func TestEVMOnlyApplicationHaltsAtAnUpgradeAndResumesWithItsHandler(t *testing.T) {
	home := t.TempDir()
	node := upgradeTestApp(t, home, false)
	_, err := node.app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	finalizeAndCommitEVMOnlyTestBlock(t, node.app, evmOnlyTestBlock(1))
	next := passUpgrade(t, node.app, 2, upgrade.Plan{Name: testUpgradeName, Height: 9, Info: `{"binaries":{}}`})
	require.Equal(t, gov.DefaultParams().MaxDepositPeriod, maxDepositPeriod(t, node.app))
	for ; next < 9; next++ {
		finalizeAndCommitEVMOnlyTestBlock(t, node.app, evmOnlyTestBlock(next))
	}

	_, err = node.app.FinalizeBlock(t.Context(), evmOnlyTestBlock(9))
	var needed *upgrade.NeededError
	require.True(t, errors.As(err, &needed), "the block halts on the plan: %v", err)
	require.Equal(t, uint64(9), needed.Block)
	bz, err := os.ReadFile(filepath.Join(home, "data", UpgradeInfoFileName))
	require.NoError(t, err)
	var info upgradeInfo
	require.NoError(t, json.Unmarshal(bz, &info))
	require.Equal(t, upgradeInfo{Name: testUpgradeName, Height: 9, Info: `{"binaries":{}}`}, info)

	node.close(t)
	resumed := upgradeTestApp(t, home, true)
	t.Cleanup(func() { resumed.close(t) })
	finalizeAndCommitEVMOnlyTestBlock(t, resumed.app, evmOnlyTestBlock(9))
	out, err := precompileCall(t, resumed.app, upgrade.Address, upgrade.ABI, upgrade.AppliedPlanMethod, testUpgradeName)
	require.NoError(t, err)
	require.Equal(t, uint64(9), out[0].(uint64), "the resumed binary applied the plan at the block the old one halted at")
	require.Equal(t, uint64(testUpgradeMaxDepositPeriod), maxDepositPeriod(t, resumed.app), "the handler's change is in state")
	finalizeAndCommitEVMOnlyTestBlock(t, resumed.app, evmOnlyTestBlock(10))
}

func TestEVMOnlyApplicationHaltsAnOldBinaryAfterAnUpgrade(t *testing.T) {
	home := t.TempDir()
	node := upgradeTestApp(t, home, true)
	_, err := node.app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	finalizeAndCommitEVMOnlyTestBlock(t, node.app, evmOnlyTestBlock(1))
	next := passUpgrade(t, node.app, 2, upgrade.Plan{Name: testUpgradeName, Height: 8, Info: `{"upgradeType":"minor"}`})
	for ; next <= 8; next++ {
		finalizeAndCommitEVMOnlyTestBlock(t, node.app, evmOnlyTestBlock(next))
	}

	node.close(t)
	old := upgradeTestApp(t, home, false)
	t.Cleanup(func() { old.close(t) })
	_, err = old.app.FinalizeBlock(t.Context(), evmOnlyTestBlock(9))
	require.ErrorIs(t, err, upgrade.ErrWrongBinary)
}

func TestEVMOnlyApplicationServesTheProxysOptionalMethods(t *testing.T) {
	node := upgradeTestApp(t, t.TempDir(), false)
	defer node.close(t)
	_, ok := node.app.(interface{ AwaitCommits() error })
	require.True(t, ok, "AwaitCommits")
	_, ok = node.app.(interface {
		EvmCode(common.Address) []byte
		EvmStorage(common.Address, common.Hash) common.Hash
	})
	require.True(t, ok, "EvmCode and EvmStorage")
	_, ok = node.app.(interface {
		EvmBalanceAt(common.Address, int64) (uint256.Int, error)
		EvmCodeAt(common.Address, int64) ([]byte, error)
		EvmNonceAt(common.Address, int64) (uint64, error)
		EvmStorageAt(common.Address, common.Hash, int64) (common.Hash, error)
	})
	require.True(t, ok, "the Evm*At history readers")
}

func TestHaltForUpgradeReportsAFailedWrite(t *testing.T) {
	home := t.TempDir()
	// A file where the data directory belongs makes the write fail.
	require.NoError(t, os.WriteFile(filepath.Join(home, "data"), nil, 0o600))
	app := &evmOnlyApplication{upgradeInfoPath: utils.Some(upgradeInfoPath(home))}
	needed := &upgrade.NeededError{Plan: upgrade.Plan{Name: "v7.0", Height: 9}, Block: 9}

	err := app.haltForUpgrade(needed, needed)
	require.ErrorIs(t, err, needed)
	require.Contains(t, err.Error(), UpgradeInfoFileName)
}

func TestEVMOnlyApplicationTracesABlockBeforeAPlansHeight(t *testing.T) {
	home := t.TempDir()
	node := upgradeTestApp(t, home, false)
	_, err := node.app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	finalizeAndCommitEVMOnlyTestBlock(t, node.app, evmOnlyTestBlock(1))
	next := passUpgrade(t, node.app, 2, upgrade.Plan{Name: testUpgradeName, Height: 20})
	for ; next < 12; next++ {
		finalizeAndCommitEVMOnlyTestBlock(t, node.app, evmOnlyTestBlock(next))
	}
	node.close(t)

	// The binary with the plan's handler halts a live block before the plan's
	// height, but rebuilds the same block for a trace.
	upgraded := upgradeTestApp(t, home, true)
	t.Cleanup(func() { upgraded.close(t) })
	_, release, err := upgraded.app.EvmStateBeforeTx(t.Context(), traceTestHeader(12, evmOnlyTestBlock(11).Hash), nil)
	require.NoError(t, err)
	release()
	_, err = upgraded.app.FinalizeBlock(t.Context(), evmOnlyTestBlock(12))
	require.ErrorIs(t, err, upgrade.ErrBinaryUpdatedBeforeTrigger)
}
