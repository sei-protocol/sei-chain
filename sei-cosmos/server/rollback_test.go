package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gogo/protobuf/grpc"
	"github.com/golang/protobuf/proto"
	"github.com/google/orderedcode"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/api"
	serverconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/rootmulti"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmcmd "github.com/sei-protocol/sei-chain/sei-tendermint/cmd/tendermint/commands"
	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/tmhash"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	tmstate "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/state"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	tmversion "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/version"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

// mockApplication is a mock implementation of types.Application for testing
type mockApplication struct {
	cms sdk.CommitMultiStore
}

func (m *mockApplication) CommitMultiStore() sdk.CommitMultiStore {
	return m.cms
}

func (m *mockApplication) Close() error {
	return m.cms.Close()
}

// Implement other required methods with no-ops
func (m *mockApplication) Info(ctx context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	return &abci.ResponseInfo{}, nil
}

func (m *mockApplication) InitChain(ctx context.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	return &abci.ResponseInitChain{}, nil
}

func (m *mockApplication) Query(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
	return &abci.ResponseQuery{}, nil
}

func (m *mockApplication) CheckTx(ctx context.Context, req *abci.RequestCheckTxV2) (*abci.ResponseCheckTxV2, error) {
	return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Code: abci.CodeTypeOK}}, nil
}

func (m *mockApplication) GetTxPriorityHint(ctx context.Context, req *abci.RequestGetTxPriorityHintV2) (*abci.ResponseGetTxPriorityHint, error) {
	return &abci.ResponseGetTxPriorityHint{}, nil
}

func (m *mockApplication) BeginBlock(ctx context.Context, req *abci.RequestBeginBlock) (*abci.ResponseBeginBlock, error) {
	return &abci.ResponseBeginBlock{}, nil
}

func (m *mockApplication) Commit(ctx context.Context) (*abci.ResponseCommit, error) {
	return &abci.ResponseCommit{}, nil
}

func (m *mockApplication) ListSnapshots(ctx context.Context, req *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
	return &abci.ResponseListSnapshots{}, nil
}

func (m *mockApplication) OfferSnapshot(ctx context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	return &abci.ResponseOfferSnapshot{}, nil
}

func (m *mockApplication) LoadSnapshotChunk(ctx context.Context, req *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
	return &abci.ResponseLoadSnapshotChunk{}, nil
}

func (m *mockApplication) ApplySnapshotChunk(ctx context.Context, req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	return &abci.ResponseApplySnapshotChunk{}, nil
}

func (m *mockApplication) PrepareProposal(ctx context.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	return &abci.ResponsePrepareProposal{}, nil
}

func (m *mockApplication) ProcessProposal(ctx context.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	return &abci.ResponseProcessProposal{}, nil
}

func (m *mockApplication) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return &abci.ResponseFinalizeBlock{}, nil
}

func (m *mockApplication) RegisterAPIRoutes(*api.Server, serverconfig.APIConfig) {}
func (m *mockApplication) RegisterGRPCServer(grpc.Server)                        {}
func (m *mockApplication) RegisterTxService(client.Context)                      {}
func (m *mockApplication) RegisterTendermintService(client.Context)              {}
func (m *mockApplication) InplaceTestnetInitialize(cryptotypes.PubKey)           {}

// setupTestApp creates a test application with a CommitMultiStore at a specific height
func setupTestApp(t *testing.T, height int64) (*mockApplication, string) {
	tempDir := t.TempDir()
	db, err := dbm.NewDB("test", dbm.MemDBBackend, tempDir)
	require.NoError(t, err)

	cms := rootmulti.NewStore(db, log.NewNopLogger())
	key := storetypes.NewKVStoreKey("test")
	cms.MountStoreWithDB(key, storetypes.StoreTypeIAVL, db)
	err = cms.LoadLatestVersion()
	require.NoError(t, err)

	// Commit to the desired height
	// Use bumpVersion=true to increment version on each commit
	for i := int64(1); i <= height; i++ {
		store := cms.GetKVStore(key)
		store.Set([]byte(fmt.Sprintf("height-%d", i)), []byte(fmt.Sprintf("value-%d", i)))
		cms.Commit(true)
	}

	// Verify we're at the correct height
	commitID := cms.LastCommitID()
	require.Equal(t, height, commitID.GetVersion())

	return &mockApplication{cms: cms}, tempDir
}

