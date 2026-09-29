package node

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

var (
	oldBinary    = strings.Repeat("0a", 20)
	targetBinary = strings.Repeat("1b", 20)
)

func TestReportUpgradeNeededWritesUpgradeInfo(t *testing.T) {
	root := t.TempDir()
	plan := gov.Plan{Name: targetBinary, Height: 42, Info: `{"binaries":{}}`, Proposal: 3}
	stopped := fmt.Errorf("giga: %w", fmt.Errorf("app.FinalizeBlock(): %w", &gov.UpgradeNeededError{Plan: plan}))

	err := reportUpgradeNeeded(root, stopped)
	require.ErrorIs(t, err, stopped)
	needed, ok := errors.AsType[*gov.UpgradeNeededError](err)
	require.True(t, ok)
	require.Equal(t, plan, needed.Plan)

	bz, err := os.ReadFile(filepath.Join(root, "data", "upgrade-info.json"))
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"`+targetBinary+`","height":42,"info":"{\"binaries\":{}}"}`, string(bz))
}

func TestReportUpgradeNeededPassesOtherErrorsThrough(t *testing.T) {
	root := t.TempDir()
	other := errors.New("consensus failure")
	require.Equal(t, other, reportUpgradeNeeded(root, other))
	require.NoError(t, reportUpgradeNeeded(root, nil))
	_, err := os.Stat(filepath.Join(root, "data"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReportUpgradeNeededReportsAnUnwritableDataDirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "data"), nil, 0o600))
	stopped := &gov.UpgradeNeededError{Plan: gov.Plan{Name: targetBinary, Height: 42}}
	err := reportUpgradeNeeded(root, stopped)
	require.ErrorIs(t, err, stopped)
	require.NotEqual(t, error(stopped), err)
}

// upgradeTestNode is an Autobahn node's application over one home, reopened as
// a restarted process would be.
type upgradeTestNode struct {
	conf  *config.Config
	key   *ecdsa.PrivateKey
	nonce uint64
}

func newUpgradeTestNode(t *testing.T) *upgradeTestNode {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	fc := defaultFileConfig(t, []config.AutobahnValidator{makeValidator([]byte("upgrade-validator"), []byte("upgrade-node"), "localhost:26660")})
	fc.Validators[0].EVMVoter = utils.Some(crypto.PubkeyToAddress(key.PublicKey))
	params := gov.DefaultParams()
	params.VotingPeriod = 2
	fc.EVMGovernance = utils.Some(params)
	return &upgradeTestNode{
		conf: &config.Config{
			BaseConfig:         config.BaseConfig{RootDir: t.TempDir(), FastCheckTx: true},
			AutobahnConfigFile: writeAutobahnConfig(t, fc),
		},
		key: key,
	}
}

func (n *upgradeTestNode) open(t *testing.T, upgrades gov.Upgrades) (abci.Application, func(), error) {
	app, storage, err := prepareApplication(t.Context(), n.conf, abci.BaseApplication{}, gigaconfig.DefaultConfig, upgrades)
	if err != nil {
		return nil, nil, err
	}
	manager, ok := storage.Get()
	require.True(t, ok)
	return app, func() {
		settler, ok := app.(interface{ AwaitCommits() error })
		require.True(t, ok)
		require.NoError(t, settler.AwaitCommits())
		require.NoError(t, manager.Close())
	}, nil
}

func (n *upgradeTestNode) govTx(t *testing.T, method string, args ...any) []byte {
	data, err := gov.ABI.Pack(method, args...)
	require.NoError(t, err)
	to := gov.Address
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    n.nonce,
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      2_000_000,
		To:       &to,
		Data:     data,
	}), ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(config.AutobahnEVMOnlyChainID)), n.key)
	require.NoError(t, err)
	n.nonce++
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw
}

func upgradeTestBlock(height int64, txs ...[]byte) *abci.RequestFinalizeBlock {
	return &abci.RequestFinalizeBlock{
		Txs:    txs,
		Hash:   crypto.Keccak256([]byte(fmt.Sprintf("block-%d", height))),
		Header: &tmproto.Header{Height: height, Time: time.Unix(1_700_000_000+height, 0)},
	}
}

func finalizeAndCommit(t *testing.T, app abci.Application, req *abci.RequestFinalizeBlock) {
	resp, err := app.FinalizeBlock(t.Context(), req)
	require.NoError(t, err)
	for _, result := range resp.TxResults {
		require.Equal(t, uint32(0), result.Code)
		require.Empty(t, result.Log)
	}
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
}

// TestAutobahnNodeUpgradesAtThePlanHeight runs an old binary up to a scheduled
// upgrade, refuses to start the target binary early, and resumes on it at the
// plan height.
func TestAutobahnNodeUpgradesAtThePlanHeight(t *testing.T) {
	const planHeight = 6
	n := newUpgradeTestNode(t)
	app, closeApp, err := n.open(t, gov.Upgrades{Name: oldBinary})
	require.NoError(t, err)
	_, err = app.InitChain(&abci.RequestInitChain{
		InitialHeight:   1,
		ConsensusParams: &tmproto.ConsensusParams{Block: &tmproto.BlockParams{MaxGas: 30_000_000}},
	})
	require.NoError(t, err)
	proposal := fmt.Sprintf(`{"title":"t","description":"d","type":"SoftwareUpgrade","plan":{"name":%q,"height":%d,"info":"i"}}`, targetBinary, planHeight)
	finalizeAndCommit(t, app, upgradeTestBlock(1, n.govTx(t, "submitProposal", proposal), n.govTx(t, "vote", uint64(1), gov.OptionYes)))
	finalizeAndCommit(t, app, upgradeTestBlock(2))
	finalizeAndCommit(t, app, upgradeTestBlock(3))
	closeApp()

	// The target binary may not start while the chain is below the plan height.
	_, _, err = n.open(t, gov.Upgrades{Name: targetBinary})
	require.ErrorIs(t, err, gov.ErrUpgradeBeforeTrigger)
	_, _, err = n.open(t, gov.Upgrades{Name: targetBinary, SkipHeights: []uint64{planHeight}})
	require.ErrorIs(t, err, gov.ErrUpgradeBeforeTrigger)

	app, closeApp, err = n.open(t, gov.Upgrades{Name: oldBinary})
	require.NoError(t, err)
	for height := int64(4); height < planHeight; height++ {
		finalizeAndCommit(t, app, upgradeTestBlock(height))
	}
	_, err = app.FinalizeBlock(t.Context(), upgradeTestBlock(planHeight))
	needed, ok := errors.AsType[*gov.UpgradeNeededError](err)
	require.True(t, ok, "want an UpgradeNeededError, got %v", err)
	require.Equal(t, gov.Plan{Name: targetBinary, Height: planHeight, Info: "i", Proposal: 1}, needed.Plan)
	_, err = app.Commit(t.Context())
	require.Error(t, err, "the refused block must leave nothing to commit")
	require.Equal(t, int64(planHeight-1), app.Info().LastBlockHeight)
	closeApp()

	// A restarted old binary stops again at the same height.
	app, closeApp, err = n.open(t, gov.Upgrades{Name: oldBinary})
	require.NoError(t, err)
	require.Equal(t, int64(planHeight-1), app.Info().LastBlockHeight)
	_, err = app.FinalizeBlock(t.Context(), upgradeTestBlock(planHeight))
	_, ok = errors.AsType[*gov.UpgradeNeededError](err)
	require.True(t, ok, "want an UpgradeNeededError, got %v", err)
	closeApp()

	// The target binary starts at the plan height and continues past it.
	app, closeApp, err = n.open(t, gov.Upgrades{Name: targetBinary})
	require.NoError(t, err)
	defer closeApp()
	for height := int64(planHeight); height <= planHeight+2; height++ {
		finalizeAndCommit(t, app, upgradeTestBlock(height))
	}
	require.Equal(t, int64(planHeight+2), app.Info().LastBlockHeight)
}

// TestAutobahnNodeWithoutGovernanceIgnoresUpgrades starts a node with no
// evm_governance under any binary name.
func TestAutobahnNodeWithoutGovernanceIgnoresUpgrades(t *testing.T) {
	fc := defaultFileConfig(t, []config.AutobahnValidator{makeValidator([]byte("plain-validator"), []byte("plain-node"), "localhost:26660")})
	_, storage, err := prepareApplication(t.Context(), &config.Config{
		BaseConfig:         config.BaseConfig{RootDir: t.TempDir(), FastCheckTx: true},
		AutobahnConfigFile: writeAutobahnConfig(t, fc),
	}, abci.BaseApplication{}, gigaconfig.DefaultConfig, gov.Upgrades{Name: targetBinary, SkipHeights: []uint64{1}})
	require.NoError(t, err)
	manager, ok := storage.Get()
	require.True(t, ok)
	require.NoError(t, manager.Close())
}
