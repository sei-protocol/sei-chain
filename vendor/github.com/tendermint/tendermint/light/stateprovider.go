package light

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/internal/p2p"
	sm "github.com/tendermint/tendermint/internal/state"
	"github.com/tendermint/tendermint/libs/log"
	lightprovider "github.com/tendermint/tendermint/light/provider"
	lighthttp "github.com/tendermint/tendermint/light/provider/http"
	lightrpc "github.com/tendermint/tendermint/light/rpc"
	lightdb "github.com/tendermint/tendermint/light/store/db"
	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/types"
	"github.com/tendermint/tendermint/version"
)

const (
	consensusParamsResponseTimeout = 5 * time.Second
)

//go:generate ../../scripts/mockery_generate.sh StateProvider

// StateProvider is a provider of trusted state data for bootstrapping a node. This refers
// to the state.State object, not the state machine. There are two implementations. One
// uses the P2P layer and the other uses the RPC layer. Both use light client verification.
type StateProvider interface {
	// AppHash returns the app hash after the given height has been committed.
	AppHash(ctx context.Context, height uint64) ([]byte, error)
	// Commit returns the commit at the given height.
	Commit(ctx context.Context, height uint64) (*types.Commit, error)
	// State returns a state object at the given height.
	State(ctx context.Context, height uint64) (sm.State, error)
}

type stateProviderRPC struct {
	sync.Mutex              // light.Client is not concurrency-safe
	lc                      *Client
	initialHeight           int64
	providers               map[lightprovider.Provider]string
	verifyLightBlockTimeout time.Duration
	logger                  log.Logger
}

// NewRPCStateProvider creates a new StateProvider using a light client and RPC clients.
func NewRPCStateProvider(
	ctx context.Context,
	chainID string,
	initialHeight int64,
	verifyLightBlockTimeout time.Duration,
	servers []string,
	trustOptions TrustOptions,
	logger log.Logger,
	blacklistTTL time.Duration,
) (StateProvider, error) {
	if len(servers) < 2 {
		return nil, fmt.Errorf("at least 2 RPC servers are required, got %d", len(servers))
	}

	providers := make([]lightprovider.Provider, 0, len(servers))
	providerRemotes := make(map[lightprovider.Provider]string)
	for _, server := range servers {
		client, err := rpcClient(server)
		if err != nil {
			return nil, fmt.Errorf("failed to set up RPC client: %w", err)
		}
		provider := lighthttp.NewWithClient(chainID, client)
		providers = append(providers, provider)
		// We store the RPC addresses keyed by provider, so we can find the address of the primary
		// provider used by the light client and use it to fetch consensus parameters.
		providerRemotes[provider] = server
	}

	lc, err := NewClient(ctx, chainID, trustOptions, providers[0], providers[1:],
		lightdb.New(dbm.NewMemDB()), blacklistTTL, Logger(logger))
	if err != nil {
		return nil, err
	}
	return &stateProviderRPC{
		logger:                  logger,
		lc:                      lc,
		initialHeight:           initialHeight,
		providers:               providerRemotes,
		verifyLightBlockTimeout: verifyLightBlockTimeout,
	}, nil
}

func (s *stateProviderRPC) verifyLightBlockAtHeight(ctx context.Context, height uint64, ts time.Time) (*types.LightBlock, error) {
	ctx, cancel := context.WithTimeout(ctx, s.verifyLightBlockTimeout)
	defer cancel()
	return s.lc.VerifyLightBlockAtHeight(ctx, int64(height), ts)
}

// AppHash implements part of StateProvider. It calls the application to verify the
// light blocks at heights h+1 and h+2 and, if verification succeeds, reports the app
// hash for the block at height h+1 which correlates to the state at height h.
func (s *stateProviderRPC) AppHash(ctx context.Context, height uint64) ([]byte, error) {
	s.Lock()
	defer s.Unlock()

	// We have to fetch the next height, which contains the app hash for the previous height.
	header, err := s.verifyLightBlockAtHeight(ctx, height+1, time.Now())
	if err != nil {
		return nil, err
	}

	// We also try to fetch the blocks at H+2, since we need these
	// when building the state while restoring the snapshot. This avoids the race
	// condition where we try to restore a snapshot before H+2 exists.
	_, err = s.verifyLightBlockAtHeight(ctx, height+2, time.Now())
	if err != nil {
		return nil, err
	}
	return header.AppHash, nil
}