// setupTendermintStateDB creates a mock tendermint state database with a specific height
func setupTendermintStateDB(t *testing.T, tempDir string, height int64) *tmconfig.Config {
	cfg := tmconfig.TestConfig()
	cfg.SetRoot(tempDir)
	cfg.BaseConfig.DBBackend = "goleveldb"

	// Create state database
	stateDB, err := dbm.NewDB("state", dbm.BackendType(cfg.DBBackend), cfg.DBDir())
	require.NoError(t, err)
	defer stateDB.Close()

	// Create a simple validator set for testing
	// We need at least one validator for rollback to work
	valPrivKey := ed25519.GenerateSecretKey()
	valPubKey := valPrivKey.Public()
	validator := &tmtypes.Validator{
		Address:          valPubKey.Address(),
		PubKey:           valPubKey,
		VotingPower:      100,
		ProposerPriority: 0,
	}
	valSet := tmtypes.NewValidatorSet([]*tmtypes.Validator{validator})

	// Convert validator set to proto
	valSetProto, err := valSet.ToProto()
	require.NoError(t, err)

	// Create validators info for multiple heights (needed for rollback)
	batch := stateDB.NewBatch()
	defer batch.Close()

	// Save validators for height, height-1, and height+1 (needed for rollback logic)
	for h := int64(1); h <= height+1; h++ {
		valInfo := &tmstate.ValidatorsInfo{
			LastHeightChanged: 1,
			ValidatorSet:      valSetProto,
		}
		valInfoBytes, err := valInfo.Marshal()
		require.NoError(t, err)

		// Create validators key: prefixValidators (5) + height
		validatorsKeyBytes, err := orderedcode.Append(nil, int64(5), h)
		require.NoError(t, err)
		err = batch.Set(validatorsKeyBytes, valInfoBytes)
		require.NoError(t, err)
	}

	// Save consensus params
	consensusParams := tmtypes.DefaultConsensusParams()
	consensusParamsProto := consensusParams.ToProto()
	consensusParamsInfo := &tmstate.ConsensusParamsInfo{
		LastHeightChanged: 1,
		ConsensusParams:   consensusParamsProto,
	}
	consensusParamsBytes, err := consensusParamsInfo.Marshal()
	require.NoError(t, err)

	// Create consensus params key: prefixConsensusParams (6) + height
	for h := int64(1); h <= height+1; h++ {
		consensusParamsKeyBytes, err := orderedcode.Append(nil, int64(6), h)
		require.NoError(t, err)
		err = batch.Set(consensusParamsKeyBytes, consensusParamsBytes)
		require.NoError(t, err)
	}

	// Create state with validators
	tmStateProto := &tmstate.State{
		Version: tmstate.Version{
			Consensus: tmversion.Consensus{
				Block: 1,
				App:   1,
			},
			Software: "test",
		},
		ChainID:                          "test-chain",
		InitialHeight:                    1,
		LastBlockHeight:                  height,
		LastBlockID:                      tmproto.BlockID{},
		LastBlockTime:                    time.Now(),
		Validators:                       valSetProto,
		NextValidators:                   valSetProto,
		LastValidators:                   valSetProto,
		LastHeightValidatorsChanged:      1,
		ConsensusParams:                  consensusParamsProto,
		LastHeightConsensusParamsChanged: 1,
		AppHash:                          []byte("app-hash"),
		LastResultsHash:                  []byte("results-hash"),
	}

	// Marshal and save state
	stateBytes, err := proto.Marshal(tmStateProto)
	require.NoError(t, err)

	// Create state key: prefixState (8)
	stateKey, err := orderedcode.Append(nil, int64(8))
	require.NoError(t, err)
	err = batch.Set(stateKey, stateBytes)
	require.NoError(t, err)

	// Write batch
	err = batch.WriteSync()
	require.NoError(t, err)

	// Also need to create blockstore with actual blocks for tendermint rollback to work
	blockStoreDB, err := dbm.NewDB("blockstore", dbm.BackendType(cfg.DBBackend), cfg.DBDir())
	require.NoError(t, err)
	defer blockStoreDB.Close()

	// Create blocks in the blockstore using the store package
	// We need to import the store package, but it's internal, so we'll create blocks manually
	// For now, let's use a simpler approach: create minimal block metadata
	// The rollback function needs blocks at height and height-1
	setupBlockstore(t, blockStoreDB, height, valSet)

	// Create private validator files (needed when removeBlock=true)
	setupPrivateValidator(t, cfg)

	return cfg
}

