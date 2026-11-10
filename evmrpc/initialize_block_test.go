package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app/legacyabci"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestReleaseOnContextPanic(t *testing.T) {
	t.Parallel()

	var released int
	release := func() { released++ }

	err := releaseOnContextPanic(release, context.Canceled)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, released)

	err = releaseOnContextPanic(release, context.DeadlineExceeded)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 2, released)

	err = releaseOnContextPanic(release, fmt.Errorf("skip: %w", context.DeadlineExceeded))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 3, released)

	require.Panics(t, func() {
		_ = releaseOnContextPanic(release, errors.New("pebble seek failed"))
	})
	require.Equal(t, 4, released)

	require.Panics(t, func() {
		_ = releaseOnContextPanic(release, "not an error")
	})
	require.Equal(t, 5, released)
}

func TestInitializeBlockReleasesLeaseOnBeginBlockDeadline(t *testing.T) {
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })
	runTraceBeginBlock = func(sdk.Context, int64, []abci.VoteInfo, []abci.Misbehavior, legacyabci.BeginBlockKeepers) {
		panic(context.DeadlineExceeded)
	}

	var released int
	backend, block := newInitializeBlockTestBackend(t)
	_, _, release, err := backend.initializeBlock(t.Context(), block, func(int64) (sdk.Context, func()) {
		return sdk.Context{}, func() { released++ }
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, released, "base snapshot lease must be released on BeginBlock abort")
	release()
	require.Equal(t, 1, released, "returned release must be a no-op after recover")
}

func TestInitializeBlockReleasesLeaseOnUnrelatedBeginBlockPanic(t *testing.T) {
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })
	runTraceBeginBlock = func(sdk.Context, int64, []abci.VoteInfo, []abci.Misbehavior, legacyabci.BeginBlockKeepers) {
		panic("boom")
	}

	var released int
	backend, block := newInitializeBlockTestBackend(t)
	require.Panics(t, func() {
		_, _, _, _ = backend.initializeBlock(t.Context(), block, func(int64) (sdk.Context, func()) {
			return sdk.Context{}, func() { released++ }
		})
	})
	require.Equal(t, 1, released)
}

func newInitializeBlockTestBackend(t *testing.T) (*Backend, *ethtypes.Block) {
	t.Helper()
	tm := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 10, EarliestBlockHeight: 1}},
		blocksByHeight: map[int64]*coretypes.ResultBlock{
			8: {
				Block: &tmtypes.Block{
					Header:     tmtypes.Header{Height: 8},
					LastCommit: &tmtypes.Commit{},
				},
			},
		},
	}
	return &Backend{
		tmClient:   tm,
		watermarks: newTestWatermarkManager(tm, 10, nil, 10),
	}, ethtypes.NewBlock(
		&ethtypes.Header{Number: big.NewInt(8), Time: 1, Difficulty: big.NewInt(0)},
		&ethtypes.Body{},
		nil,
		trie.NewStackTrie(nil),
	)
}

func TestInitializeBlockAppliesEveryValidator(t *testing.T) {
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })

	// Counts straddle the default page of 30: one page, two pages, and three.
	for _, tc := range []struct {
		name   string
		n      int
		calls  int
		absent int
		pages  []int
	}{
		{name: "one page", n: 10, calls: 1, absent: -1, pages: []int{1}},
		{name: "two pages", n: 40, calls: 2, absent: 31, pages: []int{1, 2}},
		{name: "three pages", n: 61, calls: 3, absent: 60, pages: []int{1, 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vals, sigs := validatorSet(tc.n, tc.absent)
			tm := newPagingValidatorsClient(t, vals, sigs)
			var got []abci.VoteInfo
			runTraceBeginBlock = func(_ sdk.Context, _ int64, votes []abci.VoteInfo, _ []abci.Misbehavior, _ legacyabci.BeginBlockKeepers) {
				got = votes
			}
			backend, block := newInitializeBlockBackend(t, tm)
			_, _, release, err := backend.initializeBlock(t.Context(), block, noopTraceCtx)
			require.NoError(t, err)
			release()
			require.Equal(t, tc.calls, tm.calls)
			require.Equal(t, tc.pages, tm.pages)
			require.True(t, tm.perPageOmitted)
			requireValidatorVotes(t, vals, got, tc.absent)
		})
	}
}

func TestInitializeBlockRejectsShortValidatorPage(t *testing.T) {
	vals, sigs := validatorSet(40, -1)
	tm := newPagingValidatorsClient(t, vals, sigs)
	tm.emptyFromPage = 2

	called := false
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })
	runTraceBeginBlock = func(sdk.Context, int64, []abci.VoteInfo, []abci.Misbehavior, legacyabci.BeginBlockKeepers) {
		called = true
	}

	backend, block := newInitializeBlockBackend(t, tm)
	_, _, _, err := backend.initializeBlock(t.Context(), block, noopTraceCtx)
	require.ErrorContains(t, err, "validator set at block 7 ended after 30 of 40")
	require.False(t, called)
}

func TestInitializeBlockRejectsOverlappingValidatorPages(t *testing.T) {
	const n = 40
	vals := make([]*tmtypes.Validator, n)
	sigs := make([]tmtypes.CommitSig, n)
	for i := range n {
		pk := ed25519.GenerateSecretKey().Public()
		vals[i] = tmtypes.NewValidator(pk, int64(n-i))
		sigs[i] = tmtypes.CommitSig{BlockIDFlag: tmtypes.BlockIDFlagCommit, ValidatorAddress: vals[i].Address}
	}
	tm := newPagingValidatorsClient(t, vals, sigs)
	tm.repeatFirstPage = true

	called := false
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })
	runTraceBeginBlock = func(sdk.Context, int64, []abci.VoteInfo, []abci.Misbehavior, legacyabci.BeginBlockKeepers) {
		called = true
	}

	backend, block := newInitializeBlockBackend(t, tm)
	_, _, _, err := backend.initializeBlock(t.Context(), block, noopTraceCtx)
	require.ErrorContains(t, err, "validator pages at block 7 exceeded total 40")
	require.False(t, called)
}