// Commit implements StateProvider.
func (s *stateProviderRPC) Commit(ctx context.Context, height uint64) (*types.Commit, error) {
	s.Lock()
	defer s.Unlock()
	header, err := s.verifyLightBlockAtHeight(ctx, height, time.Now())
	if err != nil {
		return nil, err
	}
	return header.Commit, nil
}

// State implements StateProvider.
func (s *stateProviderRPC) State(ctx context.Context, height uint64) (sm.State, error) {
	s.Lock()
	defer s.Unlock()

	state := sm.State{
		ChainID:       s.lc.ChainID(),
		InitialHeight: s.initialHeight,
	}
	if state.InitialHeight == 0 {
		state.InitialHeight = 1
	}

	// The snapshot height maps onto the state heights as follows:
	//
	// height: last block, i.e. the snapshotted height
	// height+1: current block, i.e. the first block we'll process after the snapshot
	// height+2: next block, i.e. the second block after the snapshot
	//
	// We need to fetch the NextValidators from height+2 because if the application changed
	// the validator set at the snapshot height then this only takes effect at height+2.
	lastLightBlock, err := s.verifyLightBlockAtHeight(ctx, height, time.Now())
	if err != nil {
		return sm.State{}, err
	}
	currentLightBlock, err := s.verifyLightBlockAtHeight(ctx, height+1, time.Now())
	if err != nil {
		return sm.State{}, err
	}
	nextLightBlock, err := s.verifyLightBlockAtHeight(ctx, height+2, time.Now())
	if err != nil {
		return sm.State{}, err
	}

	state.Version = sm.Version{
		Consensus: currentLightBlock.Version,
		Software:  version.TMVersion,
	}
	state.LastBlockHeight = lastLightBlock.Height
	state.LastBlockTime = lastLightBlock.Time
	state.LastBlockID = lastLightBlock.Commit.BlockID
	state.AppHash = currentLightBlock.AppHash
	state.LastResultsHash = currentLightBlock.LastResultsHash
	state.LastValidators = lastLightBlock.ValidatorSet
	state.Validators = currentLightBlock.ValidatorSet
	state.NextValidators = nextLightBlock.ValidatorSet
	state.LastHeightValidatorsChanged = nextLightBlock.Height

	// We'll also need to fetch consensus params via RPC, using light client verification.
	primaryURL, ok := s.providers[s.lc.Primary()]
	if !ok || primaryURL == "" {
		return sm.State{}, fmt.Errorf("could not find address for primary light client provider")
	}
	primaryRPC, err := rpcClient(primaryURL)
	if err != nil {
		return sm.State{}, fmt.Errorf("unable to create RPC client: %w", err)
	}
	rpcclient := lightrpc.NewClient(s.logger, primaryRPC, s.lc)
	result, err := rpcclient.ConsensusParams(ctx, &currentLightBlock.Height)
	if err != nil {
		return sm.State{}, fmt.Errorf("unable to fetch consensus parameters for height %v: %w",
			nextLightBlock.Height, err)
	}
	state.ConsensusParams = result.ConsensusParams
	state.LastHeightConsensusParamsChanged = currentLightBlock.Height

	return state, nil
}

// rpcClient sets up a new RPC client
func rpcClient(server string) (*rpchttp.HTTP, error) {
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}
	return rpchttp.New(server)
}

type StateProviderP2P struct {
	sync.Mutex              // light.Client is not concurrency-safe
	lc                      *Client
	initialHeight           int64
	paramsSendCh            *p2p.Channel
	paramsRecvCh            chan types.ConsensusParams
	paramsReqCreator        func(uint64) proto.Message
	verifyLightBlockTimeout time.Duration
}

