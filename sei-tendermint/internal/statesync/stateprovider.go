package statesync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/light"
	lightprovider "github.com/sei-protocol/sei-chain/sei-tendermint/light/provider"
	lighthttp "github.com/sei-protocol/sei-chain/sei-tendermint/light/provider/http"
	lightrpc "github.com/sei-protocol/sei-chain/sei-tendermint/light/rpc"
	lightdb "github.com/sei-protocol/sei-chain/sei-tendermint/light/store/db"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	rpchttp "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/http"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

const (
	consensusParamsResponseTimeout = 5 * time.Second
)

var (
	errTrustHashMismatch  = errors.New("the block at the trust height does not have the trust hash")
	errNoTrustPeriod      = errors.New("the chain sets no state sync trust period at trust height")
	errParamsHashMismatch = errors.New("consensus params do not match the header's ConsensusHash")
)

//go:generate ../../scripts/mockery_generate.sh StateProvider

var logger = seilog.NewLogger("tendermint", "internal", "statesync")

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

// TrustRoot is the block, by height and hash, that an operator trusts as the root
// of light client verification.
type TrustRoot struct {
	Height int64
	Hash   []byte
}

// paramsSource returns the consensus params at height whose hash is hash.
type paramsSource func(ctx context.Context, height int64, hash []byte) (types.ConsensusParams, error)

// chainTrustOptions returns the light client trust options for root, with the trust
// period that root's header commits to through its ConsensusHash. It fails when the
// chain sets no trust period at root's height.
func chainTrustOptions(
	ctx context.Context,
	root TrustRoot,
	primary lightprovider.Provider,
	params paramsSource,
) (light.TrustOptions, error) {
	trusted, err := primary.LightBlock(ctx, root.Height)
	if err != nil {
		return light.TrustOptions{}, fmt.Errorf("fetching the block at trust height %d: %w", root.Height, err)
	}
	// The trust hash vouches for the header, and the header for the params it commits to.
	if !bytes.Equal(trusted.Hash(), root.Hash) {
		return light.TrustOptions{}, fmt.Errorf("%w: got %X at height %d, want %X",
			errTrustHashMismatch, trusted.Hash(), root.Height, root.Hash)
	}
	cp, err := params(ctx, root.Height, trusted.ConsensusHash)
	if err != nil {
		return light.TrustOptions{}, fmt.Errorf("fetching the consensus params at trust height %d: %w", root.Height, err)
	}
	if cp.StateSync.TrustPeriod == 0 {
		return light.TrustOptions{}, fmt.Errorf("%w %d; choose a trust height at which it does", errNoTrustPeriod, root.Height)
	}
	logger.Info("using the state sync trust period committed to by the trusted header",
		"trustPeriod", cp.StateSync.TrustPeriod, "trustHeight", root.Height)
	return light.TrustOptions{
		Period: cp.StateSync.TrustPeriod,
		Height: root.Height,
		Hash:   root.Hash,
	}, nil
}

type stateProviderRPC struct {
	sync.Mutex              // light.Client is not concurrency-safe
	lc                      *light.Client
	initialHeight           int64
	providers               map[lightprovider.Provider]string
	verifyLightBlockTimeout time.Duration
}

// NewRPCStateProvider creates a new StateProvider using a light client and RPC clients.
func NewRPCStateProvider(
	ctx context.Context,
	chainID string,
	initialHeight int64,
	verifyLightBlockTimeout time.Duration,
	servers []string,
	root TrustRoot,
	blacklistTTL time.Duration,
) (StateProvider, error) {
	if len(servers) < 2 {
		return nil, fmt.Errorf("at least 2 RPC servers are required, got %d", len(servers))
	}

	providers := make([]lightprovider.Provider, 0, len(servers))
	providerRemotes := make(map[lightprovider.Provider]string)
	clients := make([]consensusParamsClient, 0, len(servers))
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
		clients = append(clients, client)
	}

	trustOptions, err := chainTrustOptions(ctx, root, providers[0], rpcParams(clients))
	if err != nil {
		return nil, err
	}
	lc, err := light.NewClient(ctx, chainID, trustOptions, providers[0], providers[1:],
		lightdb.New(dbm.NewMemDB()), blacklistTTL)
	if err != nil {
		return nil, err
	}
	return &stateProviderRPC{
		lc:                      lc,
		initialHeight:           initialHeight,
		providers:               providerRemotes,
		verifyLightBlockTimeout: verifyLightBlockTimeout,
	}, nil
}

func (s *stateProviderRPC) verifyLightBlockAtHeight(ctx context.Context, height uint64, ts time.Time) (*types.LightBlock, error) {
	ctx, cancel := context.WithTimeout(ctx, s.verifyLightBlockTimeout)
	defer cancel()
	return s.lc.VerifyLightBlockAtHeight(ctx, int64(height), ts) //nolint:gosec // height validated by Message.Validate() upstream
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
	rpcclient := lightrpc.NewClient(primaryRPC, s.lc)
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

// consensusParamsClient serves the consensus params an RPC node holds for a height.
type consensusParamsClient interface {
	ConsensusParams(ctx context.Context, height *int64) (*coretypes.ResultConsensusParams, error)
	Remote() string
}

// rpcParams returns a paramsSource that asks each client in turn and returns the
// first params whose hash matches.
func rpcParams(clients []consensusParamsClient) paramsSource {
	return func(ctx context.Context, height int64, hash []byte) (types.ConsensusParams, error) {
		var errs []error
		for _, client := range clients {
			res, err := client.ConsensusParams(ctx, &height)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", client.Remote(), err))
				continue
			}
			if !bytes.Equal(res.ConsensusParams.HashConsensusParams(), hash) {
				errs = append(errs, fmt.Errorf("%s: %w", client.Remote(), errParamsHashMismatch))
				continue
			}
			return res.ConsensusParams, nil
		}
		return types.ConsensusParams{}, errors.Join(errs...)
	}
}

