package dex_test

import (
	"testing"

	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestCancelCopy(t *testing.T) {
	cancels := dex.NewCancels()
	cancel := types.Cancellation{
		Id:      1,
		Creator: "abc",
	}
	cancels.Add(&cancel)
	copy := cancels.Copy()
	copy.Get()[0].Id = 2
	require.Equal(t, uint64(1), cancel.Id)
}

func TestCancelFilterByIds(t *testing.T) {
	cancels := dex.NewCancels()
	cancel := types.Cancellation{
		Id:      1,
		Creator: "abc",
	}
	cancels.Add(&cancel)
	cancels.FilterByIds([]uint64{1})
	require.Equal(t, 0, len(cancels.Get()))
}

func TestCancelGetIdsToCancel(t *testing.T) {
	cancels := dex.NewCancels()
	cancel := types.Cancellation{
		Id:      1,
		Creator: "abc",
	}
	cancels.Add(&cancel)
	ids := cancels.GetIdsToCancel()
	require.Equal(t, 1, len(ids))
	require.Equal(t, uint64(1), ids[0])
}
