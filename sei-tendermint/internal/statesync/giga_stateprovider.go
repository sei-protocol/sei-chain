package statesync

import (
	"context"

	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// GigaStateProvider is a minimal StateProvider implementation used when the
// node is running in autobahn (giga) mode. Unlike the RPC and P2P providers,
// it does NOT verify the app hash cryptographically before applying chunks —
// there is currently no cheap way to obtain an authoritative AppHash@h in
// giga mode (AppQCs are not retained historically, and CometBFT-style signed
// commits do not exist on giga peers). Instead this provider returns empty
// stubs; the syncer recognises an empty trustedAppHash and skips the post-
// restore AppHash comparison.
//
// TODO(autobahn-snapshot-proof): peers serving a giga snapshot must include
// an AppQC@snapshot_height in the snapshot.Metadata. This provider should
// then decode the AppQC, verify it against the committee, compare its
// AppHash against the snapshot's self-advertised AppHash, and return the
// verified AppHash as the authoritative trustedAppHash. Vanilla SyncAny
// will then retry on mismatch (errRejectSnapshot) and will not loop-trap a
// joiner against a persistently malicious peer. Until that lands, a bad
// snapshot wedges the joiner and requires external intervention to retry.
type GigaStateProvider struct {
	genDoc *types.GenesisDoc
}

// NewGigaStateProvider returns a provider that emits minimal trust data for
// autobahn state sync. genDoc supplies the chain identity and initial
// validator set that the post-sync bootstrap needs.
func NewGigaStateProvider(genDoc *types.GenesisDoc) *GigaStateProvider {
	return &GigaStateProvider{genDoc: genDoc}
}

// AppHash returns a zero-length slice. The syncer treats an empty
// trustedAppHash as "skip the post-restore AppHash check" (see
// syncer.verifyApp). This is the naive trust model described above.
func (p *GigaStateProvider) AppHash(_ context.Context, _ uint64) ([]byte, error) {
	return nil, nil
}

// Commit returns a minimal *types.Commit. Autobahn does not consume
// CometBFT commits at runtime, but the syncer threads one through to
// stateStore.Bootstrap; an empty commit satisfies its non-nil requirement
// without asserting any fabricated signatures.
func (p *GigaStateProvider) Commit(_ context.Context, height uint64) (*types.Commit, error) {
	return &types.Commit{Height: int64(height)}, nil //nolint:gosec // uint64->int64 at known-small block heights
}

// State synthesises the minimum sm.State needed by stateStore.Bootstrap.
// It populates identity + validator fields from the genesis doc; these
// match what every giga node already computes locally since the autobahn
// committee is static and derived from genesis.
//
// TODO(epoch): once autobahn supports dynamic committees, Validators and
// NextValidators must come from the epoch lookup at height h, not the
// genesis set.
func (p *GigaStateProvider) State(_ context.Context, height uint64) (sm.State, error) {
	state := sm.State{
		ChainID:       p.genDoc.ChainID,
		InitialHeight: p.genDoc.InitialHeight,
		//nolint:gosec // heights fit in int64 at any realistic chain height.
		LastBlockHeight: int64(height),
	}
	if state.InitialHeight == 0 {
		state.InitialHeight = 1
	}

	// Derive the CometBFT validator set from the genesis validator list. In
	// giga mode the committee is static (see TODO(epoch) above).
	vals := make([]*types.Validator, len(p.genDoc.Validators))
	for i, gv := range p.genDoc.Validators {
		vals[i] = types.NewValidator(gv.PubKey, gv.Power)
	}
	if len(vals) > 0 {
		state.Validators = types.NewValidatorSet(vals)
		state.NextValidators = state.Validators.CopyIncrementProposerPriority(1)
		state.LastValidators = state.Validators
	}

	state.ConsensusParams = *p.genDoc.ConsensusParams

	return state, nil
}