// NewP2PStateProvider creates a light client state
// provider but uses a dispatcher connected to the P2P layer
func NewP2PStateProvider(
	ctx context.Context,
	chainID string,
	initialHeight int64,
	verifyLightBlockTimeout time.Duration,
	providers []lightprovider.Provider,
	trustOptions TrustOptions,
	paramsSendCh *p2p.Channel,
	logger log.Logger,
	blacklistTTL time.Duration,
	paramsReqCreator func(uint64) proto.Message,
) (StateProvider, error) {
	if len(providers) < 2 {
		return nil, fmt.Errorf("at least 2 peers are required, got %d", len(providers))
	}

	lc, err := NewClient(ctx, chainID, trustOptions, providers[0], providers[1:],
		lightdb.New(dbm.NewMemDB()), blacklistTTL, Logger(logger))
	if err != nil {
		return nil, err
	}

	return &StateProviderP2P{
		lc:                      lc,
		initialHeight:           initialHeight,
		paramsSendCh:            paramsSendCh,
		paramsRecvCh:            make(chan types.ConsensusParams),
		paramsReqCreator:        paramsReqCreator,
		verifyLightBlockTimeout: verifyLightBlockTimeout,
	}, nil
}

func (s *StateProviderP2P) verifyLightBlockAtHeight(ctx context.Context, height uint64, ts time.Time) (*types.LightBlock, error) {
	ctx, cancel := context.WithTimeout(ctx, s.verifyLightBlockTimeout)
	defer cancel()
	return s.lc.VerifyLightBlockAtHeight(ctx, int64(height), ts)
}

// AppHash implements StateProvider.
func (s *StateProviderP2P) AppHash(ctx context.Context, height uint64) ([]byte, error) {
	s.Lock()
	defer s.Unlock()

	// We have to fetch the next height, which contains the app hash for the previous height.
	header, err := s.verifyLightBlockAtHeight(ctx, height+1, time.Now())
	if err != nil {
		return nil, err
	}

	// We also try to fetch the blocks at H+2, since we need these
	// when building the state while restoring the snapshot. This avoids the race
	// condition where we try to restore a snapshot before H+2 exists.
	_, err = s.verifyLightBlockAtHeight(ctx, height+2, time.Now())
	if err != nil {
		return nil, err
	}
	return header.AppHash, nil
}

// Commit implements StateProvider.
func (s *StateProviderP2P) Commit(ctx context.Context, height uint64) (*types.Commit, error) {
	s.Lock()
	defer s.Unlock()
	header, err := s.verifyLightBlockAtHeight(ctx, height, time.Now())
	if err != nil {
		return nil, err
	}
	return header.Commit, nil
}

// State implements StateProvider.
func (s *StateProviderP2P) State(ctx context.Context, height uint64) (sm.State, error) {
	s.Lock()
	defer s.Unlock()

	state := sm.State{
		ChainID:       s.lc.ChainID(),
		InitialHeight: s.initialHeight,
	}
	if state.InitialHeight == 0 {
		state.InitialHeight = 1
	}

	// The snapshot height maps onto the state heights as follows:
	//
	// height: last block, i.e. the snapshotted height
	// height+1: current block, i.e. the first block we'll process after the snapshot
	// height+2: next block, i.e. the second block after the snapshot
	//
	// We need to fetch the NextValidators from height+2 because if the application changed
	// the validator set at the snapshot height then this only takes effect at height+2.
	lastLightBlock, err := s.verifyLightBlockAtHeight(ctx, height, time.Now())
	if err != nil {
		return sm.State{}, err
	}
	currentLightBlock, err := s.verifyLightBlockAtHeight(ctx, height+1, time.Now())
	if err != nil {
		return sm.State{}, err
	}
	nextLightBlock, err := s.verifyLightBlockAtHeight(ctx, height+2, time.Now())
	if err != nil {
		return sm.State{}, err
	}

	state.Version = sm.Version{
		Consensus: currentLightBlock.Version,
		Software:  version.TMVersion,
	}
	state.LastBlockHeight = lastLightBlock.Height
	state.LastBlockTime = lastLightBlock.Time
	state.LastBlockID = lastLightBlock.Commit.BlockID
	state.AppHash = currentLightBlock.AppHash
	state.LastResultsHash = currentLightBlock.LastResultsHash
	state.LastValidators = lastLightBlock.ValidatorSet
	state.Validators = currentLightBlock.ValidatorSet
	state.NextValidators = nextLightBlock.ValidatorSet
	state.LastHeightValidatorsChanged = nextLightBlock.Height

	// We'll also need to fetch consensus params via P2P.
	state.ConsensusParams, err = s.consensusParams(ctx, currentLightBlock.Height)
	if err != nil {
		return sm.State{}, fmt.Errorf("fetching consensus params: %w", err)
	}
	// validate the consensus params
	if !bytes.Equal(nextLightBlock.ConsensusHash, state.ConsensusParams.HashConsensusParams()) {
		return sm.State{}, fmt.Errorf("consensus params hash mismatch at height %d. Expected %v, got %v",
			currentLightBlock.Height, nextLightBlock.ConsensusHash, state.ConsensusParams.HashConsensusParams())
	}
	// set the last height changed to the current height
	state.LastHeightConsensusParamsChanged = currentLightBlock.Height

	return state, nil
}

