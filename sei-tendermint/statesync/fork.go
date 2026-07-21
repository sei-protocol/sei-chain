package statesync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	tmstore "github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

// ForkParams describes the forged chain head written by BootstrapForkedChain.
type ForkParams struct {
	// ChainID is the new chain's ID. It must differ from the source chain so
	// forked nodes can never cross-connect with the origin network.
	ChainID string
	// Height is the forged last-block height. The application state on disk
	// must already be committed at exactly this height.
	Height int64
	// AppHash is the application hash after Height, i.e. the app's reported
	// LastBlockAppHash at startup.
	AppHash []byte
	// Validators sign the forged commits and become the new chain's validator
	// set. Order must match Powers.
	Validators []types.PrivValidator
	// Powers are the voting powers assigned to Validators.
	Powers []int64
	// ConsensusParams are used for the forged state and genesis doc.
	ConsensusParams types.ConsensusParams
	// AppVersion is stamped into the forged state's consensus version. It must
	// match the app's reported AppVersion or the first proposed header will
	// diverge between the forged state and the app.
	AppVersion uint64
	// Time is the forged last-block time; also used as the genesis time.
	Time time.Time
}

// BootstrapForkedChain writes the Tendermint state, block store, and genesis
// records that let a set of locally-controlled validators continue a copied
// application state as a brand-new chain at Height+1.
//
// It is the fork-time sibling of BootstrapFromRPC: instead of fetching a
// trusted block and commit from a live chain, it forges them — a synthetic
// block at Height whose seen commit is signed by the supplied validators, a
// state record whose validator sets are exactly those validators, and a
// FinalizeBlock response carrying AppHash. Every validator of the new chain
// must receive an identical copy of the resulting data directory (plus its own
// private validator key); the forged records are deterministic given identical
// inputs except for commit timestamps, so the SAME invocation output must be
// cloned to all nodes rather than re-running the tool per node.
func BootstrapForkedChain(ctx context.Context, cfg *tmcfg.Config, params ForkParams) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if params.ChainID == "" {
		return fmt.Errorf("chain ID is required")
	}
	if params.Height <= 1 {
		return fmt.Errorf("fork height must be greater than 1, got %d", params.Height)
	}
	if len(params.AppHash) == 0 {
		return fmt.Errorf("app hash is required")
	}
	if len(params.Validators) == 0 {
		return fmt.Errorf("at least one validator is required")
	}
	if len(params.Validators) != len(params.Powers) {
		return fmt.Errorf("validators (%d) and powers (%d) must match", len(params.Validators), len(params.Powers))
	}
	if params.Time.IsZero() {
		params.Time = time.Now().UTC()
	}

	vals := make([]*types.Validator, len(params.Validators))
	for i, pv := range params.Validators {
		pubKey, err := pv.GetPubKey(ctx)
		if err != nil {
			return fmt.Errorf("get validator %d pubkey: %w", i, err)
		}
		vals[i] = types.NewValidator(pubKey, params.Powers[i])
	}
	valSet := types.NewValidatorSet(vals)

	// Forge the commit for Height-1 that the forged block embeds as its
	// LastCommit. Nothing ever verifies this commit (no light client walks
	// below the fork height on the new chain), but Block.ValidateBasic
	// requires a structurally valid commit, so it is signed for real.
	prevBlockID := syntheticBlockID(params.ChainID, params.Height-1)
	prevVoteSet := types.NewVoteSet(params.ChainID, params.Height-1, 0, tmproto.PrecommitType, valSet)
	prevCommit, err := types.MakeCommit(ctx, prevBlockID, params.Height-1, 0, prevVoteSet, params.Validators, params.Time)
	if err != nil {
		return fmt.Errorf("forge commit for height %d: %w", params.Height-1, err)
	}

	block := types.MakeBlock(params.Height, nil, prevCommit, nil)
	block.Header.ChainID = params.ChainID
	block.Header.Version = version.Consensus{Block: version.BlockProtocol, App: params.AppVersion}
	block.Header.Time = params.Time
	block.Header.LastBlockID = prevBlockID
	block.Header.ValidatorsHash = valSet.Hash()
	block.Header.NextValidatorsHash = valSet.Hash()
	block.Header.ConsensusHash = params.ConsensusParams.HashConsensusParams()
	// The header's AppHash is the app hash after Height-1, which no longer
	// exists anywhere; reuse the post-Height hash. Nothing on the new chain
	// verifies it.
	block.Header.AppHash = params.AppHash
	block.Header.LastResultsHash = types.NewResults(nil).Hash()
	block.Header.ProposerAddress = valSet.Proposer.Address

	blockParts, err := block.MakePartSet(types.BlockPartSizeBytes)
	if err != nil {
		return fmt.Errorf("make block part set: %w", err)
	}
	blockID := types.BlockID{Hash: block.Hash(), PartSetHeader: blockParts.Header()}

	// The seen commit for Height becomes block Height+1's LastCommit and IS
	// verified — against state.LastValidators, which is the same forged set.
	seenVoteSet := types.NewVoteSet(params.ChainID, params.Height, 0, tmproto.PrecommitType, valSet)
	seenCommit, err := types.MakeCommit(ctx, blockID, params.Height, 0, seenVoteSet, params.Validators, params.Time)
	if err != nil {
		return fmt.Errorf("forge seen commit for height %d: %w", params.Height, err)
	}

	state := sm.State{
		Version: sm.Version{
			Consensus: version.Consensus{Block: version.BlockProtocol, App: params.AppVersion},
			Software:  version.TMVersion,
		},
		ChainID:         params.ChainID,
		InitialHeight:   1,
		LastBlockHeight: params.Height,
		LastBlockID:     blockID,
		LastBlockTime:   params.Time,
		NextValidators:  valSet.Copy(),
		Validators:      valSet.Copy(),
		LastValidators:  valSet.Copy(),
		// Post-fork saves store validator/params records as POINTERS to the
		// height these fields last changed, so both must reference heights
		// Bootstrap actually materializes (Height..Height+2 for validators,
		// Height for params) — pointing at 1 would dangle, and the first
		// LoadValidators after the fork would fail. Mirrors what the state
		// sync provider does with its light-block heights.
		LastHeightValidatorsChanged:      params.Height + 1,
		ConsensusParams:                  params.ConsensusParams,
		LastHeightConsensusParamsChanged: params.Height + 1,
		LastResultsHash:                  types.NewResults(nil).Hash(),
		AppHash:                          params.AppHash,
	}

	stateDB, err := tmcfg.DefaultDBProvider(&tmcfg.DBContext{ID: "state", Config: cfg})
	if err != nil {
		return fmt.Errorf("open state db: %w", err)
	}
	defer func() { _ = stateDB.Close() }()
	stateStore := sm.NewStore(stateDB)
	if err := stateStore.Bootstrap(state); err != nil {
		return fmt.Errorf("bootstrap state store: %w", err)
	}
	if err := stateStore.SaveFinalizeBlockResponses(params.Height, &abci.ResponseFinalizeBlock{
		AppHash: params.AppHash,
	}); err != nil {
		return fmt.Errorf("save finalize block response: %w", err)
	}

	blockStoreDB, err := tmcfg.DefaultDBProvider(&tmcfg.DBContext{ID: "blockstore", Config: cfg})
	if err != nil {
		return fmt.Errorf("open blockstore db: %w", err)
	}
	defer func() { _ = blockStoreDB.Close() }()
	blockStore := tmstore.NewBlockStore(blockStoreDB)
	blockStore.SaveBlock(block, blockParts, seenCommit)

	genesisValidators := make([]types.GenesisValidator, len(vals))
	for i, v := range vals {
		genesisValidators[i] = types.GenesisValidator{
			Address: v.Address,
			PubKey:  v.PubKey,
			Power:   v.VotingPower,
			Name:    fmt.Sprintf("fork-validator-%d", i),
		}
	}
	genDoc := types.GenesisDoc{
		ChainID:         params.ChainID,
		GenesisTime:     params.Time,
		InitialHeight:   1,
		ConsensusParams: &params.ConsensusParams,
		Validators:      genesisValidators,
	}
	if err := genDoc.ValidateAndComplete(); err != nil {
		return fmt.Errorf("validate forged genesis doc: %w", err)
	}
	if err := genDoc.SaveAs(cfg.GenesisFile()); err != nil {
		return fmt.Errorf("write forged genesis doc: %w", err)
	}
	return nil
}

// syntheticBlockID derives a deterministic, well-formed BlockID for a height
// that has no real block on the forked chain.
func syntheticBlockID(chainID string, height int64) types.BlockID {
	hash := sha256.Sum256(fmt.Appendf(nil, "sei-fork-block/%s/%d", chainID, height))
	partHash := sha256.Sum256(fmt.Appendf(nil, "sei-fork-part/%s/%d", chainID, height))
	return types.BlockID{
		Hash:          hash[:],
		PartSetHeader: types.PartSetHeader{Total: 1, Hash: partHash[:]},
	}
}