// setupPrivateValidator creates private validator files needed for rollback with removeBlock=true
func setupPrivateValidator(t *testing.T, cfg *tmconfig.Config) {
	// Create config directory if it doesn't exist
	configDir := filepath.Join(cfg.RootDir, "config")
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Generate private validator files
	keyFile := cfg.PrivValidator.KeyFile()
	stateFile := cfg.PrivValidator.StateFile()

	// Use GenFilePV to create the validator files
	privVal, err := privval.GenFilePV(keyFile, stateFile, "")
	require.NoError(t, err)

	// Set the last sign state to a reasonable height (will be updated during rollback)
	privVal.LastSignState.Height = 1
	privVal.LastSignState.Round = 0
	privVal.LastSignState.Step = 0

	// Save the validator
	err = privVal.Save()
	require.NoError(t, err)
}

// setupBlockstore creates minimal blocks in the blockstore for testing
func setupBlockstore(t *testing.T, db dbm.DB, height int64, valSet *tmtypes.ValidatorSet) {
	// We need to create block metadata for height and height-1
	// The rollback function loads block metadata, not full blocks
	// Let's create the minimal required data

	var prevBlockID tmtypes.BlockID
	// For each height from 1 to height, create block metadata
	for h := int64(1); h <= height; h++ {
		// Create a simple block
		block := &tmtypes.Block{
			Header: tmtypes.Header{
				Version: version.Consensus{
					Block: version.BlockProtocol,
					App:   1,
				},
				ChainID:            "test-chain",
				Height:             h,
				Time:               time.Now(),
				LastBlockID:        prevBlockID,
				LastCommitHash:     crypto.CRandBytes(tmhash.Size),
				DataHash:           crypto.CRandBytes(tmhash.Size),
				ValidatorsHash:     valSet.Hash(),
				NextValidatorsHash: valSet.Hash(),
				ConsensusHash:      crypto.CRandBytes(tmhash.Size),
				AppHash:            crypto.CRandBytes(tmhash.Size),
				LastResultsHash:    crypto.CRandBytes(tmhash.Size),
				EvidenceHash:       crypto.CRandBytes(tmhash.Size),
				ProposerAddress:    valSet.Validators[0].Address,
			},
			Data:       tmtypes.Data{},
			Evidence:   tmtypes.EvidenceList{},
			LastCommit: &tmtypes.Commit{Height: h - 1},
		}

		// Create part set
		partSet, err := block.MakePartSet(tmtypes.BlockPartSizeBytes)
		require.NoError(t, err)

		// Save block
		saveBlockToDB(t, db, block, partSet, h)

		// Update prevBlockID for next iteration
		prevBlockID = tmtypes.BlockID{
			Hash:          block.Hash(),
			PartSetHeader: partSet.Header(),
		}
	}
}