func TestInitializeBlockValidatorLoadError(t *testing.T) {
	vals, sigs := validatorSet(40, -1)
	tm := newPagingValidatorsClient(t, vals, sigs)
	tm.failPage = 2
	tm.failErr = errors.New("rpc down")

	called := false
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })
	runTraceBeginBlock = func(sdk.Context, int64, []abci.VoteInfo, []abci.Misbehavior, legacyabci.BeginBlockKeepers) {
		called = true
	}

	backend, block := newInitializeBlockBackend(t, tm)
	_, _, _, err := backend.initializeBlock(t.Context(), block, noopTraceCtx)
	require.ErrorContains(t, err, "failed to load validators for block 7")
	require.ErrorIs(t, err, tm.failErr)
	require.False(t, called)
}

func TestInitializeBlockStopsWhenValidatorContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	called := false
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })
	runTraceBeginBlock = func(sdk.Context, int64, []abci.VoteInfo, []abci.Misbehavior, legacyabci.BeginBlockKeepers) {
		called = true
	}

	backend, block := newInitializeBlockTestBackend(t)
	_, _, _, err := backend.initializeBlock(ctx, block, noopTraceCtx)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, called)
}

func validatorSet(n, absent int) ([]*tmtypes.Validator, []tmtypes.CommitSig) {
	vals := make([]*tmtypes.Validator, n)
	sigs := make([]tmtypes.CommitSig, n)
	for i := range n {
		pk := ed25519.GenerateSecretKey().Public()
		vals[i] = tmtypes.NewValidator(pk, int64(n-i))
		sigs[i] = tmtypes.CommitSig{
			BlockIDFlag:      tmtypes.BlockIDFlagCommit,
			ValidatorAddress: vals[i].Address,
		}
	}
	if absent >= 0 {
		sigs[absent].BlockIDFlag = tmtypes.BlockIDFlagAbsent
		sigs[absent].ValidatorAddress = nil
	}
	return vals, sigs
}

func requireValidatorVotes(t *testing.T, vals []*tmtypes.Validator, votes []abci.VoteInfo, absent int) {
	t.Helper()
	require.Len(t, votes, len(vals))
	for i, val := range vals {
		require.Equal(t, val.VotingPower, votes[i].Validator.Power, "validator %d", i)
		require.Equal(t, val.Address.Bytes(), []byte(votes[i].Validator.Address), "validator %d", i)
		require.Equal(t, i != absent, votes[i].SignedLastBlock, "validator %d", i)
	}
}

func noopTraceCtx(int64) (sdk.Context, func()) {
	return sdk.Context{}, func() {}
}

func newInitializeBlockBackend(t *testing.T, tm *pagingValidatorsClient) (*Backend, *ethtypes.Block) {
	t.Helper()
	return &Backend{
		tmClient:   tm,
		watermarks: newTestWatermarkManager(tm, 10, nil, 10),
	}, ethtypes.NewBlock(
		&ethtypes.Header{Number: big.NewInt(8), Time: 1, Difficulty: big.NewInt(0)},
		&ethtypes.Body{},
		nil,
		trie.NewStackTrie(nil),
	)
}

// pagingValidatorsClient serves validators the way CometBFT does: a nil per_page
// is the default page of 30.
type pagingValidatorsClient struct {
	*fakeTMClient
	validators      []*tmtypes.Validator
	calls           int
	pages           []int
	perPageOmitted  bool
	failPage        int
	failErr         error
	repeatFirstPage bool
	emptyFromPage   int
}

func newPagingValidatorsClient(t *testing.T, vals []*tmtypes.Validator, sigs []tmtypes.CommitSig) *pagingValidatorsClient {
	t.Helper()
	return &pagingValidatorsClient{
		fakeTMClient: &fakeTMClient{
			status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 10, EarliestBlockHeight: 1}},
			blocksByHeight: map[int64]*coretypes.ResultBlock{
				8: {
					Block: &tmtypes.Block{
						Header:     tmtypes.Header{Height: 8},
						LastCommit: &tmtypes.Commit{Signatures: sigs},
					},
				},
			},
		},
		validators: vals,
	}
}

func (c *pagingValidatorsClient) Validators(_ context.Context, _ *int64, page, perPage *int) (*coretypes.ResultValidators, error) {
	c.calls++
	if c.calls == 1 {
		c.perPageOmitted = perPage == nil
	} else {
		c.perPageOmitted = c.perPageOmitted && perPage == nil
	}
	p := 1
	if page != nil {
		p = *page
	}
	c.pages = append(c.pages, p)
	if c.failErr != nil && p >= c.failPage {
		return nil, c.failErr
	}
	if c.emptyFromPage > 0 && p >= c.emptyFromPage {
		return &coretypes.ResultValidators{Total: len(c.validators)}, nil
	}
	n := 30
	if perPage != nil && *perPage > 0 {
		n = *perPage
	}
	if c.repeatFirstPage {
		p = 1
	}
	start := (p - 1) * n
	if start >= len(c.validators) {
		return &coretypes.ResultValidators{Total: len(c.validators)}, nil
	}
	end := start + n
	if end > len(c.validators) {
		end = len(c.validators)
	}
	chunk := c.validators[start:end]
	return &coretypes.ResultValidators{
		Validators: chunk,
		Count:      len(chunk),
		Total:      len(c.validators),
	}, nil
}
