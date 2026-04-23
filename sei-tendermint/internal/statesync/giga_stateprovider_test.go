package statesync

import (
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// makeTestGenDoc produces a minimal GenesisDoc suitable for feeding
// GigaStateProvider — enough for it to synthesise a coherent sm.State.
func makeTestGenDoc(t *testing.T, numValidators int) *types.GenesisDoc {
	t.Helper()
	vals := make([]types.GenesisValidator, numValidators)
	for i := range vals {
		priv := ed25519.TestSecretKey([]byte(fmt.Sprintf("giga-sp-%d", i)))
		pub := priv.Public()
		vals[i] = types.GenesisValidator{
			Address: pub.Address(),
			PubKey:  pub,
			Power:   10,
			Name:    fmt.Sprintf("v%d", i),
		}
	}
	cp := types.DefaultConsensusParams()
	return &types.GenesisDoc{
		GenesisTime:     tmtime.Now(),
		ChainID:         "giga-test-chain",
		InitialHeight:   1,
		ConsensusParams: cp,
		Validators:      vals,
	}
}

// TestGigaStateProvider_AppHashIsEmpty verifies that the naive giga
// provider returns an empty AppHash. The syncer treats this as a signal
// to skip the post-restore AppHash check (see syncer.verifyApp).
func TestGigaStateProvider_AppHashIsEmpty(t *testing.T) {
	p := NewGigaStateProvider(makeTestGenDoc(t, 4))
	h, err := p.AppHash(t.Context(), 1234)
	require.NoError(t, err)
	require.Empty(t, h, "naive giga provider must return empty AppHash")
}

// TestGigaStateProvider_CommitMinimal checks the Commit stub carries
// nothing more than the requested height — no fabricated signatures.
func TestGigaStateProvider_CommitMinimal(t *testing.T) {
	p := NewGigaStateProvider(makeTestGenDoc(t, 4))
	c, err := p.Commit(t.Context(), 1234)
	require.NoError(t, err)
	require.Equal(t, int64(1234), c.Height)
	require.Empty(t, c.Signatures, "commit must not fabricate signatures")
}

// TestGigaStateProvider_StatePopulatesBootstrapFields verifies that
// State returns every field that stateStore.Bootstrap needs populated
// (ChainID, InitialHeight, LastBlockHeight, Validators, NextValidators,
// ConsensusParams). Empty/zero for any of these would trip the reactor's
// post-sync bootstrap.
func TestGigaStateProvider_StatePopulatesBootstrapFields(t *testing.T) {
	gd := makeTestGenDoc(t, 4)
	p := NewGigaStateProvider(gd)

	state, err := p.State(t.Context(), 9999)
	require.NoError(t, err)

	require.Equal(t, gd.ChainID, state.ChainID)
	require.Equal(t, gd.InitialHeight, state.InitialHeight)
	require.Equal(t, int64(9999), state.LastBlockHeight)

	require.NotNil(t, state.Validators)
	require.Equal(t, 4, len(state.Validators.Validators))
	require.NotNil(t, state.NextValidators)
	require.Equal(t, 4, len(state.NextValidators.Validators))

	// ConsensusParams is copied by value; a zeroed one would fail
	// downstream hash checks.
	require.NotZero(t, state.ConsensusParams.Block.MaxBytes,
		"ConsensusParams should be populated from genesis")
}

// TestGigaStateProvider_StateInitialHeightDefaults ensures a
// GenesisDoc with InitialHeight == 0 is normalised to 1 (matches the
// vanilla P2P provider).
func TestGigaStateProvider_StateInitialHeightDefaults(t *testing.T) {
	gd := makeTestGenDoc(t, 1)
	gd.InitialHeight = 0
	p := NewGigaStateProvider(gd)

	state, err := p.State(t.Context(), 42)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.InitialHeight)
}

// TestGigaStateProvider_NoValidators degenerate case: GenesisDoc without
// validators. We don't crash; ValidatorSet stays nil. Autobahn nodes are
// always configured with a validator set in practice, so this exists as a
// defensiveness check rather than a supported mode.
func TestGigaStateProvider_NoValidators(t *testing.T) {
	gd := &types.GenesisDoc{
		GenesisTime:     time.Unix(0, 0),
		ChainID:         "giga-empty",
		InitialHeight:   1,
		ConsensusParams: types.DefaultConsensusParams(),
	}
	p := NewGigaStateProvider(gd)
	state, err := p.State(t.Context(), 5)
	require.NoError(t, err)
	require.Nil(t, state.Validators)
	require.Nil(t, state.NextValidators)
}

// Ensure the provider satisfies the StateProvider interface at compile
// time. Other packages may rely on this.
var _ StateProvider = (*GigaStateProvider)(nil)