// paramsResponse is the consensus params a peer reports for a height.
type paramsResponse struct {
	height uint64
	params types.ConsensusParams
}

type StateProviderP2P struct {
	sync.Mutex              // light.Client is not concurrency-safe
	lc                      *light.Client
	initialHeight           int64
	paramsSendCh            *p2p.Channel[*pb.Message]
	paramsRecvCh            <-chan paramsResponse
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
	root TrustRoot,
	paramsSendCh *p2p.Channel[*pb.Message],
	paramsRecvCh <-chan paramsResponse,
	blacklistTTL time.Duration,
) (StateProvider, error) {
	if len(providers) < 2 {
		return nil, fmt.Errorf("at least 2 peers are required, got %d", len(providers))
	}

	allProviders := func() []lightprovider.Provider { return providers }
	params := func(ctx context.Context, height int64, hash []byte) (types.ConsensusParams, error) {
		return consensusParams(ctx, paramsSendCh, paramsRecvCh, allProviders, height, hash)
	}
	trustOptions, err := chainTrustOptions(ctx, root, providers[0], params)
	if err != nil {
		return nil, err
	}
	lc, err := light.NewClient(ctx, chainID, trustOptions, providers[0], providers[1:],
		lightdb.New(dbm.NewMemDB()), blacklistTTL)
	if err != nil {
		return nil, err
	}

	return &StateProviderP2P{
		lc:                      lc,
		initialHeight:           initialHeight,
		paramsSendCh:            paramsSendCh,
		paramsRecvCh:            paramsRecvCh,
		verifyLightBlockTimeout: verifyLightBlockTimeout,
	}, nil
}

func (s *StateProviderP2P) verifyLightBlockAtHeight(ctx context.Context, height uint64, ts time.Time) (*types.LightBlock, error) {
	ctx, cancel := context.WithTimeout(ctx, s.verifyLightBlockTimeout)
	defer cancel()
	return s.lc.VerifyLightBlockAtHeight(ctx, int64(height), ts) //nolint:gosec // height validated by Message.Validate() upstream
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

	// We'll also need to fetch consensus params via P2P. The params for a height are
	// the ones that height's header commits to.
	state.ConsensusParams, err = consensusParams(ctx, s.paramsSendCh, s.paramsRecvCh, s.lc.Witnesses,
		currentLightBlock.Height, currentLightBlock.ConsensusHash)
	if err != nil {
		return sm.State{}, fmt.Errorf("fetching consensus params: %w", err)
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

// consensusParams sends requests for consensus parameters to all providers
// in parallel, retrying with increasing backoff until a response for height
// whose hash is hash is received or the context is canceled.
//
// A single goroutine sends a parameter request to every provider, retrying periodically
// if no matching response is obtained, with increasing intervals. Responses for another
// height or that do not match hash are discarded. It returns the consensus parameters
// upon receiving a matching response, or an error if the context is canceled.
func consensusParams(
	ctx context.Context,
	sendCh *p2p.Channel[*pb.Message],
	recvCh <-chan paramsResponse,
	providers func() []lightprovider.Provider,
	height int64,
	hash []byte,
) (types.ConsensusParams, error) {
	return scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (types.ConsensusParams, error) {
		s.SpawnBg(func() error {
			for iterCount := int64(1); ; iterCount++ {
				for _, provider := range providers() {
					p, ok := provider.(*BlockProvider)
					if !ok {
						return fmt.Errorf("witness is not BlockProvider [%T]", provider)
					}

					peer, err := types.NewNodeID(p.String())
					if err != nil {
						return fmt.Errorf("invalid provider (%v) node id: %w", p, err)
					}
					sendCh.Send(wrap(&pb.ParamsRequest{Height: uint64(height)}), peer) //nolint:gosec // height is a validated positive block height
				}
				// jitter+backoff the retry loop
				timeout := time.Duration(iterCount)*consensusParamsResponseTimeout +
					time.Duration(100*rand.Int63n(iterCount))*time.Millisecond //nolint:gosec
				if err := utils.Sleep(ctx, timeout); err != nil {
					return nil
				}
			}
		})
		for {
			resp, err := utils.Recv(ctx, recvCh)
			if err != nil {
				return types.ConsensusParams{}, err
			}
			if resp.height != uint64(height) { //nolint:gosec // height is a validated positive block height
				continue
			}
			if bytes.Equal(resp.params.HashConsensusParams(), hash) {
				return resp.params, nil
			}
			logger.Info("discarding consensus params that do not match the header", "height", height)
		}
	})
}
