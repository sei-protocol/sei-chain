package consensus

import (
	"errors"
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	sf "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestHandshakeRefusesDiscardedStoredBlocks(t *testing.T) {
	ctx := t.Context()

	cfg, err := ResetConfig(t.TempDir(), "handshake_rewind_test_")
	require.NoError(t, err)
	privVal, err := privval.LoadFilePV(cfg.PrivValidator.KeyFile(), cfg.PrivValidator.StateFile())
	require.NoError(t, err)
	stateDB, state, store := stateAndStore(t, cfg, 0x0)
	stateStore := sm.NewStore(stateDB)
	genDoc, err := sm.MakeGenesisDocFromFile(cfg.GenesisFile())
	require.NoError(t, err)
	state.LastValidators = state.Validators.Copy()
	blocks := sf.MakeBlocks(ctx, t, 3, &state, privVal)
	store.chain = blocks

	t.Cleanup(types.ReplaceRewinds([]types.Rewind{{
		ChainID:    blocks[1].ChainID,
		SafeHeight: 1,
		Discarded: []types.DiscardedBlock{
			{Height: 2, Hash: blocks[1].Hash()},
			{Height: 3, Hash: blocks[2].Hash()},
		},
	}}))

	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(ctx))

	t.Run("replay", func(t *testing.T) {
		h := NewHandshaker(stateStore, state, store, eventBus, genDoc, types.DefaultConsensusPolicy())
		err := h.Handshake(ctx, proxy.New(&genesisValidatorsApp{
			badApp:     badApp{numBlocks: 3, onlyLastHashIsWrong: true},
			validators: genDoc.ValidatorUpdates(),
		}))
		require.True(t, errors.Is(err, types.ErrDiscardedBlock), "got %v", err)
	})

	t.Run("app at the discarded tip", func(t *testing.T) {
		h := NewHandshaker(stateStore, state, store, eventBus, genDoc, types.DefaultConsensusPolicy())
		err := h.Handshake(ctx, proxy.New(&tipApp{height: 3, appHash: state.AppHash}))
		require.True(t, errors.Is(err, types.ErrDiscardedBlock), "got %v", err)
	})
}

// tipApp reports that it has already committed the given height.
type tipApp struct {
	abci.BaseApplication
	height  int64
	appHash []byte
}

func (app *tipApp) Info() *abci.ResponseInfo {
	return &abci.ResponseInfo{LastBlockHeight: app.height, LastBlockAppHash: app.appHash}
}

// genesisValidatorsApp returns the genesis validators from InitChain.
type genesisValidatorsApp struct {
	badApp
	validators []abci.ValidatorUpdate
}

func (app *genesisValidatorsApp) InitChain(*abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	return &abci.ResponseInitChain{Validators: app.validators}, nil
}