// saveBlockToDB manually saves block data to the database
func saveBlockToDB(t *testing.T, db dbm.DB, block *tmtypes.Block, partSet *tmtypes.PartSet, height int64) {
	batch := db.NewBatch()
	defer batch.Close()

	// Save block parts
	for i := 0; i < int(partSet.Total()); i++ {
		partBytes, err := proto.Marshal(partSet.GetPart(i).ToProto())
		require.NoError(t, err)

		// blockPartKey: prefixBlockPart (2) + height + index
		partKey, err := orderedcode.Append(nil, int64(2), height, int64(i))
		require.NoError(t, err)
		err = batch.Set(partKey, partBytes)
		require.NoError(t, err)
	}

	// Save block meta
	blockMeta := tmtypes.NewBlockMeta(block, partSet)
	blockMetaProto := blockMeta.ToProto()
	metaBytes, err := proto.Marshal(blockMetaProto)
	require.NoError(t, err)

	// blockMetaKey: prefixBlockMeta (0) + height
	metaKey, err := orderedcode.Append(nil, int64(0), height)
	require.NoError(t, err)
	err = batch.Set(metaKey, metaBytes)
	require.NoError(t, err)

	// Save block hash mapping
	hashKey, err := orderedcode.Append(nil, int64(1), string(block.Hash()))
	require.NoError(t, err)
	err = batch.Set(hashKey, []byte(fmt.Sprintf("%d", height)))
	require.NoError(t, err)

	// Save commit
	if block.LastCommit != nil {
		commitProto := block.LastCommit.ToProto()
		commitBytes, err := proto.Marshal(commitProto)
		require.NoError(t, err)

		// blockCommitKey: prefixBlockCommit (3) + height
		commitKey, err := orderedcode.Append(nil, int64(3), height)
		require.NoError(t, err)
		err = batch.Set(commitKey, commitBytes)
		require.NoError(t, err)
	}

	// Save seen commit (for height)
	seenCommit := &tmtypes.Commit{Height: height}
	seenCommitProto := seenCommit.ToProto()
	seenCommitBytes, err := proto.Marshal(seenCommitProto)
	require.NoError(t, err)

	// seenCommitKey: prefixSeenCommit (4)
	seenCommitKey, err := orderedcode.Append(nil, int64(4))
	require.NoError(t, err)
	err = batch.Set(seenCommitKey, seenCommitBytes)
	require.NoError(t, err)

	err = batch.WriteSync()
	require.NoError(t, err)
}

// TestRollbackScenario1_BothAtSameHeight tests scenario 1: both app and tendermint at same height
func TestRollbackScenario1_BothAtSameHeight(t *testing.T) {
	initialHeight := int64(10)
	targetHeight := int64(9)

	// Setup app at height 10
	app, tempDir := setupTestApp(t, initialHeight)

	// Setup tendermint state at height 10
	cfg := setupTendermintStateDB(t, tempDir, initialHeight)

	// Create app creator
	appCreator := func(log.Logger, dbm.DB, io.Writer, *tmconfig.Config, servertypes.AppOptions) servertypes.Application {
		return app
	}

	// Create rollback command
	cmd := NewRollbackCmd(appCreator, tempDir)
	cmd.SetArgs([]string{})

	// Set up server context
	ctx := &Context{
		Config: cfg,
		Logger: log.NewNopLogger(),
		Viper:  nil,
	}
	cmdCtx := context.WithValue(context.Background(), ServerContextKey, ctx)

	// Execute rollback
	err := cmd.ExecuteContext(cmdCtx)
	require.NoError(t, err)

	// Verify app state was rolled back
	appCommit := app.CommitMultiStore().LastCommitID()
	require.Equal(t, targetHeight, appCommit.GetVersion())

	// Verify tendermint state was rolled back to match app
	tmState, err := tmcmd.LoadTendermintState(cfg)
	require.NoError(t, err)
	require.Equal(t, targetHeight, tmState.LastBlockHeight)
}

// TestRollbackScenario2_AppAheadOfTendermint tests scenario 2: app already rolled back, tendermint not
func TestRollbackScenario2_AppAheadOfTendermint(t *testing.T) {
	appHeight := int64(9)
	tmHeight := int64(10)
	targetHeight := int64(9)

	// Setup app at height 9 (already rolled back)
	app, tempDir := setupTestApp(t, appHeight)

	// Setup tendermint state at height 10 (not yet rolled back)
	cfg := setupTendermintStateDB(t, tempDir, tmHeight)

	// Create app creator
	appCreator := func(log.Logger, dbm.DB, io.Writer, *tmconfig.Config, servertypes.AppOptions) servertypes.Application {
		return app
	}

	// Create rollback command
	cmd := NewRollbackCmd(appCreator, tempDir)
	cmd.SetArgs([]string{})

	// Set up server context
	ctx := &Context{
		Config: cfg,
		Logger: log.NewNopLogger(),
		Viper:  nil,
	}
	cmdCtx := context.WithValue(context.Background(), ServerContextKey, ctx)

	// Execute rollback - should complete tendermint rollback
	err := cmd.ExecuteContext(cmdCtx)
	require.NoError(t, err)

	// Verify app state is still at target height
	appCommit := app.CommitMultiStore().LastCommitID()
	require.Equal(t, targetHeight, appCommit.GetVersion())

	// Verify tendermint state was rolled back to match app
	tmState, err := tmcmd.LoadTendermintState(cfg)
	require.NoError(t, err)
	require.Equal(t, targetHeight, tmState.LastBlockHeight)
}

