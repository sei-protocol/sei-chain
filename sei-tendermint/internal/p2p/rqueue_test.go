package p2p

import (
	"context"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"slices"
	"testing"
)

func TestQueuePruning(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	n := 20
	var want []int
	sq := NewQueue[int](n)
	for range 100 {
		// Send a bunch of messages.
		for range 30 {
			v := rng.Int()
			sq.Send(v, 10, int(v))
			want = append(want, int(v))
		}

		// Low priority messages should be dropped.
		slices.Sort(want)
		l := len(want)
		want = want[l-n:]
		if len(want) != sq.Len() {
			t.Fatalf("expected len %d, got %d", len(want), sq.Len())
		}

		// Receive a bunch of messages.
		for range 5 {
			got, err := sq.Recv(ctx)
			if err != nil {
				t.Fatal(err)
			}
			l := len(want)
			if want := want[l-1]; got != want {
				t.Fatalf("sq.Recv() = %d, want %d", got, want)
			}
			want = want[:l-1]
		}
		if len(want) != sq.Len() {
			t.Fatalf("expected len %d, got %d", len(want), sq.Len())
		}
	}
}

// Test that receivers are notified when a message is available.
func TestQueueConcurrency(t *testing.T) {
	ctx := t.Context()
	q1, q2 := NewQueue[RecvMsg[*TestMessage]](1), NewQueue[RecvMsg[*TestMessage]](1)

	if err := utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			// Echo task.
			for {
				msg, err := q1.Recv(ctx)
				if err != nil {
					return err
				}
				q2.Send(msg, 12, 0)
			}
		})
		// Send and receive a bunch of messages.
		for range 100 {
			q1.Send(RecvMsg[*TestMessage]{From: "merlin", Message: &TestMessage{}}, 12, 0)
			if _, err := q2.Recv(ctx); err != nil {
				return err
			}
		}
		return nil
	})); err != nil {
		t.Fatal(err)
	}
}
