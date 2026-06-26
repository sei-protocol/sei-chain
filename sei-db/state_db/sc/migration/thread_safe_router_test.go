package migration

import (
	"errors"
	"testing"
	"time"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// blockingRouter is a Router whose every operation reports its method name
// on entered and waits on release before returning. It gives tests
// step-by-step control over when each call enters and exits the inner
// router so the wrapper's locking can be observed deterministically.
//
// Buffered channels are used so a test goroutine can pre-stage releases (or
// drain entries) without blocking; tests pair every entered receive with a
// corresponding release send.
type blockingRouter struct {
	entered chan string
	release chan struct{}
}

func newBlockingRouter() *blockingRouter {
	return &blockingRouter{
		entered: make(chan string, 16),
		release: make(chan struct{}, 16),
	}
}

func (b *blockingRouter) Read(_ string, _ []byte) ([]byte, bool, error) {
	b.entered <- "Read"
	<-b.release
	return []byte("v"), true, nil
}

func (b *blockingRouter) ApplyChangeSets(_ []*proto.NamedChangeSet, _ bool) error {
	b.entered <- "ApplyChangeSets"
	<-b.release
	return nil
}

func (b *blockingRouter) GetProof(_ string, _ []byte) (*ics23.CommitmentProof, error) {
	b.entered <- "GetProof"
	<-b.release
	return nil, errors.New("proof: not implemented in blockingRouter")
}

func (b *blockingRouter) SetMigrationBatchSize(int) {}

// expectEntered receives one message from ch and asserts it equals expected.
// Fails the test if no message arrives within timeout.
func expectEntered(t *testing.T, ch <-chan string, expected string, timeout time.Duration) {
	t.Helper()
	select {
	case got := <-ch:
		require.Equal(t, expected, got, "unexpected inner-router method call")
	case <-time.After(timeout):
		t.Fatalf("timed out after %s waiting for inner call %q", timeout, expected)
	}
}

// TestThreadSafeRouter_Delegates verifies that every Router method is
// forwarded to the inner router, with arguments and return values intact.
// Uses TestInMemoryRouter as the inner router to get end-to-end behaviour
// for Read / ApplyChangeSets, plus its built-in error for GetProof to
// confirm that is forwarded too.
func TestThreadSafeRouter_Delegates(t *testing.T) {
	inner := NewTestInMemoryRouter()
	tsr, err := NewThreadSafeRouter(inner)
	require.NoError(t, err)

	require.NoError(t, tsr.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: "bank",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}},
	}}, true))

	val, ok, err := tsr.Read("bank", []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v"), val)

	val, ok, err = tsr.Read("bank", []byte("missing"))
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, val)

	_, err = tsr.GetProof("bank", []byte("k"))
	require.Error(t, err, "GetProof must propagate the inner router's error")
}

// TestThreadSafeRouter_NilInner asserts that the constructor rejects a
// nil inner router rather than producing a wrapper that would NPE on
// first use.
func TestThreadSafeRouter_NilInner(t *testing.T) {
	tsr, err := NewThreadSafeRouter(nil)
	require.Error(t, err)
	require.Nil(t, tsr)
}

// TestThreadSafeRouter_WriteExcludesReads confirms that an in-flight
// ApplyChangeSets blocks every read-side call (Read / GetProof) from
// reaching the inner router until the write completes.
func TestThreadSafeRouter_WriteExcludesReads(t *testing.T) {
	br := newBlockingRouter()
	tsr, err := NewThreadSafeRouter(br)
	require.NoError(t, err)

	// Start a write and wait until it has entered the inner router. From
	// this moment until we send to br.release, the wrapper's write lock is
	// held.
	writeDone := make(chan error, 1)
	go func() { writeDone <- tsr.ApplyChangeSets(nil, true) }()
	expectEntered(t, br.entered, "ApplyChangeSets", time.Second)

	// Launch each read-side call in its own goroutine. None should reach
	// the inner router while the write lock is held.
	readDone := make(chan struct{})
	proofDone := make(chan struct{})
	go func() { _, _, _ = tsr.Read("s", nil); close(readDone) }()
	go func() { _, _ = tsr.GetProof("s", nil); close(proofDone) }()

	// Negative assertion: no read-side call should enter the inner router
	// while the write is in flight. A short delay is enough to detect a
	// missing wait — if the wrapper's locking is broken the entry would
	// fire immediately.
	select {
	case op := <-br.entered:
		t.Fatalf("inner call %q reached the inner router while the write lock was held", op)
	case <-time.After(50 * time.Millisecond):
	}

	// Release the write. ApplyChangeSets returns and the wrapper's write
	// lock drops, letting the queued reads proceed.
	br.release <- struct{}{}
	require.NoError(t, <-writeDone)

	// Both queued read-side calls now reach the inner router (in some
	// order); release each in turn and confirm every goroutine returns.
	for range 2 {
		select {
		case op := <-br.entered:
			require.Contains(t, []string{"Read", "GetProof"}, op,
				"unexpected inner-router method")
			br.release <- struct{}{}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for queued read-side call after write release")
		}
	}
	<-readDone
	<-proofDone
}

// TestThreadSafeRouter_ReadsAreConcurrent confirms that Read and GetProof
// can both be in flight at the inner router simultaneously — i.e. the
// wrapper does not serialise them.
func TestThreadSafeRouter_ReadsAreConcurrent(t *testing.T) {
	br := newBlockingRouter()
	tsr, err := NewThreadSafeRouter(br)
	require.NoError(t, err)

	readDone := make(chan struct{})
	proofDone := make(chan struct{})
	go func() { _, _, _ = tsr.Read("s", nil); close(readDone) }()
	go func() { _, _ = tsr.GetProof("s", nil); close(proofDone) }()

	// Both calls must enter the inner router before any are released.
	// If the wrapper accidentally serialised them (e.g. used Lock instead
	// of RLock), we'd only see one entry until that call's release was
	// sent — which never arrives in this loop, so the test would time out.
	seen := map[string]int{}
	for range 2 {
		select {
		case op := <-br.entered:
			seen[op]++
		case <-time.After(time.Second):
			t.Fatalf("only %d/2 read-side calls entered the inner router; an exclusive lock is blocking them", len(seen))
		}
	}
	require.Equal(t, map[string]int{"Read": 1, "GetProof": 1}, seen)

	for range 2 {
		br.release <- struct{}{}
	}
	<-readDone
	<-proofDone
}

// TestThreadSafeRouter_WriteWaitsForReads confirms that an incoming
// ApplyChangeSets cannot acquire the lock while a Read is still in flight
// at the inner router. This is the symmetric counterpart of
// WriteExcludesReads and rules out a Lock-vs-RLock asymmetry bug in the
// wrapper.
func TestThreadSafeRouter_WriteWaitsForReads(t *testing.T) {
	br := newBlockingRouter()
	tsr, err := NewThreadSafeRouter(br)
	require.NoError(t, err)

	readDone := make(chan struct{})
	go func() { _, _, _ = tsr.Read("s", nil); close(readDone) }()
	expectEntered(t, br.entered, "Read", time.Second)

	writeDone := make(chan error, 1)
	go func() { writeDone <- tsr.ApplyChangeSets(nil, true) }()

	select {
	case op := <-br.entered:
		t.Fatalf("ApplyChangeSets reached inner router as %q while a Read still held the read lock", op)
	case <-time.After(50 * time.Millisecond):
	}

	br.release <- struct{}{}
	<-readDone

	expectEntered(t, br.entered, "ApplyChangeSets", time.Second)
	br.release <- struct{}{}
	require.NoError(t, <-writeDone)
}
