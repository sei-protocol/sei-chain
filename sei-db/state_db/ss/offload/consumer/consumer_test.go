package consumer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
)

type fakeSource struct {
	msgs      []kafka.Message
	fetchIdx  int
	committed []kafka.Message
	fetchErr  error
	mu        sync.Mutex
}

func (f *fakeSource) FetchMessage(ctx context.Context) (kafka.Message, error) {
	f.mu.Lock()
	if f.fetchErr != nil {
		err := f.fetchErr
		f.mu.Unlock()
		return kafka.Message{}, err
	}
	if f.fetchIdx < len(f.msgs) {
		m := f.msgs[f.fetchIdx]
		f.fetchIdx++
		f.mu.Unlock()
		return m, nil
	}
	f.mu.Unlock()
	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}

func (f *fakeSource) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.committed = append(f.committed, msgs...)
	return nil
}

type recordingSink struct {
	records []Record
	err     error
}

func (s *recordingSink) Write(ctx context.Context, rec Record) error {
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, rec)
	return nil
}
func (s *recordingSink) LastVersion(ctx context.Context) (int64, error) { return 0, nil }
func (s *recordingSink) Close() error                                   { return nil }

type batchRecordingSink struct {
	records []Record
	batches []int
}

func (s *batchRecordingSink) Write(context.Context, Record) error {
	panic("Write should not be called when WriteBatch is available")
}
func (s *batchRecordingSink) WriteBatch(_ context.Context, records []Record) error {
	s.batches = append(s.batches, len(records))
	s.records = append(s.records, records...)
	return nil
}
func (s *batchRecordingSink) LastVersion(context.Context) (int64, error) { return 0, nil }
func (s *batchRecordingSink) Close() error                               { return nil }

// Fails failuresLeft Write calls then succeeds.
type flakySink struct {
	failuresLeft int
	attempts     int
}

func (s *flakySink) Write(ctx context.Context, rec Record) error {
	s.attempts++
	if s.failuresLeft > 0 {
		s.failuresLeft--
		return errors.New("transient")
	}
	return nil
}
func (s *flakySink) LastVersion(ctx context.Context) (int64, error) { return 0, nil }
func (s *flakySink) Close() error                                   { return nil }

func marshalEntry(t *testing.T, version int64, pairs ...*dbproto.KVPair) []byte {
	t.Helper()
	entry := &dbproto.ChangelogEntry{
		Version: version,
		Changesets: []*dbproto.NamedChangeSet{{
			Name:      "evm",
			Changeset: dbproto.ChangeSet{Pairs: pairs},
		}},
	}
	payload, err := gogoproto.Marshal(entry)
	require.NoError(t, err)
	return payload
}