// TestRollbackScenario3_TendermintAheadOfApp tests scenario 3: app ahead of tendermint
func TestRollbackScenario3_TendermintAheadOfApp(t *testing.T) {
	appHeight := int64(10)
	tmHeight := int64(9)
	targetHeight := int64(9)

	// Setup app at height 10 (ahead of tendermint)
	app, tempDir := setupTestApp(t, appHeight)

	// Setup tendermint state at height 9
	cfg := setupTendermintStateDB(t, tempDir, tmHeight)

	// Create app creator
	appCreator := func(log.Logger, dbm.DB, io.Writer, *tmconfig.Config, servertypes.AppOptions) servertypes.Application {
		return app
	}

	// Create rollback command
	cmd := NewRollbackCmd(appCreator, tempDir)
	cmd.SetArgs([]string{})

	// Set up server context
	ctx := &Context{
		Config: cfg,
		Logger: log.NewNopLogger(),
		Viper:  nil,
	}
	cmdCtx := context.WithValue(context.Background(), ServerContextKey, ctx)

	// Execute rollback - should rollback app only
	err := cmd.ExecuteContext(cmdCtx)
	require.NoError(t, err)

	// Verify app state was rolled back to match tendermint
	appCommit := app.CommitMultiStore().LastCommitID()
	require.Equal(t, targetHeight, appCommit.GetVersion())

	// Verify tendermint state was rolled back to match app
	tmState, err := tmcmd.LoadTendermintState(cfg)
	require.NoError(t, err)
	require.Equal(t, targetHeight, tmState.LastBlockHeight)
}

// TestRollbackErrorCases tests error cases
func TestRollbackErrorCases(t *testing.T) {
	t.Run("Cannot rollback below height 0", func(t *testing.T) {
		// Setup app at height 0
		app, tempDir := setupTestApp(t, 0)

		// Setup tendermint state at height 0
		cfg := setupTendermintStateDB(t, tempDir, 0)

		appCreator := func(log.Logger, dbm.DB, io.Writer, *tmconfig.Config, servertypes.AppOptions) servertypes.Application {
			return app
		}

		cmd := NewRollbackCmd(appCreator, tempDir)
		cmd.SetArgs([]string{})

		ctx := &Context{
			Config: cfg,
			Logger: log.NewNopLogger(),
			Viper:  nil,
		}
		cmdCtx := context.WithValue(context.Background(), ServerContextKey, ctx)

		err := cmd.ExecuteContext(cmdCtx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot rollback below 0")
	})
}

// TestRollbackWithNumBlocks tests rolling back multiple blocks using the --num-blocks flag
func TestRollbackWithNumBlocks(t *testing.T) {
	initialHeight := int64(10)
	numBlocks := int64(3)
	targetHeight := initialHeight - numBlocks

	// Setup app at height 10
	app, tempDir := setupTestApp(t, initialHeight)

	// Setup tendermint state at height 10
	cfg := setupTendermintStateDB(t, tempDir, initialHeight)

	// Create app creator
	appCreator := func(log.Logger, dbm.DB, io.Writer, *tmconfig.Config, servertypes.AppOptions) servertypes.Application {
		return app
	}

	// Create rollback command
	cmd := NewRollbackCmd(appCreator, tempDir)
	// Pass the num-blocks flag
	cmd.SetArgs([]string{"--num-blocks", fmt.Sprintf("%d", numBlocks)})

	// Set up server context
	ctx := &Context{
		Config: cfg,
		Logger: log.NewNopLogger(),
		Viper:  nil,
	}
	cmdCtx := context.WithValue(context.Background(), ServerContextKey, ctx)

	// Execute rollback
	err := cmd.ExecuteContext(cmdCtx)
	require.NoError(t, err)

	// Verify app state was rolled back
	appCommit := app.CommitMultiStore().LastCommitID()
	require.Equal(t, targetHeight, appCommit.GetVersion())

	// Verify tendermint state was rolled back to match app
	tmState, err := tmcmd.LoadTendermintState(cfg)
	require.NoError(t, err)
	require.Equal(t, targetHeight, tmState.LastBlockHeight)
}
