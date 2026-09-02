package sview

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// A StoreView is four named views and nothing more, so these tests supply their own names rather than
// depending on flatKV's store layout. They match the names flatKV uses, so a failure message here reads
// the same as one from the store above.
const (
	accountDBDir = "account"
	codeDBDir    = "code"
	storageDBDir = "storage"
	miscDBDir    = "misc"
)

var dataDBDirs = []string{accountDBDir, codeDBDir, storageDBDir, miscDBDir}

var _ view.View = (*fakeView)(nil)

// fakeView is a view whose reserve and flush outcomes the test chooses, and which counts reservations
// both ways. The methods a StoreView never reaches panic, so a use this stub was not written for is loud
// rather than silently wrong.
type fakeView struct {
	// Reported by Name.
	name string

	// Returned by AwaitFlush.
	awaitFlushErr error

	// Returned by Reserve. A non-nil value also suppresses the reserve count.
	reserveErr error

	// Counts successful Reserve calls.
	reserves atomic.Int64

	// Counts Release calls.
	releases atomic.Int64
}

func (v *fakeView) Name() string { return v.name }

func (v *fakeView) AwaitFlush(context.Context) error { return v.awaitFlushErr }

func (v *fakeView) Reserve() error {
	if v.reserveErr != nil {
		return v.reserveErr
	}
	v.reserves.Add(1)
	return nil
}

func (v *fakeView) Release() error {
	v.releases.Add(1)
	return nil
}

func (v *fakeView) Get([]byte, bool) ([]byte, bool, error) {
	panic("fakeView: unexpected Get")
}

func (v *fakeView) BatchGet([][]byte) (map[string][]byte, error) {
	panic("fakeView: unexpected BatchGet")
}

func (v *fakeView) GetDiff() (map[string][]byte, error) {
	panic("fakeView: unexpected GetDiff")
}

func (v *fakeView) Finalize([]*proto.KVPair) error {
	panic("fakeView: unexpected Finalize")
}

// fakeViews returns a store view at version backed by one stub per database, alongside the stubs so a
// test can inspect what was done to them.
func fakeViews(t *testing.T, version int64) (*StoreView, map[string]*fakeView) {
	t.Helper()
	stubs := make(map[string]*fakeView, len(dataDBDirs))
	for _, name := range dataDBDirs {
		stubs[name] = &fakeView{name: name}
	}
	blockView, err := NewStoreView(version,
		stubs[accountDBDir], stubs[codeDBDir], stubs[storageDBDir], stubs[miscDBDir])
	require.NoError(t, err)
	return blockView, stubs
}