// AddProvider dynamically adds a peer as a new witness. A limit of 6 providers is kept as a
// heuristic. Too many overburdens the network and too little compromises the second layer of security.
func (s *StateProviderP2P) AddProvider(p lightprovider.Provider) {
	if len(s.lc.Witnesses()) < 6 {
		s.lc.AddProvider(p)
	}
}

// RemoveProviderByID removes a peer from the light client's witness list.
func (s *StateProviderP2P) RemoveProviderByID(ID types.NodeID) error {
	return s.lc.RemoveProviderByID(ID)
}

// Providers returns the list of providers (useful for tests)
func (s *StateProviderP2P) Providers() []lightprovider.Provider {
	return s.lc.Witnesses()
}

func (s *StateProviderP2P) ParamsRecvCh() chan types.ConsensusParams {
	return s.paramsRecvCh
}

// consensusParams sends requests for consensus parameters to all witnesses
// in parallel, retrying with increasing backoff until a response is
// received or the context is canceled.
//
// For each witness, a goroutine sends a parameter request, retrying periodically
// if no response is obtained, with increasing intervals. It returns the
// consensus parameters upon receiving a response, or an error if the context is canceled.
func (s *StateProviderP2P) consensusParams(ctx context.Context, height int64) (types.ConsensusParams, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	out := make(chan types.ConsensusParams)

	retryAll := func(childCtx context.Context) error {
		for _, provider := range s.lc.Witnesses() {
			p, ok := provider.(*BlockProvider)
			if !ok {
				return fmt.Errorf("witness is not BlockProvider [%T]", provider)
			}

			peer, err := types.NewNodeID(p.String())
			if err != nil {
				return fmt.Errorf("invalid provider (%s) node id: %w", p.String(), err)
			}

			go func(peer types.NodeID) {
				if err := s.paramsSendCh.Send(childCtx, p2p.Envelope{
					To:      peer,
					Message: s.paramsReqCreator(uint64(height)),
				}); err != nil {
					return
				}

				select {
				case <-childCtx.Done():
					return
				case params, ok := <-s.paramsRecvCh:
					if !ok {
						return
					}
					select {
					case <-childCtx.Done():
						return
					case out <- params:
						return
					}
				}
			}(peer)
		}
		return nil
	}

	timer := time.NewTimer(0)
	defer timer.Stop()

	var iterCount int64
	for {
		iterCount++

		childCtx, childCancel := context.WithCancel(ctx)

		err := retryAll(childCtx)
		if err != nil {
			childCancel()
			return types.ConsensusParams{}, err
		}

		// jitter+backoff the retry loop
		timer.Reset(time.Duration(iterCount)*consensusParamsResponseTimeout +
			time.Duration(100*rand.Int63n(iterCount))*time.Millisecond) // nolint:gosec

		select {
		case param := <-out:
			childCancel()
			return param, nil
		case <-ctx.Done():
			childCancel()
			return types.ConsensusParams{}, ctx.Err()
		case <-timer.C:
			childCancel()
		}
	}
}
