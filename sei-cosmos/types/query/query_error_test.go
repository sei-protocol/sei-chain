package query_test

import (
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWrapQueryErrorNil(t *testing.T) {
	require.NoError(t, query.WrapQueryError(nil))
}

func TestWrapQueryErrorPreservesStatus(t *testing.T) {
	orig := status.Errorf(codes.InvalidArgument, "limit exceeds maximum")
	wrapped := query.WrapQueryError(orig)
	require.Equal(t, orig, wrapped)

	st, ok := status.FromError(wrapped)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestWrapQueryErrorWrapsPlainError(t *testing.T) {
	orig := fmt.Errorf("unmarshal failed")
	wrapped := query.WrapQueryError(orig)

	st, ok := status.FromError(wrapped)
	require.True(t, ok)
	require.Equal(t, codes.Internal, st.Code())
	require.Equal(t, orig.Error(), st.Message())
}
