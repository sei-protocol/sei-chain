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

// validatorPageSize is the CometBFT default page, used when per_page is omitted.
const validatorPageSize = 30

func TestInitializeBlockAppliesEveryValidator(t *testing.T) {
	for _, n := range []int{validatorPageSize - 20, validatorPageSize + 10, validatorPageSize*2 + 1} {
		t.Run(fmt.Sprintf("%d validators", n), func(t *testing.T) {
			vals, sigs := validatorSet(n)
			if n > validatorPageSize {
				sigs[n-1].BlockIDFlag = tmtypes.BlockIDFlagAbsent
				sigs[n-1].ValidatorAddress = nil
			}
			tm := newValidatorPageClient(t, sigs, validatorPages(vals, validatorPageSize))

			var got []abci.VoteInfo
			stubBeginBlock(t, func(votes []abci.VoteInfo) { got = votes })
			backend, block := newInitializeBlockBackend(t, tm)
			_, _, release, err := backend.initializeBlock(t.Context(), block, noopTraceCtx)
			require.NoError(t, err)
			release()

			require.Equal(t, expectedPages(n), tm.pages)
			require.False(t, tm.perPageSet)
			requireBeginBlockVotes(t, vals, sigs, got)
		})
	}
}

func TestInitializeBlockAbortsOnBadValidatorPages(t *testing.T) {
	vals, sigs := validatorSet(validatorPageSize + 10)
	pages := validatorPages(vals, validatorPageSize)
	rpcErr := errors.New("rpc down")

	tests := []struct {
		name   string
		source validatorPageFunc
		want   string
		is     error
	}{
		{
			name: "later page is empty",
			source: func(page int) (*coretypes.ResultValidators, error) {
				if page > 1 {
					return &coretypes.ResultValidators{Total: len(vals)}, nil
				}
				return pages(page)
			},
			want: fmt.Sprintf("ended after %d of %d", validatorPageSize, len(vals)),
		},
		{
			name: "page repeats",
			source: func(int) (*coretypes.ResultValidators, error) {
				return pages(1)
			},
			want: fmt.Sprintf("exceeded total %d", len(vals)),
		},
		{
			name: "later page fails",
			source: func(page int) (*coretypes.ResultValidators, error) {
				if page > 1 {
					return nil, rpcErr
				}
				return pages(page)
			},
			want: "failed to load validators for block 7",
			is:   rpcErr,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tm := newValidatorPageClient(t, sigs, tc.source)
			called := false
			stubBeginBlock(t, func([]abci.VoteInfo) { called = true })
			backend, block := newInitializeBlockBackend(t, tm)
			_, _, _, err := backend.initializeBlock(t.Context(), block, noopTraceCtx)
			require.ErrorContains(t, err, tc.want)
			if tc.is != nil {
				require.ErrorIs(t, err, tc.is)
			}
			require.False(t, called)
		})
	}
}

func TestInitializeBlockStopsWhenValidatorContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	called := false
	stubBeginBlock(t, func([]abci.VoteInfo) { called = true })
	backend, block := newInitializeBlockTestBackend(t)
	_, _, _, err := backend.initializeBlock(ctx, block, noopTraceCtx)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, called)
}

func validatorSet(n int) ([]*tmtypes.Validator, []tmtypes.CommitSig) {
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
	return vals, sigs
}

func validatorPages(vals []*tmtypes.Validator, size int) validatorPageFunc {
	return func(page int) (*coretypes.ResultValidators, error) {
		start := (page - 1) * size
		if page < 1 || start >= len(vals) {
			return &coretypes.ResultValidators{Total: len(vals)}, nil
		}
		end := min(start+size, len(vals))
		chunk := vals[start:end]
		return &coretypes.ResultValidators{
			Validators: chunk,
			Count:      len(chunk),
			Total:      len(vals),
		}, nil
	}
}

func expectedPages(n int) []int {
	pages := (n + validatorPageSize - 1) / validatorPageSize
	out := make([]int, pages)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

func requireBeginBlockVotes(t *testing.T, vals []*tmtypes.Validator, sigs []tmtypes.CommitSig, votes []abci.VoteInfo) {
	t.Helper()
	require.Len(t, votes, len(vals))
	for i, val := range vals {
		require.Equal(t, val.VotingPower, votes[i].Validator.Power, "validator %d", i)
		require.Equal(t, val.Address.Bytes(), []byte(votes[i].Validator.Address), "validator %d", i)
		require.Equal(t, sigs[i].BlockIDFlag != tmtypes.BlockIDFlagAbsent, votes[i].SignedLastBlock, "validator %d", i)
	}
}

func stubBeginBlock(t *testing.T, fn func(votes []abci.VoteInfo)) {
	t.Helper()
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })
	runTraceBeginBlock = func(_ sdk.Context, _ int64, votes []abci.VoteInfo, _ []abci.Misbehavior, _ legacyabci.BeginBlockKeepers) {
		fn(votes)
	}
}

func noopTraceCtx(int64) (sdk.Context, func()) {
	return sdk.Context{}, func() {}
}

func newInitializeBlockBackend(t *testing.T, tm *validatorPageClient) (*Backend, *ethtypes.Block) {
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

// validatorPageFunc serves one CometBFT validators page. page is 1-based.
type validatorPageFunc func(page int) (*coretypes.ResultValidators, error)

// validatorPageClient records the pages a trace requests and serves them from source.
type validatorPageClient struct {
	*fakeTMClient
	source     validatorPageFunc
	pages      []int
	perPageSet bool
}

func newValidatorPageClient(t *testing.T, sigs []tmtypes.CommitSig, source validatorPageFunc) *validatorPageClient {
	t.Helper()
	return &validatorPageClient{
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
		source: source,
	}
}

func (c *validatorPageClient) Validators(_ context.Context, _ *int64, page, perPage *int) (*coretypes.ResultValidators, error) {
	p := 1
	if page != nil {
		p = *page
	}
	c.pages = append(c.pages, p)
	if perPage != nil {
		c.perPageSet = true
	}
	return c.source(p)
}
