package consensus

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"

	dbm "github.com/tendermint/tm-db"

	"github.com/stretchr/testify/require"
)

// newStateFromEmptyGenesisValidators builds a consensus State whose genesis
// carries no validators — the shape a gentx-launched chain has before its set
// is installed — and drives the reactor's OnStart -> updateStateFromStore path,
// leaving the round state holding an empty validator set.
func newStateFromEmptyGenesisValidators(t *testing.T) *State {
	t.Helper()
	cfg := configSetup(t)
	genDoc := factory.GenesisDoc(cfg, time.Now(), nil, factory.ConsensusParams())
	state, err := sm.MakeGenesisState(genDoc)
	require.NoError(t, err)
	require.Equal(t, 0, state.Validators.Size(), "genesis validator set must be empty")
	require.Zero(t, state.LastBlockHeight)

	thisConfig, err := ResetConfig(t.TempDir(), "plt794")
	require.NoError(t, err)

	app := kvstore.NewApplication()
	t.Cleanup(func() { _ = app.Close() })
	_, err = app.InitChain(&abci.RequestInitChain{})
	require.NoError(t, err)

	pv := loadPrivValidator(thisConfig)
	blockStore := store.NewBlockStore(dbm.NewMemDB())
	proxyApp := proxy.New(app)

	return newStateWithConfigAndBlockStore(t, thisConfig, state, pv, proxyApp, blockStore).State
}

// TestConsensusStateRPCEmptyValidatorSet guards the /consensus_state RPC path
// (GetRoundStateSimpleJSON -> RoundStateSimple -> leader resolution) against an
// empty validator set: with zero total voting power, leader election divides by
// zero and takes the process down. The RPC must serialize an empty proposer
// instead.
func TestConsensusStateRPCEmptyValidatorSet(t *testing.T) {
	cs := newStateFromEmptyGenesisValidators(t)
	require.Equal(t, 0, cs.roundState.Validators().Size())

	var bz []byte
	require.NotPanics(t, func() {
		var err error
		bz, err = cs.GetRoundStateSimpleJSON()
		require.NoError(t, err)
	}, "the /consensus_state RPC path must not divide by zero on an empty validator set")

	// The serialized round state carries an empty proposer rather than crashing.
	var simple struct {
		Proposer struct {
			Address []byte `json:"address"`
			Index   int32  `json:"index"`
		} `json:"proposer"`
	}
	require.NoError(t, json.Unmarshal(bz, &simple))
	require.Empty(t, simple.Proposer.Address)
	require.Zero(t, simple.Proposer.Index)
}