func TestConsumerRunWritesAndCommits(t *testing.T) {
	src := &fakeSource{msgs: []kafka.Message{
		{Topic: "t", Partition: 0, Offset: 10, Value: marshalEntry(t, 1, &dbproto.KVPair{Key: []byte("a"), Value: []byte("1")})},
		{Topic: "t", Partition: 0, Offset: 11, Value: marshalEntry(t, 2, &dbproto.KVPair{Key: []byte("b"), Delete: true})},
	}}
	sink := &recordingSink{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c := New(src, sink, Options{})
	err := c.Run(ctx)
	require.NoError(t, err)

	require.Len(t, sink.records, 2)
	require.Equal(t, int64(1), sink.records[0].Entry.Version)
	require.Equal(t, int64(11), sink.records[1].Offset)

	require.Len(t, src.committed, 2)
	require.Equal(t, int64(10), src.committed[0].Offset)
	require.Equal(t, int64(11), src.committed[1].Offset)
}

func TestConsumerRunSinkErrorStopsBeforeCommit(t *testing.T) {
	src := &fakeSource{msgs: []kafka.Message{
		{Topic: "t", Offset: 1, Value: marshalEntry(t, 1)},
	}}
	sink := &recordingSink{err: errors.New("sink boom")}

	c := New(src, sink, Options{
		SinkMaxAttempts: 2,
		SinkBaseBackoff: time.Millisecond,
		SinkMaxBackoff:  time.Millisecond,
	})
	err := c.Run(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "sink boom")
	require.Empty(t, src.committed, "offset must not be committed when sink fails")
}

func TestConsumerRunRetriesSinkUntilSuccess(t *testing.T) {
	src := &fakeSource{msgs: []kafka.Message{
		{Topic: "t", Partition: 0, Offset: 5, Value: marshalEntry(t, 1, &dbproto.KVPair{Key: []byte("a"), Value: []byte("1")})},
	}}
	sink := &flakySink{failuresLeft: 2}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c := New(src, sink, Options{
		SinkMaxAttempts: 5,
		SinkBaseBackoff: time.Millisecond,
		SinkMaxBackoff:  2 * time.Millisecond,
	})
	require.NoError(t, c.Run(ctx))

	require.Equal(t, 3, sink.attempts, "sink should be retried until it succeeds")
	require.Len(t, src.committed, 1)
	require.Equal(t, int64(5), src.committed[0].Offset)
}

func TestConsumerBatchesSinkWritesAndCommits(t *testing.T) {
	src := &fakeSource{msgs: []kafka.Message{
		{Topic: "t", Partition: 0, Offset: 10, Value: marshalEntry(t, 1)},
		{Topic: "t", Partition: 0, Offset: 11, Value: marshalEntry(t, 2)},
		{Topic: "t", Partition: 0, Offset: 12, Value: marshalEntry(t, 3)},
	}}
	sink := &batchRecordingSink{}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	c := New(src, sink, Options{
		Workers:         1,
		MaxBatchRecords: 3,
		BatchMaxWait:    time.Hour,
	})
	require.NoError(t, c.Run(ctx))

	require.Equal(t, []int{3}, sink.batches)
	require.Len(t, sink.records, 3)
	require.Len(t, src.committed, 3)
	require.Equal(t, int64(12), src.committed[2].Offset)
}

func TestConsumerRunDecodeErrorStops(t *testing.T) {
	src := &fakeSource{msgs: []kafka.Message{
		{Topic: "t", Offset: 1, Value: []byte{0xff, 0xff}},
	}}
	sink := &recordingSink{}

	err := New(src, sink, Options{}).Run(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode message")
	require.Empty(t, sink.records)
	require.Empty(t, src.committed)
}

func TestConsumerRunCancelReturnsNil(t *testing.T) {
	src := &fakeSource{fetchErr: context.Canceled}
	err := New(src, &recordingSink{}, Options{}).Run(context.Background())
	require.NoError(t, err)
}

// Tracks max in-flight so the test can assert >1 Write runs at once.
type concurrentSink struct {
	mu          sync.Mutex
	records     []Record
	maxInFlight int32
	inFlight    int32
	delay       time.Duration
}

func (s *concurrentSink) Write(_ context.Context, rec Record) error {
	cur := atomic.AddInt32(&s.inFlight, 1)
	defer atomic.AddInt32(&s.inFlight, -1)
	for {
		prev := atomic.LoadInt32(&s.maxInFlight)
		if cur <= prev || atomic.CompareAndSwapInt32(&s.maxInFlight, prev, cur) {
			break
		}
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	s.mu.Lock()
	s.records = append(s.records, rec)
	s.mu.Unlock()
	return nil
}
func (s *concurrentSink) LastVersion(context.Context) (int64, error) { return 0, nil }
func (s *concurrentSink) Close() error                               { return nil }

func TestConsumerParallelFansOutAcrossPartitions(t *testing.T) {
	const nPartitions = 4
	msgs := make([]kafka.Message, 0, nPartitions)
	for p := 0; p < nPartitions; p++ {
		msgs = append(msgs, kafka.Message{
			Topic: "t", Partition: p, Offset: int64(p),
			Value: marshalEntry(t, int64(p+1)),
		})
	}
	src := &fakeSource{msgs: msgs}
	sink := &concurrentSink{delay: 25 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c := New(src, sink, Options{Workers: nPartitions})
	require.NoError(t, c.Run(ctx))

	require.Len(t, sink.records, nPartitions)
	require.Greater(t, atomic.LoadInt32(&sink.maxInFlight), int32(1),
		"with Workers=%d the sink should see >1 concurrent writes", nPartitions)
}

// Same partition lands on the same worker (preserves ordering).
func TestShardForStablePerPartition(t *testing.T) {
	require.Equal(t, shardFor(7, 4), shardFor(7, 4))
	require.NotEqual(t, shardFor(0, 4), shardFor(1, 4))
	require.GreaterOrEqual(t, shardFor(-3, 4), 0, "negative partition shouldn't go negative")
}
